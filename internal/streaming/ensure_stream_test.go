package streaming

import (
	"context"
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
	mu        sync.Mutex
	active    map[string]bool // names visible in /v3/paths/list
	addCalls  map[string]int  // add attempts per name
	failAdd   bool            // when true, /add returns 500
	appearAt  map[string]time.Time
}

func newFakeMTX() *fakeMTX {
	return &fakeMTX{active: map[string]bool{}, addCalls: map[string]int{}, appearAt: map[string]time.Time{}}
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
		f.mu.Lock()
		f.addCalls[name]++
		fail := f.failAdd
		if !fail {
			f.active[name] = true
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
