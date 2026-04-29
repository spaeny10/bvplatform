// Package ai provides clients for the YOLO detection and Qwen vLM reasoning
// services. The pipeline is event-triggered: camera metadata fires an event,
// the VMS grabs a snapshot, sends it through YOLO → Qwen, and enriches the
// SOC alarm with AI-generated intelligence.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds AI service endpoints.
type Config struct {
	YOLOEndpoint string // e.g. "http://127.0.0.1:8501"
	QwenEndpoint string // e.g. "http://127.0.0.1:8502"
	Enabled      bool
}

// Detection is a single YOLO detection result.
type Detection struct {
	Class         string  `json:"class"`
	Confidence    float64 `json:"confidence"`
	BBox          BBox    `json:"bbox"`
	BBoxNorm      BBox    `json:"bbox_normalized"`
}

// BBox is a bounding box in pixel or normalized coordinates.
type BBox struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// YOLOResult is the response from the YOLO detection service.
type YOLOResult struct {
	Detections     []Detection `json:"detections"`
	PPEDetections  []Detection `json:"ppe_detections"`
	PPEViolations  []Detection `json:"ppe_violations"`
	InferenceMs    float64     `json:"inference_ms"`
	SecurityMs     float64     `json:"security_ms"`
	PPEMs          float64     `json:"ppe_ms"`
	Model          string      `json:"model"`
	PPEModel       string      `json:"ppe_model"`
	Device         string      `json:"device"`
}

// QwenResult is the response from the Qwen vLM reasoning service.
type QwenResult struct {
	ThreatLevel          string       `json:"threat_level"`
	Description          string       `json:"description"`
	RecommendedAction    string       `json:"recommended_action"`
	FalsePositivePct     float64      `json:"false_positive_likelihood"`
	Objects              []QwenObject `json:"objects"`
	InferenceMs          float64      `json:"inference_ms"`
	Model                string       `json:"model"`
	Degraded             bool         `json:"degraded"` // true when server fell back to mock_analysis
}

// QwenObject is a detected object with attributes from the vLM.
type QwenObject struct {
	Type       string            `json:"type"`
	Attributes map[string]interface{} `json:"attributes"`
}

// AnalysisResult is the combined output of YOLO + Qwen for a single event frame.
type AnalysisResult struct {
	// YOLO detection tier
	Detections    []Detection `json:"detections"`
	PPEDetections []Detection `json:"ppe_detections"`
	PPEViolations []Detection `json:"ppe_violations"`
	YOLOModel     string      `json:"yolo_model"`
	PPEModel      string      `json:"ppe_model"`
	YOLOMs        float64     `json:"yolo_ms"`

	// Qwen reasoning tier
	ThreatLevel       string       `json:"threat_level"`
	Description       string       `json:"description"`
	RecommendedAction string       `json:"recommended_action"`
	FalsePositivePct  float64      `json:"false_positive_pct"`
	AIObjects         []QwenObject `json:"ai_objects"`
	QwenModel         string       `json:"qwen_model"`
	QwenMs            float64      `json:"qwen_ms"`

	// Pipeline metadata
	TotalMs     float64 `json:"total_ms"`
	FrameSource string  `json:"frame_source"` // "snapshot_cgi" | "rtsp_keyframe"
}

// Client manages communication with the AI inference services.
type Client struct {
	cfg    Config
	http   *http.Client
	yoloOK bool
	qwenOK bool

	// Runtime counters. Process-local, atomic, lost on restart — they
	// give the operator a live "AI funnel" view (frames → YOLO confirmed
	// → Qwen confirmed → alert) without round-tripping to Postgres on
	// every inference. Persisted aggregates land in a hypertable in
	// Phase 3 of the runtime-metrics work.
	yoloCalls     atomic.Int64
	yoloConfirmed atomic.Int64 // YOLO returned >=1 detection
	yoloMsTotal   atomic.Int64 // sum of inference ms (for averaging)

	qwenCalls     atomic.Int64
	qwenConfirmed atomic.Int64 // Qwen returned a threat (not "none" and FP < 50%)
	qwenMsTotal   atomic.Int64

	// Per-site breakdown for the "GPU rental bill" view. Keyed by site
	// UUID (string). Populated only when a caller threads siteID into
	// AnalyzeForSite — the indexer's DescribeVideo path leaves it nil.
	siteStats sync.Map // map[string]*siteCounters
}

// siteCounters holds per-site cumulative AI counters. Atomic ints so
// the sampler can read while a hot inference path writes.
type siteCounters struct {
	yoloCalls     atomic.Int64
	yoloConfirmed atomic.Int64
	yoloMsTotal   atomic.Int64
	qwenCalls     atomic.Int64
	qwenConfirmed atomic.Int64
	qwenMsTotal   atomic.Int64
}

// SiteAIStats is the per-site row in a usage breakdown.
type SiteAIStats struct {
	SiteID         string `json:"site_id"`
	YOLOCalls      int64  `json:"yolo_calls"`
	YOLOConfirmed  int64  `json:"yolo_confirmed"`
	YOLOFiltered   int64  `json:"yolo_filtered"`
	YOLOAvgMs      int64  `json:"yolo_avg_ms"`
	QwenCalls      int64  `json:"qwen_calls"`
	QwenConfirmed  int64  `json:"qwen_confirmed"`
	QwenFiltered   int64  `json:"qwen_filtered"`
	QwenAvgMs      int64  `json:"qwen_avg_ms"`
}

// SiteStatsSnapshot returns one entry per site that has ever fired an
// inference since process start. Sorted by total calls descending so
// the heaviest consumers float to the top.
func (c *Client) SiteStatsSnapshot() []SiteAIStats {
	out := []SiteAIStats{}
	c.siteStats.Range(func(k, v interface{}) bool {
		sid, _ := k.(string)
		sc, _ := v.(*siteCounters)
		if sc == nil {
			return true
		}
		yc := sc.yoloCalls.Load()
		yo := sc.yoloConfirmed.Load()
		ym := sc.yoloMsTotal.Load()
		qc := sc.qwenCalls.Load()
		qo := sc.qwenConfirmed.Load()
		qm := sc.qwenMsTotal.Load()
		avg := func(t, n int64) int64 {
			if n == 0 {
				return 0
			}
			return t / n
		}
		out = append(out, SiteAIStats{
			SiteID:        sid,
			YOLOCalls:     yc,
			YOLOConfirmed: yo,
			YOLOFiltered:  yc - yo,
			YOLOAvgMs:     avg(ym, yc),
			QwenCalls:     qc,
			QwenConfirmed: qo,
			QwenFiltered:  qc - qo,
			QwenAvgMs:     avg(qm, qc),
		})
		return true
	})
	return out
}

// counterFor returns (and lazily creates) the per-site counter struct.
// siteID is the UUID string; empty siteID returns nil so callers fall
// through to the global counters only.
func (c *Client) counterFor(siteID string) *siteCounters {
	if siteID == "" {
		return nil
	}
	if v, ok := c.siteStats.Load(siteID); ok {
		return v.(*siteCounters)
	}
	sc := &siteCounters{}
	actual, _ := c.siteStats.LoadOrStore(siteID, sc)
	return actual.(*siteCounters)
}

// AIStats is the snapshot of the runtime counters returned to the UI.
// All counts are cumulative since process start.
type AIStats struct {
	YOLOCalls       int64 `json:"yolo_calls"`
	YOLOConfirmed   int64 `json:"yolo_confirmed"`
	YOLOFiltered    int64 `json:"yolo_filtered"`
	YOLOAvgMs       int64 `json:"yolo_avg_ms"`

	QwenCalls       int64 `json:"qwen_calls"`
	QwenConfirmed   int64 `json:"qwen_confirmed"`
	QwenFiltered    int64 `json:"qwen_filtered"`
	QwenAvgMs       int64 `json:"qwen_avg_ms"`
}

// Stats returns a snapshot of the runtime counters. Safe for concurrent
// use; the four reads aren't atomically consistent with one another but
// the drift between them is at most one in-flight inference, which is
// not material for an operator dashboard.
func (c *Client) Stats() AIStats {
	yc := c.yoloCalls.Load()
	yo := c.yoloConfirmed.Load()
	ym := c.yoloMsTotal.Load()
	qc := c.qwenCalls.Load()
	qo := c.qwenConfirmed.Load()
	qm := c.qwenMsTotal.Load()

	avg := func(total, count int64) int64 {
		if count == 0 {
			return 0
		}
		return total / count
	}
	return AIStats{
		YOLOCalls:     yc,
		YOLOConfirmed: yo,
		YOLOFiltered:  yc - yo,
		YOLOAvgMs:     avg(ym, yc),
		QwenCalls:     qc,
		QwenConfirmed: qo,
		QwenFiltered:  qc - qo,
		QwenAvgMs:     avg(qm, qc),
	}
}

// NewClient creates an AI pipeline client.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// CheckHealth probes both services and logs their status.
func (c *Client) CheckHealth(ctx context.Context) {
	if !c.cfg.Enabled {
		log.Println("[AI] Pipeline disabled")
		return
	}

	// YOLO health
	if resp, err := c.http.Get(c.cfg.YOLOEndpoint + "/health"); err == nil {
		resp.Body.Close()
		c.yoloOK = resp.StatusCode == 200
		log.Printf("[AI] YOLO service: %s (status %d)", c.cfg.YOLOEndpoint, resp.StatusCode)
	} else {
		log.Printf("[AI] YOLO service unreachable: %v", err)
	}

	// Qwen health
	if resp, err := c.http.Get(c.cfg.QwenEndpoint + "/health"); err == nil {
		resp.Body.Close()
		c.qwenOK = resp.StatusCode == 200
		log.Printf("[AI] Qwen service: %s (status %d)", c.cfg.QwenEndpoint, resp.StatusCode)
	} else {
		log.Printf("[AI] Qwen service unreachable: %v", err)
	}
}

// Analyze runs the full YOLO → Qwen pipeline on a JPEG frame.
// `siteID` is the UUID string of the originating site; pass "" if the
// caller doesn't have one (e.g. ad-hoc snapshot tools). Site attribution
// drives the per-site usage breakdown on the Services dashboard.
// Returns nil if AI services are unavailable (graceful degradation).
func (c *Client) Analyze(ctx context.Context, jpegFrame []byte, siteID, siteContext string) *AnalysisResult {
	if !c.cfg.Enabled || len(jpegFrame) == 0 {
		return nil
	}
	site := c.counterFor(siteID)

	start := time.Now()
	result := &AnalysisResult{FrameSource: "snapshot_cgi"}

	// ── Tier 1: YOLO detection ──
	yoloResult, err := c.detectYOLO(ctx, jpegFrame)
	if err != nil {
		log.Printf("[AI] YOLO inference failed: %v", err)
		return nil
	}
	c.yoloCalls.Add(1)
	c.yoloMsTotal.Add(int64(yoloResult.InferenceMs))
	if len(yoloResult.Detections) > 0 {
		c.yoloConfirmed.Add(1)
	}
	if site != nil {
		site.yoloCalls.Add(1)
		site.yoloMsTotal.Add(int64(yoloResult.InferenceMs))
		if len(yoloResult.Detections) > 0 {
			site.yoloConfirmed.Add(1)
		}
	}
	result.Detections = yoloResult.Detections
	result.PPEDetections = yoloResult.PPEDetections
	result.PPEViolations = yoloResult.PPEViolations
	result.YOLOModel = yoloResult.Model
	result.PPEModel = yoloResult.PPEModel
	result.YOLOMs = yoloResult.InferenceMs

	// Gate: only promote to Qwen if YOLO found something
	if len(yoloResult.Detections) == 0 {
		result.TotalMs = float64(time.Since(start).Milliseconds())
		result.Description = "No objects detected by YOLO"
		result.ThreatLevel = "none"
		return result
	}

	log.Printf("[AI] YOLO: %d detections in %.0fms (model=%s)",
		len(yoloResult.Detections), yoloResult.InferenceMs, yoloResult.Model)

	// ── Tier 2: Qwen reasoning ──
	// Enrich site context with PPE findings so Qwen can reason about safety compliance
	enrichedContext := siteContext
	if len(yoloResult.PPEViolations) > 0 {
		enrichedContext += "\n\nPPE VIOLATIONS DETECTED:"
		for _, v := range yoloResult.PPEViolations {
			enrichedContext += fmt.Sprintf("\n- %s (%.0f%% confidence)", v.Class, v.Confidence*100)
		}
	}
	if len(yoloResult.PPEDetections) > 0 {
		enrichedContext += "\n\nPPE status detected: "
		ppeList := []string{}
		for _, d := range yoloResult.PPEDetections {
			ppeList = append(ppeList, fmt.Sprintf("%s(%.0f%%)", d.Class, d.Confidence*100))
		}
		enrichedContext += strings.Join(ppeList, ", ")
	}

	qwenResult, err := c.analyzeQwen(ctx, jpegFrame, yoloResult.Detections, enrichedContext)
	if err != nil {
		log.Printf("[AI] Qwen inference failed: %v (using YOLO-only result)", err)
		// Graceful degradation: return YOLO results without Qwen reasoning
		result.TotalMs = float64(time.Since(start).Milliseconds())
		result.Description = buildYOLOOnlyDescription(yoloResult.Detections)
		result.ThreatLevel = inferThreatFromYOLO(yoloResult.Detections)
		return result
	}
	c.qwenCalls.Add(1)
	c.qwenMsTotal.Add(int64(qwenResult.InferenceMs))
	// "Confirmed" = Qwen produced an actionable verdict. Anything classified
	// "none" or with a >50% false-positive probability counts as filtered.
	qwenConfirmed := qwenResult.ThreatLevel != "" && qwenResult.ThreatLevel != "none" && qwenResult.FalsePositivePct < 0.5
	if qwenConfirmed {
		c.qwenConfirmed.Add(1)
	}
	if site != nil {
		site.qwenCalls.Add(1)
		site.qwenMsTotal.Add(int64(qwenResult.InferenceMs))
		if qwenConfirmed {
			site.qwenConfirmed.Add(1)
		}
	}

	result.ThreatLevel = qwenResult.ThreatLevel
	result.Description = qwenResult.Description
	result.RecommendedAction = qwenResult.RecommendedAction
	result.FalsePositivePct = qwenResult.FalsePositivePct
	result.AIObjects = qwenResult.Objects
	result.QwenModel = qwenResult.Model
	result.QwenMs = qwenResult.InferenceMs
	result.TotalMs = float64(time.Since(start).Milliseconds())

	log.Printf("[AI] Qwen: threat=%s fp=%.0f%% in %.0fms — %s",
		qwenResult.ThreatLevel, qwenResult.FalsePositivePct*100,
		qwenResult.InferenceMs, truncate(qwenResult.Description, 80))

	return result
}

// DescribeResult is the index-mode response from Qwen — neutral facts about
// a clip for later FTS/tag search, with no threat assessment. Lives alongside
// QwenResult because most fields differ.
type DescribeResult struct {
	Description   string                 `json:"description"`
	Tags          []string               `json:"tags"`
	ActivityLevel string                 `json:"activity_level"`
	Entities      []QwenObject           `json:"entities"`
	InferenceMs   float64                `json:"inference_ms"`
	Model         string                 `json:"model"`
	Degraded      bool                   `json:"degraded"`
	// Raw is kept so unexpected fields survive a future prompt change.
	Raw map[string]interface{} `json:"-"`
}

// DescribeVideo sends a clip to Qwen's describe-mode endpoint. Used by the
// background indexer to build a search corpus without touching the live
// alarm pipeline. Returns nil on transport/decode failure; callers persist
// a stub row so the indexer doesn't retry forever.
func (c *Client) DescribeVideo(ctx context.Context, mp4Clip []byte, detections []Detection) *DescribeResult {
	if !c.cfg.Enabled || !c.qwenOK || len(mp4Clip) < 1000 {
		return nil
	}

	// Same 60s cap as AnalyzeVideo — VLM inference time is the bottleneck.
	httpClient := &http.Client{Timeout: 60 * time.Second}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("video", "clip.mp4")
	part.Write(mp4Clip)

	detectionsJSON, _ := json.Marshal(detections)
	writer.WriteField("detections", string(detectionsJSON))
	writer.WriteField("mode", "describe")
	writer.Close()

	req, _ := http.NewRequestWithContext(ctx, "POST", c.cfg.QwenEndpoint+"/analyze_video", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[AI] describe request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		log.Printf("[AI] describe HTTP %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
		return nil
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("[AI] describe decode: %v", err)
		return nil
	}
	// Re-marshal into the typed struct so tag/attribute coercion is explicit.
	b, _ := json.Marshal(raw)
	var result DescribeResult
	_ = json.Unmarshal(b, &result)
	result.Raw = raw
	return &result
}

// DetectYOLO is a thin public wrapper so the indexer can use the same YOLO
// client as the live pipeline (gate empty segments without Qwen cost).
func (c *Client) DetectYOLO(ctx context.Context, jpegFrame []byte) (*YOLOResult, error) {
	return c.detectYOLO(ctx, jpegFrame)
}

// AnalyzeVideo runs Qwen video inference on a short surveillance clip. The
// video tier runs after the single-frame tier — it gives Qwen motion context
// (e.g., how an object is carried, movement direction) that a single snapshot
// cannot convey. Returns nil on failure (caller keeps the snapshot-tier result).
// `siteID` is the originating site UUID for per-site usage attribution; pass
// "" to skip site accounting.
func (c *Client) AnalyzeVideo(ctx context.Context, mp4Clip []byte, detections []Detection, siteID, siteContext string) *QwenResult {
	if !c.cfg.Enabled || !c.qwenOK || len(mp4Clip) < 1000 {
		return nil
	}
	site := c.counterFor(siteID)

	// Longer timeout — video inference is ~15-25s vs ~5-10s for a still.
	httpClient := &http.Client{Timeout: 60 * time.Second}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, _ := writer.CreateFormFile("video", "clip.mp4")
	part.Write(mp4Clip)

	detectionsJSON, _ := json.Marshal(detections)
	writer.WriteField("detections", string(detectionsJSON))
	if siteContext != "" {
		writer.WriteField("site_context", siteContext)
	}
	writer.Close()

	req, _ := http.NewRequestWithContext(ctx, "POST", c.cfg.QwenEndpoint+"/analyze_video", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[AI] Qwen video request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		log.Printf("[AI] Qwen video HTTP %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
		return nil
	}

	var result QwenResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[AI] Qwen video decode: %v", err)
		return nil
	}
	c.qwenCalls.Add(1)
	c.qwenMsTotal.Add(int64(result.InferenceMs))
	confirmed := result.ThreatLevel != "" && result.ThreatLevel != "none" && result.FalsePositivePct < 0.5
	if confirmed {
		c.qwenConfirmed.Add(1)
	}
	if site != nil {
		site.qwenCalls.Add(1)
		site.qwenMsTotal.Add(int64(result.InferenceMs))
		if confirmed {
			site.qwenConfirmed.Add(1)
		}
	}
	log.Printf("[AI] Qwen video: threat=%s fp=%.0f%% in %.0fms — %s",
		result.ThreatLevel, result.FalsePositivePct*100,
		result.InferenceMs, truncate(result.Description, 80))
	return &result
}

// detectYOLO sends a frame to the YOLO service.
func (c *Client) detectYOLO(ctx context.Context, jpegFrame []byte) (*YOLOResult, error) {
	body, contentType, err := buildMultipartImage(jpegFrame)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", c.cfg.YOLOEndpoint+"/detect", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("YOLO request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("YOLO HTTP %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
	}

	var result YOLOResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("YOLO decode: %w", err)
	}
	return &result, nil
}

// analyzeQwen sends a frame + YOLO detections to the Qwen vLM service.
func (c *Client) analyzeQwen(ctx context.Context, jpegFrame []byte, detections []Detection, siteContext string) (*QwenResult, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Image file
	part, _ := writer.CreateFormFile("image", "frame.jpg")
	part.Write(jpegFrame)

	// Detections JSON
	detectionsJSON, _ := json.Marshal(detections)
	writer.WriteField("detections", string(detectionsJSON))

	// Site context (optional)
	if siteContext != "" {
		writer.WriteField("site_context", siteContext)
	}

	writer.Close()

	req, _ := http.NewRequestWithContext(ctx, "POST", c.cfg.QwenEndpoint+"/analyze", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Qwen request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Qwen HTTP %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
	}

	var result QwenResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("Qwen decode: %w", err)
	}
	return &result, nil
}

// buildMultipartImage creates a multipart form body with a JPEG image.
func buildMultipartImage(jpegFrame []byte) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("image", "frame.jpg")
	if err != nil {
		return nil, "", err
	}
	part.Write(jpegFrame)
	writer.Close()
	return body, writer.FormDataContentType(), nil
}

// buildYOLOOnlyDescription creates a description when Qwen is unavailable.
func buildYOLOOnlyDescription(detections []Detection) string {
	if len(detections) == 0 {
		return "No objects detected"
	}
	desc := fmt.Sprintf("YOLO detected %d object(s):", len(detections))
	for i, d := range detections {
		if i >= 3 {
			desc += fmt.Sprintf(" +%d more", len(detections)-3)
			break
		}
		desc += fmt.Sprintf(" %s (%.0f%%)", d.Class, d.Confidence*100)
		if i < len(detections)-1 && i < 2 {
			desc += ","
		}
	}
	return desc
}

// inferThreatFromYOLO guesses threat level from YOLO classes when Qwen is unavailable.
func inferThreatFromYOLO(detections []Detection) string {
	for _, d := range detections {
		if d.Class == "person" && d.Confidence > 0.8 {
			return "high"
		}
	}
	for _, d := range detections {
		if d.Class == "person" {
			return "medium"
		}
	}
	return "low"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
