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
// Returns nil if AI services are unavailable (graceful degradation).
func (c *Client) Analyze(ctx context.Context, jpegFrame []byte, siteContext string) *AnalysisResult {
	if !c.cfg.Enabled || len(jpegFrame) == 0 {
		return nil
	}

	start := time.Now()
	result := &AnalysisResult{FrameSource: "snapshot_cgi"}

	// ── Tier 1: YOLO detection ──
	yoloResult, err := c.detectYOLO(ctx, jpegFrame)
	if err != nil {
		log.Printf("[AI] YOLO inference failed: %v", err)
		return nil
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
func (c *Client) AnalyzeVideo(ctx context.Context, mp4Clip []byte, detections []Detection, siteContext string) *QwenResult {
	if !c.cfg.Enabled || !c.qwenOK || len(mp4Clip) < 1000 {
		return nil
	}

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
