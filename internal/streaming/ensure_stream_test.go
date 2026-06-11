package streaming

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/config"
)

// fakeMTX is a minimal stand-in for the mediamtx control API. It records
// the paths added via /v3/config/paths/add/{name} and serves them back from
// /v3/paths/list, so EnsureStreamRegistered can be exercised end-to-end
// without a real mediamtx. addDelay simulates the "path not present yet"
// window between the add call and the path appearing in the active list.
type fakeMTX struct {
	mu          sync.Mutex
	active      map[string]bool   // names visible in /v3/paths/list
	source      map[string]string // last source pushed per name (proves new URI lands)
	addCalls    map[string]int    // add attempts per name
	deleteCalls map[string]int    // delete attempts per name
	failAdd     bool              // when true, /add returns 500
	failDelete  bool              // when true, /delete returns 500
	appearAt    map[string]time.Time
}

func newFakeMTX() *fakeMTX {
	return &fakeMTX{
		active:      map[string]bool{},
		source:      map[string]string{},
		addCalls:    map[string]int{},
		deleteCalls: map[string]int{},
		appearAt:    map[string]time.Time{},
	}
}

func (f *fakeMTX) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/paths/list", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var sb strings.Builder
		sb.WriteString(`{"itemCount":0,"pageCount":1,"items":[`)
		first := true
		now := time.Now()
		for name := range f.active {
			if at, ok := f.appearAt[name]; ok && now.Before(at) {
				continue // not visible yet
			}
			if !first {
				sb.WriteString(",")
			}
			sb.WriteString(`{"name":"` + name + `"}`)
			first = false
		}
		sb.WriteString(`]}`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sb.String()))
	})
	mux.HandleFunc("/v3/config/paths/add/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/v3/config/paths/add/")
		var p struct {
			Source string `json:"source"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		f.addCalls[name]++
		fail := f.failAdd
		if !fail {
			f.active[name] = true
			f.source[name] = p.Source
		}
		f.mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v3/config/paths/delete/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/v3/config/paths/delete/")
		f.mu.Lock()
		f.deleteCalls[name]++
		fail := f.failDelete
		if !fail {
			delete(f.active, name)
			delete(f.source, name)
		}
		f.mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func newTestServer(t *testing.T, f *fakeMTX) *MediaMTXServer {
	t.Helper()
	ts := httptest.NewServer(f.handler())
	t.Cleanup(ts.Close)
	addr := strings.TrimPrefix(ts.URL, "http://")
	return &MediaMTXServer{
		cfg:     &config.Config{MediaMTXEmbedded: false, MediaMTXAPIAddr: addr},
		streams: map[uuid.UUID]streamInfo{},
	}
}

func TestEnsureStreamRegistered_EmbeddedIsNoOp(t *testing.T) {
	srv := &MediaMTXServer{cfg: &config.Config{MediaMTXEmbedded: true}, streams: map[uuid.UUID]streamInfo{}}
	if err := srv.EnsureStreamRegistered(context.Background(), uuid.New(), time.Second); err != nil {
		t.Fatalf("embedded mode must be a no-op, got %v", err)
	}
}

func TestEnsureStreamRegistered_RegistersMissingPath(t *testing.T) {
	f := newFakeMTX()
	srv := newTestServer(t, f)
	id := uuid.New()
	srv.streams[id] = streamInfo{cameraName: "cam", rtspURI: "rtsp://x/main", subStreamURI: "rtsp://x/sub"}

	if err := srv.EnsureStreamRegistered(context.Background(), id, 3*time.Second); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addCalls[id.String()] < 1 {
		t.Fatalf("expected main path to be added, addCalls=%d", f.addCalls[id.String()])
	}
	if f.addCalls[id.String()+"_sub"] < 1 {
		t.Fatalf("expected sub path to be added, addCalls=%d", f.addCalls[id.String()+"_sub"])
	}
}

func TestEnsureStreamRegistered_AlreadyActiveNoAdd(t *testing.T) {
	f := newFakeMTX()
	srv := newTestServer(t, f)
	id := uuid.New()
	srv.streams[id] = streamInfo{cameraName: "cam", rtspURI: "rtsp://x/main"}
	// Pre-seed the active list so the main path is already present.
	f.active[id.String()] = true

	if err := srv.EnsureStreamRegistered(context.Background(), id, 2*time.Second); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addCalls[id.String()] != 0 {
		t.Fatalf("path was already active — no add expected, got %d", f.addCalls[id.String()])
	}
}

func TestEnsureStreamRegistered_TimesOutNonFatal(t *testing.T) {
	f := newFakeMTX()
	f.failAdd = true // adds never make the path appear
	srv := newTestServer(t, f)
	id := uuid.New()
	srv.streams[id] = streamInfo{cameraName: "cam", rtspURI: "rtsp://x/main"}

	start := time.Now()
	err := srv.EnsureStreamRegistered(context.Background(), id, 600*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error when the path never appears")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout should be bounded by the deadline, took %s", elapsed)
	}
}

func TestEnsureStreamRegistered_UnknownCamera(t *testing.T) {
	f := newFakeMTX()
	srv := newTestServer(t, f)
	if err := srv.EnsureStreamRegistered(context.Background(), uuid.New(), time.Second); err == nil {
		t.Fatal("expected an error for a camera not in the stream map")
	}
}

// TestEnsureStreamRegistered_DelayedAppearance exercises the exact BUG-4 window:
// the path is added but doesn't show in /v3/paths/list immediately, so the
// poll loop must retry until it appears. Uses appearAt to delay visibility.
func TestEnsureStreamRegistered_DelayedAppearance(t *testing.T) {
	f := newFakeMTX()
	srv := newTestServer(t, f)
	id := uuid.New()
	srv.streams[id] = streamInfo{cameraName: "cam", rtspURI: "rtsp://x/main"}
	// The path becomes visible 500ms after it's added — the first poll(s)
	// must see it absent and keep retrying.
	f.appearAt[id.String()] = time.Now().Add(500 * time.Millisecond)

	if err := srv.EnsureStreamRegistered(context.Background(), id, 3*time.Second); err != nil {
		t.Fatalf("expected eventual success once the path appears, got %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addCalls[id.String()] < 1 {
		t.Fatalf("expected at least one add while waiting for the path, got %d", f.addCalls[id.String()])
	}
}

// TestReplaceStreamSource_StaleByNameForcesReplace is the MUST-FIX 1 regression
// guard: the path is ALREADY present under the camera UUID with the OLD source
// (a URI change keeps the same path name). EnsureStreamRegistered would see
// present-by-name and return without pushing anything. ReplaceStreamSource must
// instead DELETE the stale path and re-ADD it with the NEW source.
func TestReplaceStreamSource_StaleByNameForcesReplace(t *testing.T) {
	f := newFakeMTX()
	srv := newTestServer(t, f)
	id := uuid.New()
	// Stream map already holds the NEW source (handler called SetStreamSource).
	srv.streams[id] = streamInfo{cameraName: "cam", rtspURI: "rtsp://new/main", subStreamURI: "rtsp://new/sub"}
	// mediamtx still has the path present under the same name with the OLD source.
	f.active[id.String()] = true
	f.source[id.String()] = "rtsp://old/main"

	if err := srv.ReplaceStreamSource(context.Background(), id, 3*time.Second); err != nil {
		t.Fatalf("expected ReplaceStreamSource to succeed, got %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteCalls[id.String()] < 1 {
		t.Fatalf("expected the stale main path to be DELETED, deleteCalls=%d", f.deleteCalls[id.String()])
	}
	if f.deleteCalls[id.String()+"_sub"] < 1 {
		t.Fatalf("expected the stale sub path to be DELETED, deleteCalls=%d", f.deleteCalls[id.String()+"_sub"])
	}
	if f.addCalls[id.String()] < 1 {
		t.Fatalf("expected the main path to be re-ADDED, addCalls=%d", f.addCalls[id.String()])
	}
	// The new source must be what mediamtx now holds — proves the URI change took.
	if got := f.source[id.String()]; got != "rtsp://new/main" {
		t.Fatalf("expected main path source to be replaced with the NEW URI, got %q", got)
	}
	if got := f.source[id.String()+"_sub"]; got != "rtsp://new/sub" {
		t.Fatalf("expected sub path source to be replaced with the NEW URI, got %q", got)
	}
}

// TestReplaceStreamSource_DeleteUnreliableSurfaces verifies that if the
// mediamtx delete endpoint is unreliable (hangs/errors) on the deployed
// instance, ReplaceStreamSource surfaces a clear error (so the handler logs
// the "needs a reload" warning) rather than silently claiming success.
func TestReplaceStreamSource_DeleteUnreliableSurfaces(t *testing.T) {
	f := newFakeMTX()
	f.failDelete = true
	srv := newTestServer(t, f)
	id := uuid.New()
	srv.streams[id] = streamInfo{cameraName: "cam", rtspURI: "rtsp://new/main"}
	f.active[id.String()] = true
	f.source[id.String()] = "rtsp://old/main"

	err := srv.ReplaceStreamSource(context.Background(), id, time.Second)
	if err == nil {
		t.Fatal("expected an error when the mediamtx delete endpoint fails")
	}
	if !strings.Contains(err.Error(), "reload") {
		t.Fatalf("expected the error to flag that a mediamtx reload is needed, got %v", err)
	}
}

func TestReplaceStreamSource_EmbeddedIsNoOp(t *testing.T) {
	srv := &MediaMTXServer{cfg: &config.Config{MediaMTXEmbedded: true}, streams: map[uuid.UUID]streamInfo{}}
	if err := srv.ReplaceStreamSource(context.Background(), uuid.New(), time.Second); err != nil {
		t.Fatalf("embedded mode must be a no-op, got %v", err)
	}
}
