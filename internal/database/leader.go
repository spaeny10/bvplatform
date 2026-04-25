package database

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Postgres advisory-lock leader election for the worker process.
//
// Why this matters for UL 827B / TMA-AVS-01 deployments: the worker
// runs retention, the VLM indexer, and the export queue. Two workers
// racing on the same export job could double-send evidence to a
// customer; two retention runs racing could over-delete segments
// during a brief overlap. We need exactly one worker active at a
// time, with predictable failover when that worker dies.
//
// Postgres advisory locks fit this perfectly:
//   - pg_try_advisory_lock(key) is non-blocking; we can retry.
//   - Locks are session-scoped; if our connection drops (process
//     crash, network partition, kill -9), Postgres releases the lock
//     automatically and another standby can take over.
//   - No external coordinator needed. The DB is already a hard
//     dependency for the worker, so adding etcd/zookeeper for this
//     would be net-negative.
//
// The lock key is a 63-bit hash of a stable string. Different worker
// types (export vs indexer vs retention) can use different strings if
// we ever want them to fail over independently — for now they all
// share a single lock so "leader = the one worker process running."

// LeaderHandle is what the caller holds while it's the leader. Calling
// Release drops the lock (and closes the dedicated connection); the
// next process polling pg_try_advisory_lock will pick it up. Lost()
// returns a channel that closes when the connection drops out from
// under us, so the caller can shut down its loops gracefully.
type LeaderHandle struct {
	conn   *pgx.Conn
	cancel context.CancelFunc
	lostCh chan struct{}
	once   sync.Once
}

// Lost returns a channel closed when leadership is lost — either
// because Release() was called explicitly OR because the underlying
// connection died. Block on this in the worker main loop to know
// when to stop scheduling jobs.
func (h *LeaderHandle) Lost() <-chan struct{} {
	return h.lostCh
}

// Release voluntarily gives up leadership. Idempotent.
func (h *LeaderHandle) Release() {
	h.once.Do(func() {
		h.cancel()
		// Close the underlying connection — this releases the advisory
		// lock at the Postgres side. If the connection is already dead,
		// Close is a no-op.
		if h.conn != nil {
			_ = h.conn.Close(context.Background())
		}
		close(h.lostCh)
	})
}

// AcquireLeader attempts to become the worker leader by acquiring a
// Postgres session-scoped advisory lock. Polls every pollInterval
// until the lock is obtained or ctx is cancelled. Once acquired,
// spawns a heartbeat goroutine that pings the connection every 10s;
// if the ping fails, leadership is lost and the returned handle's
// Lost() channel closes.
//
// Pass a fresh DSN (not the pool config string) so we open a dedicated
// session. Reusing pool connections doesn't work for advisory locks
// — pgxpool can hand the connection to another goroutine, dropping
// our lock as a side effect.
//
// The key string identifies the leader-election scope. Use a stable,
// human-readable string like "ironsight-worker-loops" so any
// operational tooling that inspects pg_locks can see what's held.
func AcquireLeader(ctx context.Context, dsn, key string, pollInterval time.Duration) (*LeaderHandle, error) {
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	keyInt := int64(fnv.New64a().Sum64())
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	keyInt = int64(h.Sum64() & 0x7FFFFFFFFFFFFFFF) // drop sign bit so int8 conversion is well-defined

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("leader: connect: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(context.Background())
			return nil, ctx.Err()
		default:
		}

		var got bool
		err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, keyInt).Scan(&got)
		if err != nil {
			_ = conn.Close(context.Background())
			return nil, fmt.Errorf("leader: try_advisory_lock: %w", err)
		}
		if got {
			break
		}
		log.Printf("[LEADER] another worker holds the lock (key=%q); will retry in %s", key, pollInterval)

		t := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			t.Stop()
			_ = conn.Close(context.Background())
			return nil, ctx.Err()
		case <-t.C:
		}
	}

	log.Printf("[LEADER] acquired advisory lock (key=%q)", key)
	hbCtx, cancel := context.WithCancel(context.Background())
	handle := &LeaderHandle{
		conn:   conn,
		cancel: cancel,
		lostCh: make(chan struct{}),
	}

	// Heartbeat. Postgres releases the lock if the connection drops,
	// so all we need is a periodic ping to confirm the connection is
	// still alive. If the ping errors, signal lost-leadership and stop.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				if err := conn.Ping(hbCtx); err != nil {
					log.Printf("[LEADER] heartbeat failed: %v — releasing leadership", err)
					handle.Release()
					return
				}
			}
		}
	}()

	return handle, nil
}
