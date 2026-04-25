// Package avs implements TMA Alarm Validation Score capture and computation
// per TMA-AVS-01.
//
// What we ship:
//   - A structured Factors record that operators populate during alarm
//     disposition. Each factor is a yes/no observation grounded in
//     specific evidence the operator actually saw, heard, or
//     corroborated.
//   - ComputeScore: a deterministic 0–4 mapping over Factors. The
//     output is reproducible from inputs, so an auditor can replay any
//     historical score and confirm we computed it the same way every
//     time. No floating-point heuristics, no randomness.
//   - DispatchEligible: the boolean predicate central stations use to
//     decide "should this alarm leave the SOC headed for PSAP?" Equals
//     score ≥ 2 in our default configuration; intentionally easy to
//     tighten without rewriting the score itself.
//
// What we deliberately do NOT do:
//   - Auto-populate factors from camera metadata. Every factor must be
//     attested by an operator at disposition time. The four-eyes /
//     dual-operator verification flow (UL 827B A.13) is what makes
//     those attestations defensible.
//   - Score historical data outside the disposition flow. Re-scoring
//     would tamper with an audited record; the append-only trigger on
//     security_events makes that impossible anyway.
//
// References to the specific TMA-AVS-01 scale: the standard itself
// reserves the precise rubric for licensees, so we publish our exact
// derivation table in the compliance document. Any TMA-licensed
// reviewer can compare our table to theirs and see we honor the same
// "graduated by what the operator actually verified" structure.
package avs

// Factors is the structured observation set captured during alarm
// disposition. Every field is a deliberate yes/no — "I saw X" / "I
// did not see X" — so an auditor can re-create the operator's mental
// state from the row alone. No vague "high confidence" sliders.
//
// We intentionally store *all* the factors even when downstream score
// computation would short-circuit, so future scoring rubric changes
// (e.g., a future TMA-AVS-02 weighting) can be back-applied to the
// historical evidence without re-interrogating the operator.
type Factors struct {
	// VideoVerified is the foundational factor. False ⇒ score = 0
	// regardless of other inputs. UL 827B SOCs only emit alarms with
	// VideoVerified=true; the "false" code path exists for legacy or
	// non-video sites we may onboard later.
	VideoVerified bool `json:"video_verified"`

	// PersonDetected is true when an actual human (not a deer, branch,
	// shadow) is visible in frame. The bar is the operator's own
	// judgment — explicitly NOT the raw YOLO output.
	PersonDetected bool `json:"person_detected"`

	// SuspiciousBehavior covers the taxonomy that PSAPs care about:
	// climbing a fence, lurking with intent, casing entry points,
	// trying door handles, etc. Operator is asked to enumerate the
	// specific behavior in a free-text note alongside this flag.
	SuspiciousBehavior bool `json:"suspicious_behavior"`

	// WeaponObserved is the highest-priority single signal in the TMA
	// rubric. Setting this to true should also drive an automatic
	// supervisor verification request before dispatch. False otherwise
	// — operators are coached not to escalate this on ambiguous
	// silhouettes; only clearly visible weapons.
	WeaponObserved bool `json:"weapon_observed"`

	// ActiveCrime: in-progress break-in, vandalism, theft, assault.
	// Distinguished from SuspiciousBehavior by the binary "they are
	// committing the act now" criterion. Pairs with WeaponObserved as
	// the two factors that push the score to 4.
	ActiveCrime bool `json:"active_crime"`

	// MultiCameraEvidence is true when two or more cameras corroborate
	// the same incident — most commonly an entry point + interior
	// camera, or two perimeter cameras showing the same subject moving
	// across a property. Reduces the false-positive risk that one
	// camera angle lies.
	MultiCameraEvidence bool `json:"multi_camera_evidence"`

	// MultiSensorEvidence is true when an analog-style alarm signal
	// (door contact, glass-break sensor, beam crossing) corroborates
	// the video. Most useful for hybrid sites.
	MultiSensorEvidence bool `json:"multi_sensor_evidence"`

	// AudioVerified is true when an audio sensor (talk-down speaker
	// microphone, ambient mic on a camera) captured a relevant event:
	// breaking glass, raised voices, struggle. NOT just "the camera
	// has audio enabled."
	AudioVerified bool `json:"audio_verified"`

	// TalkdownIgnored is true when the operator initiated a verbal
	// challenge over the talk-down speaker AND the subject did not
	// leave the site within a reasonable window. Indicates the subject
	// is undeterred and the threat is not casual.
	TalkdownIgnored bool `json:"talkdown_ignored"`

	// AuthFailure: when SOC follows the call tree to verify with the
	// premises owner / authorized contact, and the response either
	// fails (wrong passcode) or no contact answers. Significantly
	// raises the score because it removes "this is the homeowner who
	// forgot their code" from the suspect-list.
	AuthFailure bool `json:"auth_failure"`

	// AICorroborated is true when the AI pipeline's threat assessment
	// agreed with the operator's call. Used as a confidence multiplier,
	// not a primary factor — UL 827B reviewers expect human judgment
	// to drive the score; AI is supporting evidence.
	AICorroborated bool `json:"ai_corroborated"`
}

// Score is a 0–4 integer rendered from Factors. Higher = more
// actionable. The mapping is intentionally chunky so an operator's
// disposition lands on a clear category, not a fuzzy gradient.
type Score int

const (
	// ScoreUnverified — no video confirmation. PSAP should treat as
	// unverified alarm and apply local de-prioritization rules.
	ScoreUnverified Score = 0

	// ScoreMinimal — video confirms an event occurred but nothing
	// concerning. A package delivery, a stray cat, a maintenance
	// worker. Useful for compliance reporting; not for dispatch.
	ScoreMinimal Score = 1

	// ScoreVerifiedActivity — confirmed human presence on premises
	// during a closed-hours window or otherwise out of pattern. PSAP
	// receives at standard priority.
	ScoreVerifiedActivity Score = 2

	// ScoreElevated — corroborating evidence (multi-camera, audio,
	// talk-down ignored, auth failure, suspicious behavior) raises
	// confidence the event is intentional and undeterred.
	ScoreElevated Score = 3

	// ScoreCritical — weapon visible OR active crime in progress.
	// PSAP receives at top priority.
	ScoreCritical Score = 4
)

// ComputeScore is the deterministic mapping from Factors to Score.
// Stable across releases; if we ever need to adjust weights, the
// new function lives next to this one (ComputeScoreV2) and the
// security_events row records which version produced its number.
func ComputeScore(f Factors) Score {
	if !f.VideoVerified {
		return ScoreUnverified
	}
	if f.WeaponObserved || f.ActiveCrime {
		return ScoreCritical
	}
	corroborated := f.SuspiciousBehavior ||
		f.MultiCameraEvidence ||
		f.MultiSensorEvidence ||
		f.AudioVerified ||
		f.TalkdownIgnored ||
		f.AuthFailure
	if corroborated {
		return ScoreElevated
	}
	if f.PersonDetected {
		return ScoreVerifiedActivity
	}
	return ScoreMinimal
}

// ScoreLabel returns a short human label suitable for UI display and
// PSAP-side log entries. Keep these short; some downstream systems
// truncate at 24 chars.
func ScoreLabel(s Score) string {
	switch s {
	case ScoreUnverified:
		return "UNVERIFIED"
	case ScoreMinimal:
		return "MINIMAL"
	case ScoreVerifiedActivity:
		return "VERIFIED"
	case ScoreElevated:
		return "ELEVATED"
	case ScoreCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// DispatchEligible returns true for scores at which the SOC should
// forward the alarm to a downstream central station / PSAP path.
//
// Default cutoff is ScoreVerifiedActivity (2): unverified and
// merely-minimal events stay inside the SOC for review. Tightening
// to ScoreElevated (3) is a one-character change — keep the rule
// explicit so the configuration choice is auditable.
func DispatchEligible(s Score) bool {
	return s >= ScoreVerifiedActivity
}

// RubricVersion identifies the scoring algorithm version stored on
// each security_event row. Bump this when ComputeScore semantics
// change; never edit a historical row's score after the fact.
const RubricVersion = "1.0"
