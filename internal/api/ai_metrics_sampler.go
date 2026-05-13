package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"ironsight/internal/ai"
	"ironsight/internal/database"
)

// StartAIMetricsSampler runs a background loop that snapshots the AI
// runtime counters and live GPU stats every `interval` and persists one
// row per service into the ai_runtime_metrics hypertable.
//
// Counters are stored as deltas (calls/confirmed/filtered since the
// previous tick) — that's what makes range queries correct after the
// api process restarts and resets the in-memory atomics. GPU fields
// are point-in-time absolutes.
//
// The sampler shares the api process so it can read ai.Client's
// counters without any IPC, and it dies cleanly with the api when the
// parent context is cancelled.
func StartAIMetricsSampler(parent context.Context, db *database.DB, aiClient *ai.Client, yoloURL, qwenURL string, interval time.Duration) {
	if aiClient == nil || db == nil {
		return
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}

	go func() {
		// Previous-tick snapshots. Global counters drive the GPU-anchored
		// rows; the per-site map drives the additional per-site rows.
		var prev ai.AIStats
		prevSite := map[string]ai.SiteAIStats{}

		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-parent.Done():
				return
			case <-t.C:
				now := time.Now().UTC()
				cur := aiClient.Stats()

				// If a counter went backwards (process restart, running
				// in a tight loop after a crash), treat the new value as
				// the new baseline instead of writing a negative delta.
				yCalls := cur.YOLOCalls - prev.YOLOCalls
				yConf := cur.YOLOConfirmed - prev.YOLOConfirmed
				yFilt := cur.YOLOFiltered - prev.YOLOFiltered
				if yCalls < 0 || yConf < 0 || yFilt < 0 {
					yCalls, yConf, yFilt = 0, 0, 0
				}
				qCalls := cur.QwenCalls - prev.QwenCalls
				qConf := cur.QwenConfirmed - prev.QwenConfirmed
				qFilt := cur.QwenFiltered - prev.QwenFiltered
				if qCalls < 0 || qConf < 0 || qFilt < 0 {
					qCalls, qConf, qFilt = 0, 0, 0
				}
				prev = cur

				yGPU := fetchGPUStats(parent, yoloURL)
				qGPU := fetchGPUStats(parent, qwenURL)

				// Global rows (site_id NULL) — anchor the GPU readings
				// and the site-less aggregate counters.
				if err := writeAIMetric(parent, db, now, "yolo", "", yCalls, yConf, yFilt, cur.YOLOAvgMs, yGPU); err != nil {
					log.Printf("[AI-METRICS] write yolo: %v", err)
				}
				if err := writeAIMetric(parent, db, now, "qwen", "", qCalls, qConf, qFilt, cur.QwenAvgMs, qGPU); err != nil {
					log.Printf("[AI-METRICS] write qwen: %v", err)
				}

				// Per-site rows. Only emit when the site had activity in
				// this window — silent sites would just bloat the table.
				curSite := aiClient.SiteStatsSnapshot()
				newPrev := map[string]ai.SiteAIStats{}
				for _, s := range curSite {
					p := prevSite[s.SiteID]
					sYC := s.YOLOCalls - p.YOLOCalls
					sYO := s.YOLOConfirmed - p.YOLOConfirmed
					sYF := s.YOLOFiltered - p.YOLOFiltered
					sQC := s.QwenCalls - p.QwenCalls
					sQO := s.QwenConfirmed - p.QwenConfirmed
					sQF := s.QwenFiltered - p.QwenFiltered
					if sYC < 0 || sYO < 0 || sYF < 0 {
						sYC, sYO, sYF = 0, 0, 0
					}
					if sQC < 0 || sQO < 0 || sQF < 0 {
						sQC, sQO, sQF = 0, 0, 0
					}
					if sYC > 0 || sYO > 0 || sYF > 0 {
						_ = writeAIMetric(parent, db, now, "yolo", s.SiteID, sYC, sYO, sYF, s.YOLOAvgMs, gpuStats{})
					}
					if sQC > 0 || sQO > 0 || sQF > 0 {
						_ = writeAIMetric(parent, db, now, "qwen", s.SiteID, sQC, sQO, sQF, s.QwenAvgMs, gpuStats{})
					}
					newPrev[s.SiteID] = s
				}
				prevSite = newPrev
			}
		}
	}()
}

type gpuStats struct {
	UtilPct      *int
	MemoryUsedMB *int
	MemoryTotMB  *int
	TempC        *int
}

func fetchGPUStats(ctx context.Context, base string) gpuStats {
	var out gpuStats
	if base == "" {
		return out
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, base+"/health", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return out
	}
	if v, ok := payload["gpu_util_pct"].(float64); ok {
		i := int(v)
		out.UtilPct = &i
	}
	if v, ok := payload["gpu_memory_used_mb"].(float64); ok {
		i := int(v)
		out.MemoryUsedMB = &i
	}
	if v, ok := payload["gpu_memory_total_mb"].(float64); ok {
		i := int(v)
		out.MemoryTotMB = &i
	}
	if v, ok := payload["gpu_temperature_c"].(float64); ok {
		i := int(v)
		out.TempC = &i
	}
	return out
}

func writeAIMetric(ctx context.Context, db *database.DB, ts time.Time, service, siteID string, calls, confirmed, filtered, avgMs int64, gpu gpuStats) error {
	// site_id is NULL for global rows (the GPU-anchored aggregate); use
	// a typed nil so pgx writes UUID NULL rather than the literal string.
	var siteParam interface{}
	if siteID != "" {
		siteParam = siteID
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO ai_runtime_metrics
			(ts, service, site_id, gpu_util_pct, gpu_memory_used_mb, gpu_memory_total_mb,
			 gpu_temperature_c, calls_delta, confirmed_delta, filtered_delta, avg_inference_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		ts, service, siteParam,
		gpu.UtilPct, gpu.MemoryUsedMB, gpu.MemoryTotMB, gpu.TempC,
		calls, confirmed, filtered, avgMs,
	)
	return err
}
