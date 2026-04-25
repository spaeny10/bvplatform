// Package main — Ironsight worker binary.
//
// Runs the background batch workloads that previously lived inside the
// api process:
//
//   - RetentionManager  — hourly segment purge per site policy
//   - VLM Indexer       — enriches recording segments with AI descriptions
//   - Export Worker     — processes queued video-export jobs
//
// Designed for docker-compose / K8s deployments where the api service
// runs with RUN_WORKERS=false and this binary runs as a single-replica
// sibling container. In dev, run the api binary with default
// RUN_WORKERS=true and skip this process entirely.
//
// Explicitly NOT moved here yet:
//
//   - Recording engine  (stateful per-camera FFmpeg, stays with api)
//   - HLS server        (same reason)
//   - MediaMTX control  (colocated with recording)
//   - ONVIF event subs  (tightly coupled to the event-mode recording
//                        trigger path; split is Phase 3)
//
// The worker does NOT serve HTTP. Health can be observed via its logs
// and via the DB rows it writes (e.g., export status transitions).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"onvif-tool/internal/ai"
	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
	"onvif-tool/internal/export"
	"onvif-tool/internal/indexer"
	"onvif-tool/internal/recording"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("============================================")
	log.Println("  Ironsight Worker — batch background jobs")
	log.Println("============================================")

	cfg := config.Load()

	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Database connect: %v", err)
	}
	defer db.Close()

	// AI client — same wiring as api. Indexer needs it; retention and
	// exports don't. Cheap to initialise either way; skipping keeps the
	// worker dependent on AI service availability only at indexing time.
	aiClient := ai.NewClient(ai.Config{
		YOLOEndpoint: cfg.DetectionServiceURL, // repurposed; kept non-critical
		QwenEndpoint: envOr("AI_QWEN_URL", "http://127.0.0.1:8502"),
		Enabled:      envOr("AI_ENABLED", "true") != "false",
	})
	// Non-fatal — indexer handles a missing AI gracefully (jobs fail but
	// retention + exports keep working).
	aiClient.CheckHealth(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Leader election ──────────────────────────────────────────
	// Acquire a Postgres advisory lock before starting any of the
	// retention / indexer / export loops. If another worker process
	// already holds the lock, this call blocks (with periodic retry
	// logging) until it's free — at which point this process takes
	// over. Combined with restart: unless-stopped on the worker
	// container, this gives us hot-standby semantics for free: bring
	// up N worker replicas, exactly one runs jobs at a time, and
	// failover happens within ~30s of the leader dying. Set
	// WORKER_LEADER_DISABLED=1 to skip the lock entirely (single-binary
	// dev where the api process runs the workers in-process).
	if os.Getenv("WORKER_LEADER_DISABLED") != "1" {
		leader, err := database.AcquireLeader(ctx, cfg.DatabaseURL, "ironsight-worker-loops", 30*time.Second)
		if err != nil {
			log.Fatalf("[FATAL] leader election: %v", err)
		}
		defer leader.Release()
		// If the connection drops out from under us mid-flight, treat
		// it as a fatal signal: another standby will pick up
		// leadership, and we exit so the container restart loop
		// doesn't leave us in a half-leader state.
		go func() {
			<-leader.Lost()
			log.Println("[WORKER] leadership lost, initiating shutdown")
			cancel()
		}()
	} else {
		log.Println("[WORKER] WORKER_LEADER_DISABLED=1 — skipping advisory-lock leader election")
	}

	// ── Retention — hourly segment purge ─────────────────────────
	retentionMgr := recording.NewRetentionManager(db)
	go retentionMgr.Start(ctx)
	log.Println("[WORKER] Retention manager started")

	// ── VLM Indexer — AI enrichment of recorded segments ─────────
	vlmIndexer := indexer.New(cfg, db, aiClient)
	vlmIndexer.Start()
	log.Println("[WORKER] VLM indexer started")

	// ── Export worker — video concat jobs ────────────────────────
	exportWorker := export.NewWorker(cfg, db)
	exportWorker.Start(ctx)
	log.Println("[WORKER] Export worker started")

	// Graceful shutdown — signal arrives, cancel ctx, wait for workers
	// to finish. Retention respects ctx.Done() immediately; export worker
	// completes any in-flight FFmpeg then exits.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("[WORKER] Signal %v received, shutting down...", sig)
	case <-ctx.Done():
		log.Println("[WORKER] Context cancelled (likely lost leadership), shutting down...")
	}

	cancel()
	retentionMgr.Stop()
	vlmIndexer.Stop()
	exportWorker.Wait()

	log.Println("[WORKER] Clean shutdown complete")
}

// envOr is a tiny helper so the worker binary doesn't pull in config.Load()
// for every one-off env var. Redundant for values cfg already carries; useful
// for AI endpoints that the worker wants to override independently of the
// api's config.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
