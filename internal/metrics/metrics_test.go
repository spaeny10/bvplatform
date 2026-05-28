package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"ironsight/internal/metrics"
)

// newTestRegistry returns a fresh Prometheus registry pre-populated with
// the same metrics as the package-level Registry. Used so each test gets
// isolated counters instead of accumulating across the test binary run.
//
// Because the package-level vars are already registered to Registry (via
// init()), tests that want isolation should create new metric instances
// and a new registry. We keep it simple: just use the package-level Registry
// and rely on absolute value checks rather than delta checks.

// TestHTTPMiddleware_CounterIncrement verifies that one request increments
// HTTPRequestsTotal by exactly 1 for the observed (route, method, status) triple.
func TestHTTPMiddleware_CounterIncrement(t *testing.T) {
	// Snapshot counter value before the request.
	before := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/api/health", "GET", "200"))

	// Build a chi router with the middleware and a simple handler.
	r := chi.NewRouter()
	r.Use(metrics.HTTPMiddleware)
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	after := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/api/health", "GET", "200"))
	if delta := after - before; delta != 1 {
		t.Errorf("expected counter delta 1, got %.0f", delta)
	}
}

// TestHTTPMiddleware_HistogramObserved verifies that the histogram recorded
// a positive count after a request (i.e. the Observe call happened and
// elapsed >= 0). We use Registry.Gather() and inspect the protobuf output
// because testutil.ToFloat64 only works on scalar collectors, not
// *HistogramVec.
func TestHTTPMiddleware_HistogramObserved(t *testing.T) {
	// Take a pre-request snapshot of the sample count for this label pair.
	// We count how many histogram samples had route=/api/health before and
	// after — the delta must be exactly 1.
	countBefore := histSampleCount(t, "/api/health", "GET")

	r := chi.NewRouter()
	r.Use(metrics.HTTPMiddleware)
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	countAfter := histSampleCount(t, "/api/health", "GET")
	if delta := countAfter - countBefore; delta != 1 {
		t.Errorf("expected histogram sample count to increase by 1, got %d", delta)
	}
}

// histSampleCount gathers the ironsight_http_request_duration_seconds
// histogram from the package Registry and returns the SampleCount for the
// label pair (route, method). Returns 0 if the series hasn't been observed
// yet.
func histSampleCount(t *testing.T, route, method string) uint64 {
	t.Helper()
	mfs, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "ironsight_http_request_duration_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := make(map[string]string)
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			if labels["route"] == route && labels["method"] == method {
				if h := m.GetHistogram(); h != nil {
					return h.GetSampleCount()
				}
			}
		}
	}
	return 0
}

// TestHTTPMiddleware_RoutePattern verifies that the chi route pattern label
// is used instead of the resolved URL path.
// Route registered as /cameras/{id}; request hits /cameras/abc-123.
// The counter label must be route=/cameras/{id} NOT /cameras/abc-123.
func TestHTTPMiddleware_RoutePattern(t *testing.T) {
	pattern := "/cameras/{id}"
	resolvedPath := "/cameras/abc-123"

	// Snapshot the pattern-label counter before.
	before := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues(pattern, "GET", "200"))
	// The resolved-path counter must stay at 0.
	beforeResolved := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues(resolvedPath, "GET", "200"))

	r := chi.NewRouter()
	r.Use(metrics.HTTPMiddleware)
	r.Get(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, resolvedPath, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	afterPattern := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues(pattern, "GET", "200"))
	afterResolved := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues(resolvedPath, "GET", "200"))

	if delta := afterPattern - before; delta != 1 {
		t.Errorf("expected pattern-label counter to increment by 1, got %.0f", delta)
	}
	if afterResolved != beforeResolved {
		t.Errorf("expected resolved-path counter to stay unchanged, got %.0f (before %.0f)", afterResolved, beforeResolved)
	}
}

// TestHTTPMiddleware_StatusLabels verifies that a 200 and a 500 produce
// separate series labels.
func TestHTTPMiddleware_StatusLabels(t *testing.T) {
	before200 := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/test/status", "GET", "200"))
	before500 := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/test/status", "GET", "500"))

	r := chi.NewRouter()
	r.Use(metrics.HTTPMiddleware)
	r.Get("/test/status", func(w http.ResponseWriter, req *http.Request) {
		// Return 500 if query param says so, else 200.
		if req.URL.Query().Get("fail") == "1" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// 200 request
	req200 := httptest.NewRequest(http.MethodGet, "/test/status", nil)
	r.ServeHTTP(httptest.NewRecorder(), req200)

	// 500 request
	req500 := httptest.NewRequest(http.MethodGet, "/test/status?fail=1", nil)
	r.ServeHTTP(httptest.NewRecorder(), req500)

	after200 := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/test/status", "GET", "200"))
	after500 := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/test/status", "GET", "500"))

	if delta := after200 - before200; delta != 1 {
		t.Errorf("200 counter: expected delta 1, got %.0f", delta)
	}
	if delta := after500 - before500; delta != 1 {
		t.Errorf("500 counter: expected delta 1, got %.0f", delta)
	}
}

// TestGaugeSetGet verifies that SetActiveCameras round-trips through
// RecordingActiveCameras correctly.
func TestGaugeSetGet(t *testing.T) {
	metrics.SetActiveCameras(42)
	got := testutil.ToFloat64(metrics.RecordingActiveCameras)
	if got != 42 {
		t.Errorf("SetActiveCameras(42): got %.0f", got)
	}

	metrics.SetActiveCameras(0)
	got = testutil.ToFloat64(metrics.RecordingActiveCameras)
	if got != 0 {
		t.Errorf("SetActiveCameras(0): got %.0f", got)
	}
}

// TestDBPoolStats verifies that SyncDBPoolStats updates all three pool gauges.
func TestDBPoolStats(t *testing.T) {
	stat := metrics.DBPoolStat{
		AcquireCount: 1234,
		IdleConns:    3,
		TotalConns:   10,
	}
	metrics.SyncDBPoolStats(stat)

	if got := testutil.ToFloat64(metrics.DBPoolAcquireCount); got != 1234 {
		t.Errorf("DBPoolAcquireCount: want 1234, got %.0f", got)
	}
	if got := testutil.ToFloat64(metrics.DBPoolIdle); got != 3 {
		t.Errorf("DBPoolIdle: want 3, got %.0f", got)
	}
	if got := testutil.ToFloat64(metrics.DBPoolTotal); got != 10 {
		t.Errorf("DBPoolTotal: want 10, got %.0f", got)
	}
}

// TestSegmentsWrittenCounter verifies IncSegmentsWritten increments the
// per-camera-id counter.
func TestSegmentsWrittenCounter(t *testing.T) {
	camID := "test-cam-uuid-001"
	before := testutil.ToFloat64(metrics.RecordingSegmentsWritten.WithLabelValues(camID))

	metrics.IncSegmentsWritten(camID)
	metrics.IncSegmentsWritten(camID)

	after := testutil.ToFloat64(metrics.RecordingSegmentsWritten.WithLabelValues(camID))
	if delta := after - before; delta != 2 {
		t.Errorf("expected 2 segment increments, got %.0f", delta)
	}
}

// TestRegistryExposes verifies that the package-level Registry can gather
// all registered metrics and that the output contains expected metric names.
func TestRegistryExposes(t *testing.T) {
	mfs, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather: %v", err)
	}

	got := make(map[string]bool)
	for _, mf := range mfs {
		got[mf.GetName()] = true
	}

	want := []string{
		"ironsight_http_requests_total",
		"ironsight_http_request_duration_seconds",
		"ironsight_recording_active_cameras",
		"ironsight_recording_ffmpeg_subprocesses",
		"ironsight_recording_segments_written_total",
		"ironsight_db_pool_acquire_count",
		"ironsight_db_pool_idle",
		"ironsight_db_pool_total",
		"ironsight_ws_clients_connected",
		"ironsight_rbac_cache_refresh_total",
		"ironsight_rbac_cache_refresh_errors_total",
		"ironsight_goose_migration_version",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("metric %q not found in Registry output", name)
		}
	}

	// Also verify Go runtime metrics are present (collectors.NewGoCollector).
	foundGo := false
	for name := range got {
		if strings.HasPrefix(name, "go_") {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Error("no go_* metrics found — NewGoCollector may not be registered")
	}
}
