package safety

// vlm_validator.go — P2-C-03 VLM validation pass for PPE candidates.
//
// ValidatePPECandidate is a pure function: no DB I/O, no side effects.
// It builds a PPE-specific prompt, calls the AI client, and maps the
// response to a VLMVerdict. The VLM worker (vlm_worker.go) calls it
// once per pending_review_queue row.
//
// Verdict-mapping priority (highest to lowest):
//  1. Degraded=true → VerdictError (sidecar in mock mode; trust nothing)
//  2. Transport error → VerdictError
//  3. Embedded JSON in description → parsed verdict overrides thresholds
//  4. false_positive_likelihood thresholds:
//       >= 0.65 → VerdictDismissed
//       <= 0.35 → VerdictConfirmed
//       otherwise → VerdictUncertain
//
// The threshold strategy is conservative by design: err toward keeping
// candidates in the human queue rather than auto-dismissing borderline
// cases.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ironsight/internal/ai"
)

// AIClient is the subset of ai.Client that the VLM validator needs.
// Defined here to keep internal/safety independent of ai.Client's
// concrete type — makes the validator unit-testable with a mock.
type AIClient interface {
	ValidatePPEFrame(ctx context.Context, jpegBytes []byte,
		detections []ai.Detection, ppePrompt string) (*ai.PPEValidationResult, error)
}

// VLMVerdict is the outcome of a VLM validation pass.
type VLMVerdict string

const (
	VerdictConfirmed VLMVerdict = "confirmed"
	VerdictDismissed VLMVerdict = "dismissed"
	VerdictUncertain VLMVerdict = "uncertain"
	VerdictError     VLMVerdict = "error"
)

// VLMValidationResult holds the verdict and its provenance.
type VLMValidationResult struct {
	Verdict   VLMVerdict
	Reasoning string
	Model     string
	Degraded  bool
}

// ValidatePPECandidate asks Qwen whether a YOLO PPE candidate is a genuine
// violation. It builds a PPE-specific prompt, calls the AI client, and maps
// the response to a VLMVerdict.
//
// Returns a non-nil VLMValidationResult under all conditions — callers
// record the result and continue; they never need to nil-check.
func ValidatePPECandidate(
	ctx context.Context,
	client AIClient,
	jpegBytes []byte,
	detectionClass string,
	missingLabel string,
	confidence float64,
	bboxNorm ai.BBox,
) VLMValidationResult {
	prompt := buildPPEPrompt(detectionClass, missingLabel, confidence, bboxNorm)

	// Build a minimal Detection slice so Qwen sees the spatial context.
	detections := []ai.Detection{
		{
			Class:      detectionClass,
			Confidence: confidence,
			BBoxNorm:   bboxNorm,
		},
	}

	result, err := client.ValidatePPEFrame(ctx, jpegBytes, detections, prompt)
	if err != nil {
		return VLMValidationResult{
			Verdict:   VerdictError,
			Reasoning: fmt.Sprintf("transport error: %v", err),
			Degraded:  true,
		}
	}
	if result.Degraded {
		return VLMValidationResult{
			Verdict:   VerdictError,
			Reasoning: "sidecar in degraded/mock mode; verdict not trusted",
			Model:     result.Model,
			Degraded:  true,
		}
	}

	// Attempt to parse an explicit verdict from the description field.
	// The PPE prompt asks Qwen to embed {"verdict":"...","reasoning":"..."}
	// in its response. If parsing succeeds, that overrides the threshold path.
	if v, reasoning, ok := parseEmbeddedVerdict(result.Description); ok {
		return VLMValidationResult{
			Verdict:   v,
			Reasoning: truncateReasoning(reasoning),
			Model:     result.Model,
		}
	}

	// Fallback: map false_positive_likelihood to verdict via thresholds.
	verdict := mapLikelihoodToVerdict(result.FalsePositivePct)
	reasoning := truncateReasoning(result.Description)

	return VLMValidationResult{
		Verdict:   verdict,
		Reasoning: reasoning,
		Model:     result.Model,
	}
}

// buildPPEPrompt constructs the site_context string injected into the Qwen
// /analyze request. The prompt asks Qwen to embed a structured JSON verdict
// in its description field because the sidecar endpoint is shared and we
// cannot add a PPE-specific system prompt without modifying server.py.
func buildPPEPrompt(class, label string, confidence float64, bbox ai.BBox) string {
	return fmt.Sprintf(
		"PPE COMPLIANCE VALIDATION REQUEST.\n"+
			"YOLO detected a missing PPE item: %s (class: %s, confidence: %.0f%%).\n"+
			"Bounding box (normalized): x1=%.3f y1=%.3f x2=%.3f y2=%.3f.\n\n"+
			"Is the person at this bounding box actually missing their %s?\n"+
			"Consider: partial occlusion, viewing angle, similar-looking objects, "+
			"person partially out of frame.\n"+
			"Respond ONLY with JSON: "+
			`{"verdict":"confirmed"|"dismissed"|"uncertain","reasoning":"one sentence"}`,
		label, class, confidence*100,
		bbox.X1, bbox.Y1, bbox.X2, bbox.Y2,
		label,
	)
}

// mapLikelihoodToVerdict applies the conservative threshold bands.
//
//	>= 0.65 → dismissed  (Qwen confident it is a false positive)
//	<= 0.35 → confirmed  (Qwen confident it is a real violation)
//	else    → uncertain  (middle band; keep in human queue)
func mapLikelihoodToVerdict(likelihood float64) VLMVerdict {
	switch {
	case likelihood >= 0.65:
		return VerdictDismissed
	case likelihood <= 0.35:
		return VerdictConfirmed
	default:
		return VerdictUncertain
	}
}

// embeddedVerdictJSON is the shape we ask Qwen to embed in its description.
type embeddedVerdictJSON struct {
	Verdict   string `json:"verdict"`
	Reasoning string `json:"reasoning"`
}

// parseEmbeddedVerdict searches result.Description for a JSON object
// containing a "verdict" field and extracts it. Returns (verdict, reasoning,
// true) on success; (zero, "", false) when no valid embedded JSON is found.
//
// The search is resilient to leading/trailing prose from Qwen: we scan for
// the first '{' and attempt to parse from there, stopping at the matching '}'.
func parseEmbeddedVerdict(description string) (VLMVerdict, string, bool) {
	start := strings.Index(description, "{")
	if start < 0 {
		return "", "", false
	}
	end := strings.LastIndex(description, "}")
	if end <= start {
		return "", "", false
	}
	candidate := description[start : end+1]

	var ev embeddedVerdictJSON
	if err := json.Unmarshal([]byte(candidate), &ev); err != nil {
		return "", "", false
	}

	var verdict VLMVerdict
	switch ev.Verdict {
	case "confirmed":
		verdict = VerdictConfirmed
	case "dismissed":
		verdict = VerdictDismissed
	case "uncertain":
		verdict = VerdictUncertain
	default:
		return "", "", false
	}
	return verdict, ev.Reasoning, true
}

// truncateReasoning caps reasoning text at 500 chars so DB column stays
// manageable even if Qwen returns a verbose response.
func truncateReasoning(s string) string {
	const maxLen = 500
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
