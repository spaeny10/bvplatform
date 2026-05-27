package safety

import (
	"context"
	"errors"
	"testing"

	"ironsight/internal/ai"
)

// mockAIClient implements AIClient for unit tests.
type mockAIClient struct {
	result *ai.PPEValidationResult
	err    error
}

func (m *mockAIClient) ValidatePPEFrame(
	_ context.Context,
	_ []byte,
	_ []ai.Detection,
	_ string,
) (*ai.PPEValidationResult, error) {
	return m.result, m.err
}

var testBBox = ai.BBox{X1: 0.1, Y1: 0.1, X2: 0.5, Y2: 0.9}

func callValidate(t *testing.T, client AIClient) VLMValidationResult {
	t.Helper()
	return ValidatePPECandidate(
		context.Background(), client,
		[]byte("fake-jpeg"),
		"no-vest", "safety vest",
		0.75, testBBox,
	)
}

func TestValidatePPECandidate_Confirmed(t *testing.T) {
	client := &mockAIClient{
		result: &ai.PPEValidationResult{FalsePositivePct: 0.10, Model: "qwen3b"},
	}
	got := callValidate(t, client)
	if got.Verdict != VerdictConfirmed {
		t.Errorf("expected confirmed, got %s", got.Verdict)
	}
	if got.Model != "qwen3b" {
		t.Errorf("expected model qwen3b, got %s", got.Model)
	}
	if got.Degraded {
		t.Error("expected Degraded=false")
	}
}

func TestValidatePPECandidate_Dismissed(t *testing.T) {
	client := &mockAIClient{
		result: &ai.PPEValidationResult{FalsePositivePct: 0.80, Model: "qwen3b"},
	}
	got := callValidate(t, client)
	if got.Verdict != VerdictDismissed {
		t.Errorf("expected dismissed, got %s", got.Verdict)
	}
}

func TestValidatePPECandidate_Uncertain(t *testing.T) {
	client := &mockAIClient{
		result: &ai.PPEValidationResult{FalsePositivePct: 0.50, Model: "qwen3b"},
	}
	got := callValidate(t, client)
	if got.Verdict != VerdictUncertain {
		t.Errorf("expected uncertain, got %s", got.Verdict)
	}
}

func TestValidatePPECandidate_DegradedMode(t *testing.T) {
	// Degraded=true must always produce VerdictError regardless of likelihood.
	client := &mockAIClient{
		result: &ai.PPEValidationResult{FalsePositivePct: 0.10, Degraded: true, Model: "mock"},
	}
	got := callValidate(t, client)
	if got.Verdict != VerdictError {
		t.Errorf("expected error (degraded), got %s", got.Verdict)
	}
	if !got.Degraded {
		t.Error("expected Degraded=true")
	}
}

func TestValidatePPECandidate_TransportError(t *testing.T) {
	client := &mockAIClient{
		result: &ai.PPEValidationResult{Degraded: true},
		err:    errors.New("connection refused"),
	}
	got := callValidate(t, client)
	if got.Verdict != VerdictError {
		t.Errorf("expected error (transport), got %s", got.Verdict)
	}
	if got.Reasoning == "" {
		t.Error("expected non-empty reasoning on transport error")
	}
}

func TestValidatePPECandidate_ParsesEmbeddedJSON_Dismissed(t *testing.T) {
	// Qwen returned a description with embedded JSON verdict.
	client := &mockAIClient{
		result: &ai.PPEValidationResult{
			FalsePositivePct: 0.10, // threshold would say confirmed
			Description:      `{"verdict":"dismissed","reasoning":"person out of zone, arm blocking view"}`,
			Model:            "qwen3b",
		},
	}
	got := callValidate(t, client)
	// Embedded JSON should override the threshold-based verdict.
	if got.Verdict != VerdictDismissed {
		t.Errorf("expected dismissed (embedded JSON), got %s", got.Verdict)
	}
	if got.Reasoning != "person out of zone, arm blocking view" {
		t.Errorf("unexpected reasoning: %s", got.Reasoning)
	}
}

func TestValidatePPECandidate_ParsesEmbeddedJSON_Confirmed(t *testing.T) {
	client := &mockAIClient{
		result: &ai.PPEValidationResult{
			FalsePositivePct: 0.80, // threshold would say dismissed
			Description:      `Some preamble text. {"verdict":"confirmed","reasoning":"clearly no vest visible"} trailing`,
			Model:            "qwen3b",
		},
	}
	got := callValidate(t, client)
	if got.Verdict != VerdictConfirmed {
		t.Errorf("expected confirmed (embedded JSON overrides threshold), got %s", got.Verdict)
	}
}

func TestValidatePPECandidate_ParsesEmbeddedJSON_UnknownVerdict(t *testing.T) {
	// Unknown verdict in JSON → fall back to threshold path.
	client := &mockAIClient{
		result: &ai.PPEValidationResult{
			FalsePositivePct: 0.10,
			Description:      `{"verdict":"maybe","reasoning":"unsure"}`,
			Model:            "qwen3b",
		},
	}
	got := callValidate(t, client)
	// Threshold for 0.10 → confirmed.
	if got.Verdict != VerdictConfirmed {
		t.Errorf("expected confirmed (threshold fallback), got %s", got.Verdict)
	}
}

// TestMapLikelihoodToVerdict verifies boundary values for the threshold bands.
func TestMapLikelihoodToVerdict(t *testing.T) {
	cases := []struct {
		likelihood float64
		want       VLMVerdict
	}{
		{0.00, VerdictConfirmed},
		{0.35, VerdictConfirmed},
		{0.36, VerdictUncertain},
		{0.50, VerdictUncertain},
		{0.64, VerdictUncertain},
		{0.65, VerdictDismissed},
		{1.00, VerdictDismissed},
	}
	for _, tc := range cases {
		got := mapLikelihoodToVerdict(tc.likelihood)
		if got != tc.want {
			t.Errorf("likelihood=%.2f: got %s, want %s", tc.likelihood, got, tc.want)
		}
	}
}
