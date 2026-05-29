// Package metrics defines the Prometheus metric registry for the Ironsight
// API server. All instrumentation points import from this package and call
// the helper functions below — callers never touch prometheus.Registry
// directly, which keeps the registration side-effect-free in tests and makes
// the series catalog auditable from one file.
//
// Decision context: D-02 (docs/decisions.md) — self-hosted Prom + Grafana LXC.
// The /metrics endpoint is exposed by internal/api/router.go; this package
// owns only the metric definitions and the HTTP middleware.
package metrics

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry is the process-wide Prometheus registry. Using a non-default
// registry (instead of prometheus.DefaultRegisterer) means test code can
// call NewRegistry() and get a clean slate without sharing state between
// test runs or leaking the process-level Go runtime metrics into tests.
var Registry = prometheus.NewRegistry()

// ── HTTP request metrics ──────────────────────────────────────────────────

// HTTPRequestsTotal counts every completed HTTP request, labeled by chi
// route pattern, method, and status code. Route is the chi pattern (e.g.
// /cameras/{id}) not the resolved URL — that keeps cardinality bounded
// even with 90 cameras in the fleet.
var HTTPRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ironsight_http_requests_total",
		Help: "Total HTTP requests completed, by route pattern, method, and status code.",
	},
	[]string{"route", "method", "status"},
)

// HTTPRequestDuration observes each request's wall-clock latency, labeled
// by route pattern and method. No status label here — the histogram already
// gives a latency distribution; status breakdown is in the counter above.
var HTTPRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "ironsight_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by route pattern and method.",
		Buckets: prometheus.DefBuckets, // .005 .01 .025 .05 .1 .25 .5 1 2.5 5 10
	},
	[]string{"route", "method"},
)

// ── Recording engine metrics ──────────────────────────────────────────────

// RecordingActiveCameras is set on each reconciliation cycle of the
// recording engine. It reflects the number of cameras that currently
// have an active FFmpeg or gortsplib session.
var RecordingActiveCameras = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ironsight_recording_active_cameras",
	Help: "Number of cameras with an active recording session (FFmpeg or Go recorder).",
})

// RecordingFFmpegSubprocesses tracks the number of live FFmpeg child
// processes. May diverge from RecordingActiveCameras when cameras use
// the pure-Go gortsplib recorder (they count toward active cameras but
// not toward this gauge).
var RecordingFFmpegSubprocesses = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ironsight_recording_ffmpeg_subprocesses",
	Help: "Number of live FFmpeg child processes managed by the recording engine.",
})

// RecordingSegmentsWritten counts segments successfully inserted into the
// DB, labeled by camera_id. Cardinality: 90 cameras × 1 series = 90 series,
// well within safe limits. Do NOT add tenant_id or any unbounded label here.
var RecordingSegmentsWritten = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ironsight_recording_segments_written_total",
		Help: "Total recording segments inserted into the database, by camera ID.",
	},
	[]string{"camera_id"},
)

// ── Database pool metrics ─────────────────────────────────────────────────

// DBPoolAcquireCount is set to pgxpool.Stat().AcquireCount on each scrape.
// This is a cumulative count from the pool, reflected as a gauge because
// pgxpool exposes it as a total (not a delta), and promhttp scrapes it in
// place. Prom can still compute rate() on a gauge; the alternative
// (wrapping it as a counter) would require tracking a local delta.
var DBPoolAcquireCount = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ironsight_db_pool_acquire_count",
	Help: "Cumulative number of successful pgxpool connection acquisitions (from pgxpool.Stat.AcquireCount).",
})

// DBPoolIdle is set to pgxpool.Stat().IdleConns on each scrape.
var DBPoolIdle = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ironsight_db_pool_idle",
	Help: "Number of idle connections in the pgxpool (from pgxpool.Stat.IdleConns).",
})

// DBPoolTotal is set to pgxpool.Stat().TotalConns on each scrape.
var DBPoolTotal = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ironsight_db_pool_total",
	Help: "Total open connections in the pgxpool (from pgxpool.Stat.TotalConns).",
})

// ── WebSocket hub metrics ─────────────────────────────────────────────────

// WSClientsConnected tracks the current number of authenticated WebSocket
// clients. Set by the Hub on every connect and disconnect event.
var WSClientsConnected = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ironsight_ws_clients_connected",
	Help: "Number of currently connected and authenticated WebSocket clients.",
})

// ── RBAC cache metrics ────────────────────────────────────────────────────

// RBACCacheRefreshTotal counts every RBAC refresh cycle (the background
// goroutine that re-resolves per-client allowed-camera sets).
var RBACCacheRefreshTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "ironsight_rbac_cache_refresh_total",
	Help: "Total RBAC refresher cycles that have run.",
})

// RBACCacheRefreshErrors counts refresher cycles that encountered a DB
// error. On error the refresher keeps the prior cached set — this metric
// lets the operator see how often that fallback is exercised.
var RBACCacheRefreshErrors = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "ironsight_rbac_cache_refresh_errors_total",
	Help: "Total RBAC refresher cycles that hit a DB error and fell back to cached allow-set.",
})

// ── Boot / migration metrics ──────────────────────────────────────────────

// GooseMigrationVersion is set once at boot to the goose schema version
// the process applied. Useful for cross-referencing alerts with the schema
// state at the time they fired.
var GooseMigrationVersion = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ironsight_goose_migration_version",
	Help: "Goose schema migration version applied at startup.",
})

// ── Registration ──────────────────────────────────────────────────────────

func init() {
	// Collect standard Go runtime + process metrics (goroutines, GC,
	// memory, file descriptors). NewGoCollector and NewProcessCollector
	// are the v1.14+ replacements for the deprecated DefaultGoCollector
	// / DefaultProcessCollector functions.
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),

		HTTPRequestsTotal,
		HTTPRequestDuration,
		RecordingActiveCameras,
		RecordingFFmpegSubprocesses,
		RecordingSegmentsWritten,
		DBPoolAcquireCount,
		DBPoolIdle,
		DBPoolTotal,
		WSClientsConnected,
		RBACCacheRefreshTotal,
		RBACCacheRefreshErrors,
		GooseMigrationVersion,
	)
}

// ── Convenience setters (keep callers thin) ───────────────────────────────

// SetActiveCameras updates RecordingActiveCameras. Called by the recording
// engine on each reconciliation cycle.
func SetActiveCameras(n int) {
	RecordingActiveCameras.Set(float64(n))
}

// SetFFmpegSubprocesses updates RecordingFFmpegSubprocesses.
func SetFFmpegSubprocesses(n int) {
	RecordingFFmpegSubprocesses.Set(float64(n))
}

// IncSegmentsWritten increments RecordingSegmentsWritten for the given
// camera ID. Called by the recording engine after a successful InsertSegment.
func IncSegmentsWritten(cameraID string) {
	RecordingSegmentsWritten.WithLabelValues(cameraID).Inc()
}

// SetWSClients updates WSClientsConnected.
func SetWSClients(n int) {
	WSClientsConnected.Set(float64(n))
}

// IncRBACRefresh increments RBACCacheRefreshTotal.
func IncRBACRefresh() {
	RBACCacheRefreshTotal.Inc()
}

// IncRBACRefreshError increments RBACCacheRefreshErrors.
func IncRBACRefreshError() {
	RBACCacheRefreshErrors.Inc()
}

// SetGooseMigrationVersion records the applied migration version.
func SetGooseMigrationVersion(v int64) {
	GooseMigrationVersion.Set(float64(v))
}

// DBPoolStat holds the subset of pgxpool stats we expose. Callers fill
// this from db.Pool.Stat() and pass it to SyncDBPoolStats.
type DBPoolStat struct {
	AcquireCount int64
	IdleConns    int32
	TotalConns   int32
}

// SyncDBPoolStats updates the three DB pool gauges from a snapshot of
// pgxpool.Stat. Called by the DB pool stats collector goroutine started
// in cmd/server/main.go.
func SyncDBPoolStats(s DBPoolStat) {
	DBPoolAcquireCount.Set(float64(s.AcquireCount))
	DBPoolIdle.Set(float64(s.IdleConns))
	DBPoolTotal.Set(float64(s.TotalConns))
}

// ── HTTP middleware ───────────────────────────────────────────────────────

// HTTPMiddleware is a chi-compatible HTTP middleware that records
// ironsight_http_requests_total and ironsight_http_request_duration_seconds
// for every request that passes through it.
//
// Mount it BEFORE the logging middleware's recoverer so a panic that gets
// caught and converted to a 500 still increments the counter with status=500.
//
// Route labels: chi.RouteContext(ctx).RoutePattern() is used instead of
// r.URL.Path so that /cameras/{id}/recordings doesn't explode into 90
// separate series (one per camera UUID in the resolved path).
//
// If the chi route context is unavailable (e.g. a request that doesn't match
// any registered route) the label falls back to "unmatched".
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		// Read the route pattern AFTER the handler runs so chi has had
		// time to populate the route context via its trie lookup.
		route := "unmatched"
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if p := rctx.RoutePattern(); p != "" {
				route = p
			}
		}

		status := strconv.Itoa(rw.status)
		method := r.Method
		elapsed := time.Since(start).Seconds()

		HTTPRequestsTotal.WithLabelValues(route, method, status).Inc()
		HTTPRequestDuration.WithLabelValues(route, method).Observe(elapsed)
	})
}

// statusRecorder is a minimal ResponseWriter shim that captures the
// response status code for the metrics middleware. It mirrors the shim
// in internal/logging/middleware.go but is a separate type so the two
// packages remain independent (no import cycle).
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (sr *statusRecorder) WriteHeader(code int) {
	if sr.wrote {
		return
	}
	sr.status = code
	sr.wrote = true
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if !sr.wrote {
		sr.status = http.StatusOK
		sr.wrote = true
	}
	return sr.ResponseWriter.Write(b)
}

// Flush delegates to the underlying ResponseWriter so SSE / streaming
// endpoints are not broken by the metrics shim.
func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack delegates to the underlying ResponseWriter so WebSocket upgrades
// through the metrics middleware chain work correctly.
func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := sr.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("metrics middleware: underlying ResponseWriter does not support Hijack")
	}
	if !sr.wrote {
		// Mark status as 101 so the metrics label is informative.
		sr.status = http.StatusSwitchingProtocols
		sr.wrote = true
	}
	return h.Hijack()
}

// ReadFrom delegates to the underlying ResponseWriter's io.ReaderFrom
// when available. This is required because downstream wrappers
// (notably sentry-go's httputils.NewWrapResponseWriter) only preserve
// the Hijacker interface when the wrapped writer is "fully fancy" —
// implements Flusher AND Hijacker AND ReaderFrom. Without ReadFrom
// here, sentryhttp's wrapper falls back to a flush-only proxy that
// strips Hijack, breaking the WS upgrade chain. The interface check
// passes on the presence of the METHOD; the inner ResponseWriter does
// not need to support it (we'll use io.Copy as the fallback).
func (sr *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := sr.ResponseWriter.(io.ReaderFrom); ok {
		if !sr.wrote {
			sr.wrote = true
		}
		return rf.ReadFrom(src)
	}
	// Fallback: ordinary Write loop. This path is unusual because
	// the stdlib http.response always implements ReaderFrom.
	return io.Copy(writeOnlyWriter{sr}, src)
}

// writeOnlyWriter strips the ReadFrom method from a ResponseWriter so
// io.Copy doesn't infinite-loop into ReadFrom when fallback-copying.
type writeOnlyWriter struct{ http.ResponseWriter }

func (w writeOnlyWriter) Write(p []byte) (int, error) { return w.ResponseWriter.Write(p) }
