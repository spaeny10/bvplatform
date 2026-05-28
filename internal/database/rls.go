package database

// rls.go — P4-SCHEMA-07: Row-Level Security tenant connection helpers.
//
// DESIGN: Option B (AcquireWithTenant)
// ─────────────────────────────────────────────────────────────────────────
// We chose Option B (explicit acquire + SET + release) over Option A
// (connection-in-context for every handler) because Option A's blast radius
// is too wide: ~200 db.Pool.Query call sites across 15 files would all need
// to be rerouted through a context-extracted connection. That refactor scope
// exceeds the RLS defense-in-depth goal and creates regression risk.
//
// Option B provides equivalent DB-level enforcement with surgical scope:
//   1. Call AcquireWithTenant(ctx, pool, tenant) — acquires a connection
//      from the pool and issues SET LOCAL app.current_tenant = $1.
//      "LOCAL" scopes the GUC to the current transaction; when the
//      transaction ends (on conn release pgxpool automatically rolls back
//      implicit transactions) the GUC resets automatically.
//   2. Use conn.Exec/Query/... as needed.
//   3. Call conn.Release() — returns the connection to the pool.
//      If SET was session-scoped (not LOCAL) we would need an explicit
//      RESET; with SET LOCAL + a transaction the reset is automatic.
//
// TRANSACTION REQUIREMENT
// ───────────────────────
// SET LOCAL is only valid inside an explicit transaction. AcquireWithTenant
// therefore begins an explicit transaction. The caller is responsible for
// committing or rolling back. If the caller only reads, a Rollback is fine.
//
// WORKER / SERVICE MODE
// ─────────────────────
// Background workers (PPE worker, VLM worker, consistency-check, seed, migrate)
// connect as 'onvif' which has the service_bypass policy. They do NOT need to
// call AcquireWithTenant unless they want per-tenant filtering. When a worker
// iterates per-org (e.g. the monthly digest worker), call AcquireWithTenant
// for each org's query batch so RLS provides an extra safety net on accidental
// cross-org queries.
//
// CONNECTION-LEAK PREVENTION
// ────────────────────────────
// The caller pattern MUST be:
//
//   conn, tx, err := db.AcquireWithTenant(ctx, tenant)
//   if err != nil { ... }
//   defer conn.Release()      // always return to pool
//   defer tx.Rollback(ctx)    // no-op after Commit; safe to call always
//   // ... use conn for queries within tx ...
//   if err := tx.Commit(ctx); err != nil { ... }
//
// DO NOT call conn.Release() without either Commit or Rollback — the
// connection will be returned to the pool with an open transaction.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AcquireWithTenant acquires a connection from the pool, begins an explicit
// transaction, and issues SET LOCAL app.current_tenant = <tenant> inside
// that transaction so that all RLS policies in migration 0031 evaluate
// against the given tenant ID.
//
// The caller MUST:
//   - defer conn.Release() immediately after a successful return
//   - call tx.Rollback(ctx) or tx.Commit(ctx) before release
//   - not call Release() before the transaction is ended
//
// Returns an error if the pool acquire fails, the BEGIN fails, or the SET
// fails.  In all error cases the connection (if acquired) is released before
// returning, so the caller never receives a half-initialised connection.
func AcquireWithTenant(ctx context.Context, pool *pgxpool.Pool, tenant string) (conn *pgxpool.Conn, tx pgx.Tx, err error) {
	conn, err = pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("rls.AcquireWithTenant: acquire: %w", err)
	}

	// Begin explicit transaction — required for SET LOCAL.
	tx, err = conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		conn.Release()
		return nil, nil, fmt.Errorf("rls.AcquireWithTenant: begin tx: %w", err)
	}

	// set_config(name, value, is_local) is the parametrised equivalent of
	// SET LOCAL — `SET` itself does not accept $-placeholders, so we use the
	// function form to avoid string-concatenating tenant IDs into SQL.
	// is_local=true scopes the GUC to this transaction; when the transaction
	// ends (commit or rollback) the GUC is automatically cleared — no explicit
	// RESET needed. Even if the caller forgets to commit/rollback, pgxpool's
	// release path triggers a rollback on the idle connection, clearing the
	// GUC before the connection re-enters the pool.
	if _, err = tx.Exec(ctx, `SELECT set_config('app.current_tenant', $1, true)`, tenant); err != nil {
		_ = tx.Rollback(ctx)
		conn.Release()
		return nil, nil, fmt.Errorf("rls.AcquireWithTenant: set local: %w", err)
	}

	return conn, tx, nil
}

// SetTenantOnConn sets app.current_tenant on an already-open transaction.
// Use when you need to switch the tenant mid-transaction (e.g. a worker
// iterating across orgs with a single acquired connection). The caller
// is responsible for calling this again with a new tenant or ending the
// transaction before reuse.
//
// This is a lower-level escape hatch; prefer AcquireWithTenant for the
// standard request-scoped use case.
func SetTenantOnConn(ctx context.Context, tx pgx.Tx, tenant string) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_tenant', $1, true)`, tenant); err != nil {
		return fmt.Errorf("rls.SetTenantOnConn: %w", err)
	}
	return nil
}
