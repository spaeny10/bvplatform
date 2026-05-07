package streaming

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"onvif-tool/internal/config"
)

// MediaMTXServer manages a MediaMTX process for WebRTC streaming
type MediaMTXServer struct {
	cfg        *config.Config
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	mu         sync.Mutex
	streams    map[uuid.UUID]streamInfo
	configPath string
	binPath    string
	whepProxy  *httputil.ReverseProxy
}

type streamInfo struct {
	cameraName   string
	rtspURI      string
	subStreamURI string
}

// mediamtx YAML config structures
type mtxConfig struct {
	LogLevel              string              `yaml:"logLevel"`
	LogDestinations       []string            `yaml:"logDestinations"`
	API                   bool                `yaml:"api"`
	APIAddress            string              `yaml:"apiAddress"`
	RTSP                  bool                `yaml:"rtsp"`
	RTSPAddress           string              `yaml:"rtspAddress"`
	RTMP                  bool                `yaml:"rtmp"`
	RTMPAddress           string              `yaml:"rtmpAddress"`
	HLS                   bool                `yaml:"hls"`
	HLSAddress            string              `yaml:"hlsAddress"`
	WebRTC                bool                `yaml:"webrtc"`
	WebRTCAddress         string              `yaml:"webrtcAddress"`
	WebRTCAdditionalHosts []string            `yaml:"webrtcAdditionalHosts,omitempty"`
	WebRTCICEServers2     []interface{}       `yaml:"webrtcICEServers2"`
	Paths                 map[string]*mtxPath `yaml:"paths"`
}

type mtxPath struct {
	Source         string `yaml:"source"`
	RTSPTransport  string `yaml:"rtspTransport"`
	SourceOnDemand bool   `yaml:"sourceOnDemand"`
}

// NewMediaMTXServer creates a new MediaMTX server manager. In embedded mode
// (cfg.MediaMTXEmbedded, the default for dev) we spawn a local mediamtx
// binary from ./bin/ and write its config there. In external mode — used
// by docker-compose and every production deployment — mediamtx runs in its
// own container and the Go process only needs the WHEP proxy address.
func NewMediaMTXServer(cfg *config.Config) *MediaMTXServer {
	cwd, _ := os.Getwd()
	binName := "mediamtx"
	if filepathSeparator() == '\\' {
		binName = "mediamtx.exe"
	}
	binPath := filepath.Join(cwd, "bin", binName)
	configPath := filepath.Join(cwd, "bin", "mediamtx_runtime.yml")

	// Reverse proxy to MediaMTX's WHEP endpoint — target host is whatever
	// docker-compose / the operator configured (defaults to 127.0.0.1:8889
	// for single-process dev).
	targetURL := "http://" + cfg.MediaMTXWebRTCAddr
	if !strings.HasPrefix(cfg.MediaMTXWebRTCAddr, "http") {
		targetURL = "http://" + cfg.MediaMTXWebRTCAddr
	} else {
		targetURL = cfg.MediaMTXWebRTCAddr
	}
	target, _ := url.Parse(targetURL)
	proxy := httputil.NewSingleHostReverseProxy(target)

	return &MediaMTXServer{
		cfg:        cfg,
		streams:    make(map[uuid.UUID]streamInfo),
		configPath: configPath,
		binPath:    binPath,
		whepProxy:  proxy,
	}
}

// filepathSeparator returns the OS path separator. Split out as a func so
// the binary-name selection above is testable without importing runtime
// directly in every caller.
func filepathSeparator() rune { return filepath.Separator }

// listenPortSuffix extracts ":port" from a "host:port" pair. The mediamtx
// YAML config takes listen addresses in the ":port" form (bind on all
// interfaces); we only configure the host side of MEDIAMTX_*_ADDR so the
// Go process knows where to *reach* mediamtx, not where it listens. Falls
// back to ":fallback" when the address is malformed.
func listenPortSuffix(hostPort, fallback string) string {
	if hostPort == "" {
		return ":" + fallback
	}
	// Strip any scheme prefix someone pasted by habit (e.g., "http://host:8889").
	if i := strings.Index(hostPort, "://"); i >= 0 {
		hostPort = hostPort[i+3:]
	}
	if i := strings.LastIndex(hostPort, ":"); i >= 0 {
		return hostPort[i:]
	}
	return ":" + fallback
}

// AddStream registers a camera stream with MediaMTX at runtime.
//
// The in-memory map is the source of truth for what writeConfig() emits
// on bootstrap. The HTTP control API is how we tell a *running* MediaMTX
// about the new path without a restart — that's the change in Phase 2.
// If the API call fails (e.g., MediaMTX not yet ready), we still keep
// the in-memory entry so the next Restart()/bootstrap picks it up.
func (m *MediaMTXServer) AddStream(cameraID uuid.UUID, cameraName, rtspURI, subStreamURI string) {
	m.mu.Lock()
	m.streams[cameraID] = streamInfo{
		cameraName:   cameraName,
		rtspURI:      rtspURI,
		subStreamURI: subStreamURI,
	}
	m.mu.Unlock()

	// Push to the live API. Background context is fine — the helper has
	// its own 5s timeout. We don't want to block the caller (HTTP handler)
	// on a flaky MediaMTX.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := m.apiAddPath(ctx, cameraID.String(), mtxAPIPath{
			Source:         rtspURI,
			SourceOnDemand: false,
			RTSPTransport:  "tcp",
		}); err != nil {
			log.Printf("[MEDIAMTX] AddStream API: main path for %s failed: %v", cameraName, err)
		}
		if subStreamURI != "" {
			if err := m.apiAddPath(ctx, cameraID.String()+"_sub", mtxAPIPath{
				Source:         subStreamURI,
				SourceOnDemand: true,
				RTSPTransport:  "tcp",
			}); err != nil {
				log.Printf("[MEDIAMTX] AddStream API: sub path for %s failed: %v", cameraName, err)
			}
		}
	}()
}

// RemoveStream deletes a camera stream from MediaMTX at runtime.
// Same failure-tolerance rule as AddStream — if the API call fails, the
// in-memory map is still updated so the next bootstrap reflects reality.
func (m *MediaMTXServer) RemoveStream(cameraID uuid.UUID) {
	m.mu.Lock()
	delete(m.streams, cameraID)
	m.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.apiRemovePath(ctx, cameraID.String()); err != nil {
			log.Printf("[MEDIAMTX] RemoveStream API: main path for %s failed: %v", cameraID, err)
		}
		if err := m.apiRemovePath(ctx, cameraID.String()+"_sub"); err != nil {
			log.Printf("[MEDIAMTX] RemoveStream API: sub path for %s failed: %v", cameraID, err)
		}
	}()
}

// Start generates the config and launches MediaMTX — unless the server is
// configured to run externally (docker-compose sibling, K8s sidecar), in
// which case we just log and let the caller use the WHEP proxy + config
// writes against the shared volume.
func (m *MediaMTXServer) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.cfg.MediaMTXEmbedded {
		// External mode: another container owns the mediamtx lifecycle.
		// Write the bootstrap config to the shared volume so the mediamtx
		// container has a starting state on first run (API enabled, any
		// pre-existing streams). Runtime changes after this go through
		// the HTTP control API (see apiAddPath/apiRemovePath), not config
		// rewrites — mediamtx doesn't auto-reload its YAML anyway.
		if err := m.writeConfig(); err != nil {
			return fmt.Errorf("write mediamtx config: %w", err)
		}
		log.Printf("[MEDIAMTX] External mode — reaching mediamtx at WHEP=%s, RTSP=%s, API=%s. Bootstrap config at %s.",
			m.cfg.MediaMTXWebRTCAddr, m.cfg.MediaMTXRTSPAddr, m.cfg.MediaMTXAPIAddr, m.configPath)

		// Don't block server start on mediamtx being up — in compose the
		// api and mediamtx containers come up concurrently. Probe the API
		// in the background and log once it answers, so operators can see
		// in the logs when runtime control becomes available.
		go func() {
			waitCtx, waitCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer waitCancel()
			if err := m.apiReady(waitCtx); err != nil {
				log.Printf("[MEDIAMTX] Control API still unreachable after 60s (%v) — runtime AddStream/RemoveStream will fail until mediamtx is up.", err)
				return
			}
			log.Printf("[MEDIAMTX] Control API ready at %s — runtime path updates will apply without restart.", m.cfg.MediaMTXAPIAddr)
		}()
		return nil
	}

	// Kill any orphaned mediamtx process from a previous server run. The
	// implementation is platform-specific — Windows needs taskkill; Unix
	// relies on the container/init reaping children (see the build-tagged
	// mediamtx_kill_*.go files).
	killOrphanMediaMTXProcess(m.binPath)

	if err := m.writeConfig(); err != nil {
		return fmt.Errorf("write mediamtx config: %w", err)
	}

	childCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	m.cmd = exec.CommandContext(childCtx, m.binPath, m.configPath)

	// Redirect MediaMTX output to a log file instead of sharing the parent's
	// noisy stdout (FFmpeg HLS output can block child process writes).
	logPath := filepath.Join(filepath.Dir(m.binPath), "mediamtx.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("[MEDIAMTX] Warning: could not open log file %s: %v — using discard", logPath, err)
		m.cmd.Stdout = nil
		m.cmd.Stderr = nil
	} else {
		m.cmd.Stdout = logFile
		m.cmd.Stderr = logFile
	}

	if err := m.cmd.Start(); err != nil {
		cancel()
		if logFile != nil {
			logFile.Close()
		}
		return fmt.Errorf("start mediamtx: %w", err)
	}

	log.Printf("[MEDIAMTX] Started (PID %d) with %d streams, log: %s", m.cmd.Process.Pid, len(m.streams), logPath)

	// Probe the HTTP control API in the background — once it answers,
	// runtime AddStream/RemoveStream calls will land. Embedded spawn
	// typically reaches "ready" in under a second.
	go func() {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer waitCancel()
		if err := m.apiReady(waitCtx); err == nil {
			log.Printf("[MEDIAMTX] Control API ready at %s", m.cfg.MediaMTXAPIAddr)
		}
	}()

	// Watch for unexpected exit and auto-restart (unlimited)
	go func() {
		err := m.cmd.Wait()
		if logFile != nil {
			logFile.Close()
		}
		if childCtx.Err() != nil {
			return // deliberate shutdown
		}
		log.Printf("[MEDIAMTX] Process exited unexpectedly: %v — will auto-restart", err)

		restartCount := 0
		for {
			restartCount++
			time.Sleep(2 * time.Second)
			if childCtx.Err() != nil {
				return
			}

			m.mu.Lock()
			if err := m.writeConfig(); err != nil {
				m.mu.Unlock()
				log.Printf("[MEDIAMTX] Failed to write config on restart #%d: %v", restartCount, err)
				continue
			}
			m.cmd = exec.CommandContext(childCtx, m.binPath, m.configPath)
			lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				m.cmd.Stdout = nil
				m.cmd.Stderr = nil
			} else {
				m.cmd.Stdout = lf
				m.cmd.Stderr = lf
			}
			if err := m.cmd.Start(); err != nil {
				m.mu.Unlock()
				if lf != nil {
					lf.Close()
				}
				log.Printf("[MEDIAMTX] Restart #%d failed to start: %v", restartCount, err)
				continue
			}
			log.Printf("[MEDIAMTX] Restarted successfully (PID %d, restart #%d)", m.cmd.Process.Pid, restartCount)
			m.mu.Unlock()

			// Wait for this instance to exit
			err = m.cmd.Wait()
			if lf != nil {
				lf.Close()
			}
			if childCtx.Err() != nil {
				return // deliberate shutdown
			}
			log.Printf("[MEDIAMTX] Process crashed again (restart #%d): %v — restarting...", restartCount, err)
		}
	}()

	return nil
}

// PersistConfig rewrites the bootstrap YAML so a future mediamtx restart
// (container OOM, scheduled upgrade, dev hot-reload) comes back with the
// current set of paths. Runtime path changes already applied via the
// HTTP API — this is the durability side of the same story.
//
// Prefer PersistConfig over Restart in the camera CRUD path: the old
// behaviour was to fully restart the mediamtx process after every
// AddStream, which dropped every live WebRTC session on the floor for
// no good reason now that paths register via API.
func (m *MediaMTXServer) PersistConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeConfig()
}

// Restart regenerates config and restarts the process. In embedded mode
// this kills and respawns the mediamtx binary — still useful for full
// recovery (e.g., the process wedged and stopped accepting API calls).
// In external mode we can't touch the container's lifecycle, so Restart
// degrades to a config rewrite; the operator must bounce the container
// separately if they need a full restart.
//
// For routine AddStream/RemoveStream, use PersistConfig — it's cheaper
// and doesn't disrupt live streams.
func (m *MediaMTXServer) Restart(ctx context.Context) error {
	if !m.cfg.MediaMTXEmbedded {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.writeConfig()
	}
	m.Stop()
	return m.Start(ctx)
}

// Stop terminates MediaMTX (embedded mode only — no-op when external).
func (m *MediaMTXServer) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.cfg.MediaMTXEmbedded {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Kill()
	}
	log.Println("[MEDIAMTX] Stopped")
}

// WHEPHandler returns an HTTP handler that proxies WHEP requests to MediaMTX
func (m *MediaMTXServer) WHEPHandler() http.Handler {
	return m.whepProxy
}

// writeConfig generates the mediamtx YAML config from registered streams
func (m *MediaMTXServer) writeConfig() error {
	paths := make(map[string]*mtxPath)

	for id, info := range m.streams {
		// Main stream path — persistent connection so the local RTSP relay
		// is always available for recording without opening new camera sessions.
		paths[id.String()] = &mtxPath{
			Source:         info.rtspURI,
			RTSPTransport:  "tcp",
			SourceOnDemand: false,
		}

		// Sub stream path (on-demand — only needed for live view adaptive bitrate)
		if info.subStreamURI != "" {
			paths[id.String()+"_sub"] = &mtxPath{
				Source:         info.subStreamURI,
				RTSPTransport:  "tcp",
				SourceOnDemand: true,
			}
		}
	}

	// Listen addresses come from config. Drop the host and keep only the
	// port so mediamtx binds on 0.0.0.0 inside the container; the hostname
	// side of MediaMTXRTSPAddr/MediaMTXWebRTCAddr is how the *Go* process
	// reaches mediamtx, not how mediamtx listens.
	cfg := &mtxConfig{
		LogLevel:        "info",
		LogDestinations: []string{"stdout"},
		// Enable the HTTP control API so runtime path adds/removes go
		// through apiAddPath/apiRemovePath instead of config-rewrite +
		// reload. See internal/streaming/mediamtx_api.go.
		API:                   true,
		APIAddress:            listenPortSuffix(m.cfg.MediaMTXAPIAddr, "9997"),
		RTSP:                  true, // Local RTSP relay — recording engine pulls from here instead of opening new camera connections
		RTSPAddress:           listenPortSuffix(m.cfg.MediaMTXRTSPAddr, "18554"),
		RTMP:                  false,
		RTMPAddress:           ":11935",
		HLS:                   false, // We don't need HLS from MediaMTX
		HLSAddress:            ":18888",
		WebRTC:                true,
		WebRTCAddress:         listenPortSuffix(m.cfg.MediaMTXWebRTCAddr, "8889"),
		WebRTCAdditionalHosts: m.cfg.WebRTCAdditionalHosts,
		Paths:                 paths,
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(m.configPath, data, 0644)
}
