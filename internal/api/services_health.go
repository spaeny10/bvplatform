package api

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"onvif-tool/internal/ai"
	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
)

// ─────────────────────────────────────────────────────────────────────
// Service health surface
//
// Reports liveness of the four moving parts the operator dashboard
// can't see directly: the two GPU AI services (YOLO, Qwen), the
// mediamtx control API, and the worker process.
//
// Cameras and recording health already live behind /api/recording/health
// and /api/system/health. This endpoint covers the gap that bit us
// when the CDI driver duplication silently took yolo+qwen offline —
// the Go process kept logging "AI unreachable" to stderr and nothing
// surfaced it to the UI.
//
// Each probe runs in parallel with a 2-second hard timeout so a hung
// service can't block the page. The whole endpoint returns in well
// under a second even when everything is down.
// ─────────────────────────────────────────────────────────────────────

type ServiceStatus struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // "up", "down", "degraded", "unknown"
	Detail      string `json:"detail"`
	Endpoint    string `json:"endpoint,omitempty"`
	ResponseMs  int64  `json:"response_ms"`
	LastChecked string `json:"last_checked"`

	// GPU fields are populated only for AI services and only when the
	// service's /health endpoint reports them (pynvml present + CUDA
	// available). All four are optional.
	GPUUtilPct       *int `json:"gpu_util_pct,omitempty"`
	GPUMemoryUsedMB  *int `json:"gpu_memory_used_mb,omitempty"`
	GPUMemoryTotalMB *int `json:"gpu_memory_total_mb,omitempty"`
	GPUTemperatureC  *int `json:"gpu_temperature_c,omitempty"`
}

func HandleServicesHealth(cfg *config.Config, db *database.DB, aiClient *ai.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		yoloURL := envOrDefault("AI_YOLO_URL", "http://127.0.0.1:8501")
		qwenURL := envOrDefault("AI_QWEN_URL", "http://127.0.0.1:8502")
		mtxURL := "http://" + cfg.MediaMTXAPIAddr

		// Each probe is independent — fan out, then collect.
		var (
			wg      sync.WaitGroup
			results = make([]ServiceStatus, 4)
		)
		wg.Add(4)
		go func() { defer wg.Done(); results[0] = probeAI(ctx, "YOLO", yoloURL) }()
		go func() { defer wg.Done(); results[1] = probeAI(ctx, "Qwen VLM", qwenURL) }()
		go func() { defer wg.Done(); results[2] = probeMediaMTX(ctx, mtxURL) }()
		go func() { defer wg.Done(); results[3] = probeWorker(ctx, db) }()
		wg.Wait()

		payload := map[string]interface{}{
			"services":   results,
			"checked_at": time.Now().UTC().Format(time.RFC3339),
		}
		if aiClient != nil {
			payload["ai_stats"] = aiClient.Stats()
		}
		writeJSON(w, payload)
	}
}

// probeAI hits /health on a YOLO or Qwen service and parses the
// JSON body for model/GPU detail when the service responds.
func probeAI(ctx context.Context, name, base string) ServiceStatus {
	start := time.Now()
	st := ServiceStatus{
		Name:        name,
		Endpoint:    base,
		LastChecked: time.Now().UTC().Format(time.RFC3339),
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	st.ResponseMs = time.Since(start).Milliseconds()
	if err != nil {
		st.Status = "down"
		st.Detail = condenseErr(err)
		return st
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		st.Status = "degraded"
		st.Detail = "HTTP " + resp.Status
		return st
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)

	st.Status = "up"
	st.Detail = aiDetail(payload)

	// Pull GPU stats if the service emitted them. Pointer fields stay
	// nil if the service didn't include them (services without pynvml
	// or with NVML calls failing); the UI will hide the columns.
	if v, ok := payload["gpu_util_pct"].(float64); ok {
		i := int(v)
		st.GPUUtilPct = &i
	}
	if v, ok := payload["gpu_memory_used_mb"].(float64); ok {
		i := int(v)
		st.GPUMemoryUsedMB = &i
	}
	if v, ok := payload["gpu_memory_total_mb"].(float64); ok {
		i := int(v)
		st.GPUMemoryTotalMB = &i
	}
	if v, ok := payload["gpu_temperature_c"].(float64); ok {
		i := int(v)
		st.GPUTemperatureC = &i
	}
	return st
}

// aiDetail squeezes the most useful fields the YOLO/Qwen FastAPI
// /health handlers return into one short string for the UI. The
// Python side may evolve; we degrade silently when fields are missing.
func aiDetail(p map[string]interface{}) string {
	if p == nil {
		return "ready"
	}
	parts := []string{}
	if v, ok := p["model"].(string); ok && v != "" {
		parts = append(parts, v)
	}
	if v, ok := p["device"].(string); ok && v != "" {
		parts = append(parts, v)
	}
	if used, uok := p["gpu_memory_used_mb"].(float64); uok && used > 0 {
		if total, tok := p["gpu_memory_total_mb"].(float64); tok && total > 0 {
			parts = append(parts, fmt.Sprintf("%.1f/%.1fGB", used/1024, total/1024))
		} else {
			parts = append(parts, fmt.Sprintf("%.1fGB", used/1024))
		}
	}
	if util, uok := p["gpu_util_pct"].(float64); uok {
		parts = append(parts, fmt.Sprintf("%d%% util", int(util)))
	}
	if len(parts) == 0 {
		return "ready"
	}
	return strings.Join(parts, " · ")
}

// probeMediaMTX confirms the mediamtx control API is reachable. We
// don't need a 200 — a 401 proves the daemon is alive and answering
// HTTP, which is all we care about for liveness. Connection refused
// or a timeout means it's actually down.
func probeMediaMTX(ctx context.Context, base string) ServiceStatus {
	start := time.Now()
	st := ServiceStatus{
		Name:        "mediamtx",
		Endpoint:    base,
		LastChecked: time.Now().UTC().Format(time.RFC3339),
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v3/config/global/get", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	st.ResponseMs = time.Since(start).Milliseconds()
	if err != nil {
		st.Status = "down"
		st.Detail = condenseErr(err)
		return st
	}
	defer resp.Body.Close()
	st.Status = "up"
	st.Detail = "control API reachable"
	return st
}

// probeWorker checks whether the worker process is holding its
// advisory lock. This mirrors database.AcquireLeader's keying
// (fnv-1a 64 of "ironsight-worker-loops", sign bit dropped) and
// looks the lock up in pg_locks.
//
// "up" means the lock is held; if it isn't held, either no worker
// is running or the worker died holding stale state — both visible
// to the operator as "down".
func probeWorker(ctx context.Context, db *database.DB) ServiceStatus {
	start := time.Now()
	st := ServiceStatus{
		Name:        "worker",
		LastChecked: time.Now().UTC().Format(time.RFC3339),
	}

	// Replicate the leader-key derivation. Keep this in sync with
	// internal/database/leader.go — it's the identifier the worker
	// process uses when it calls pg_try_advisory_lock.
	h := fnv.New64a()
	_, _ = h.Write([]byte("ironsight-worker-loops"))
	keyInt := int64(h.Sum64() & 0x7FFFFFFFFFFFFFFF)

	// pg_try_advisory_lock(bigint) splits the 64-bit key into
	// classid (upper 32) and objid (lower 32) in pg_locks.
	// objsubid = 1 marks the single-key form. Cast oid → bigint
	// preserves the unsigned semantics correctly.
	var held bool
	err := db.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_locks
			WHERE locktype = 'advisory'
			  AND objsubid = 1
			  AND granted
			  AND ((classid::bigint << 32) | objid::bigint) = $1
		)`,
		keyInt,
	).Scan(&held)
	st.ResponseMs = time.Since(start).Milliseconds()

	if err != nil {
		st.Status = "unknown"
		st.Detail = "could not query pg_locks: " + condenseErr(err)
		return st
	}
	if held {
		st.Status = "up"
		st.Detail = "leader lock held"
		return st
	}
	st.Status = "down"
	st.Detail = "no worker is holding the advisory lock"
	return st
}

// condenseErr trims wrapped error chains down to the leaf message
// so we don't ship multi-line stack-y strings to the UI.
func condenseErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i < len(s)-2 {
		return s[i+2:]
	}
	return s
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// AIMetricSample is one row in the time-series response. ts is RFC3339;
// every other field is nullable because the sampler may have failed to
// reach the AI service for that window.
type AIMetricSample struct {
	Ts                string `json:"ts"`
	GPUUtilPct        *int   `json:"gpu_util_pct,omitempty"`
	GPUMemoryUsedMB   *int   `json:"gpu_memory_used_mb,omitempty"`
	GPUMemoryTotalMB  *int   `json:"gpu_memory_total_mb,omitempty"`
	GPUTemperatureC   *int   `json:"gpu_temperature_c,omitempty"`
	CallsDelta        int64  `json:"calls_delta"`
	ConfirmedDelta    int64  `json:"confirmed_delta"`
	FilteredDelta     int64  `json:"filtered_delta"`
	AvgInferenceMs    *int   `json:"avg_inference_ms,omitempty"`
}

// HandleAIMetricsTimeseries serves the recent samples for the AI
// runtime chart. Default window is 60 minutes; clamp at 24h to keep
// the response small (a 24h window at 30s sampling is 5,760 rows ×
// 2 services = ~12k rows, still under 1 MB JSON).
//
// GET /api/system/services/timeseries?minutes=60
func HandleAIMetricsTimeseries(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		minutes := 60
		if v := r.URL.Query().Get("minutes"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > 1440 {
					n = 1440
				}
				minutes = n
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		rows, err := db.Pool.Query(ctx, `
			SELECT ts, service, gpu_util_pct, gpu_memory_used_mb,
			       gpu_memory_total_mb, gpu_temperature_c,
			       calls_delta, confirmed_delta, filtered_delta, avg_inference_ms
			FROM ai_runtime_metrics
			WHERE site_id IS NULL
			  AND ts > now() - ($1 * INTERVAL '1 minute')
			ORDER BY ts ASC
		`, minutes)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		out := map[string][]AIMetricSample{
			"yolo": {},
			"qwen": {},
		}
		for rows.Next() {
			var ts time.Time
			var service string
			var util, used, total, temp, avgMs *int
			var calls, confirmed, filtered int64
			if err := rows.Scan(&ts, &service, &util, &used, &total, &temp, &calls, &confirmed, &filtered, &avgMs); err != nil {
				continue
			}
			s := AIMetricSample{
				Ts:               ts.UTC().Format(time.RFC3339),
				GPUUtilPct:       util,
				GPUMemoryUsedMB:  used,
				GPUMemoryTotalMB: total,
				GPUTemperatureC:  temp,
				CallsDelta:       calls,
				ConfirmedDelta:   confirmed,
				FilteredDelta:    filtered,
				AvgInferenceMs:   avgMs,
			}
			if _, ok := out[service]; !ok {
				out[service] = []AIMetricSample{}
			}
			out[service] = append(out[service], s)
		}

		writeJSON(w, map[string]interface{}{
			"window_minutes": minutes,
			"series":         out,
		})
	}
}

// SiteUsageRow is one row of the per-site AI usage breakdown.
type SiteUsageRow struct {
	SiteID         string  `json:"site_id"`
	SiteName       string  `json:"site_name"`
	YOLOCalls      int64   `json:"yolo_calls"`
	YOLOConfirmed  int64   `json:"yolo_confirmed"`
	YOLOFiltered   int64   `json:"yolo_filtered"`
	QwenCalls      int64   `json:"qwen_calls"`
	QwenConfirmed  int64   `json:"qwen_confirmed"`
	QwenFiltered   int64   `json:"qwen_filtered"`
	EstimatedCost  float64 `json:"estimated_cost"`
}

// HandleAIUsageBySite serves the per-site usage breakdown for the
// "GPU rental bill" view. Default window is 7 days; clamp at 90 days
// since hypertable performance starts to slow on full-table scans.
//
// `cost_per_1k_yolo` and `cost_per_1k_qwen` are query params (default
// 0.05 / 0.50 USD per 1k inferences) — admin overrides them in the UI
// to model their actual GPU/cloud cost. Storing these in DB settings
// is a future refinement; today they're just URL params so the UI can
// experiment without a write.
//
// GET /api/system/services/usage?days=7&cost_per_1k_yolo=0.05&cost_per_1k_qwen=0.50
func HandleAIUsageBySite(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := 7
		if v := r.URL.Query().Get("days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > 90 {
					n = 90
				}
				days = n
			}
		}
		costYolo, _ := strconv.ParseFloat(r.URL.Query().Get("cost_per_1k_yolo"), 64)
		if costYolo <= 0 {
			costYolo = 0.05
		}
		costQwen, _ := strconv.ParseFloat(r.URL.Query().Get("cost_per_1k_qwen"), 64)
		if costQwen <= 0 {
			costQwen = 0.50
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// Pivot service into yolo/qwen columns in one pass — TimescaleDB
		// handles the time-window predicate via the hypertable index.
		rows, err := db.Pool.Query(ctx, `
			SELECT
				m.site_id::text AS site_id,
				COALESCE(s.name, 'Unknown') AS site_name,
				SUM(CASE WHEN m.service = 'yolo' THEN m.calls_delta     ELSE 0 END) AS yolo_calls,
				SUM(CASE WHEN m.service = 'yolo' THEN m.confirmed_delta ELSE 0 END) AS yolo_confirmed,
				SUM(CASE WHEN m.service = 'yolo' THEN m.filtered_delta  ELSE 0 END) AS yolo_filtered,
				SUM(CASE WHEN m.service = 'qwen' THEN m.calls_delta     ELSE 0 END) AS qwen_calls,
				SUM(CASE WHEN m.service = 'qwen' THEN m.confirmed_delta ELSE 0 END) AS qwen_confirmed,
				SUM(CASE WHEN m.service = 'qwen' THEN m.filtered_delta  ELSE 0 END) AS qwen_filtered
			FROM ai_runtime_metrics m
			LEFT JOIN sites s ON s.id = m.site_id::text
			WHERE m.site_id IS NOT NULL
			  AND m.ts > now() - ($1 * INTERVAL '1 day')
			GROUP BY m.site_id, s.name
			ORDER BY (
				SUM(CASE WHEN m.service = 'yolo' THEN m.calls_delta ELSE 0 END) +
				SUM(CASE WHEN m.service = 'qwen' THEN m.calls_delta ELSE 0 END)
			) DESC
		`, days)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		out := []SiteUsageRow{}
		var totalCost float64
		for rows.Next() {
			var row SiteUsageRow
			if err := rows.Scan(
				&row.SiteID, &row.SiteName,
				&row.YOLOCalls, &row.YOLOConfirmed, &row.YOLOFiltered,
				&row.QwenCalls, &row.QwenConfirmed, &row.QwenFiltered,
			); err != nil {
				continue
			}
			row.EstimatedCost = (float64(row.YOLOCalls)/1000)*costYolo +
				(float64(row.QwenCalls)/1000)*costQwen
			totalCost += row.EstimatedCost
			out = append(out, row)
		}

		writeJSON(w, map[string]interface{}{
			"window_days":       days,
			"cost_per_1k_yolo":  costYolo,
			"cost_per_1k_qwen":  costQwen,
			"total_cost":        totalCost,
			"sites":             out,
		})
	}
}
