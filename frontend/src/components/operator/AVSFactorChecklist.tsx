'use client';

import type { AVSFactors } from '@/lib/ironsight-api';

interface Props {
  factors: AVSFactors;
  onToggle: (k: keyof AVSFactors) => void;
  preview: { score: 0 | 1 | 2 | 3 | 4; label: string; dispatch: boolean };
}

// Operator-facing TMA-AVS-01 validation-factor checklist.
//
// The 11 boolean factors map directly to internal/avs/scoring.go on
// the backend. We render them in three priority bands so the
// operator's eye is drawn to the highest-impact attestations first:
//
//   Band 1 (foundational):   video_verified, person_detected
//   Band 2 (corroborating):  multi-camera, multi-sensor, audio,
//                            talkdown_ignored, suspicious_behavior,
//                            auth_failure, ai_corroborated
//   Band 3 (priority):       weapon_observed, active_crime
//
// A live "Score: VERIFIED (2)" preview sits at the top, recomputed
// from the factor state on every toggle. The backend recomputes the
// authoritative score on submit — this is purely UX so the operator
// understands what their attestations imply for downstream dispatch.

const FACTOR_LABELS: Record<keyof AVSFactors, { label: string; help: string }> = {
  video_verified:        { label: 'Video verified',         help: 'I personally saw the event on camera (not just trusted the AI)' },
  person_detected:       { label: 'Person detected',        help: 'A human is visible in frame (not animal, shadow, or branch)' },
  suspicious_behavior:   { label: 'Suspicious behavior',    help: 'Climbing fence, lurking, casing entry, trying handles' },
  weapon_observed:       { label: 'Weapon observed',        help: 'A weapon is clearly visible (use sparingly)' },
  active_crime:          { label: 'Active crime',           help: 'Break-in, vandalism, theft, or assault in progress' },
  multi_camera_evidence: { label: 'Multi-camera evidence',  help: '2+ cameras corroborate the same incident' },
  multi_sensor_evidence: { label: 'Multi-sensor evidence',  help: 'Door contact / glass break / beam corroborates' },
  audio_verified:        { label: 'Audio verified',         help: 'Audio captured glass, voices, struggle' },
  talkdown_ignored:      { label: 'Talk-down ignored',      help: 'Verbal challenge issued AND subject did not leave' },
  auth_failure:          { label: 'Auth failure',           help: 'Customer call-back failed or wrong passcode' },
  ai_corroborated:       { label: 'AI corroborated',        help: 'Qwen/YOLO threat assessment agrees with you' },
};

const BANDS: Array<{ title: string; keys: Array<keyof AVSFactors> }> = [
  { title: 'Foundational', keys: ['video_verified', 'person_detected'] },
  { title: 'Corroborating', keys: [
      'multi_camera_evidence', 'multi_sensor_evidence', 'audio_verified',
      'talkdown_ignored', 'suspicious_behavior', 'auth_failure', 'ai_corroborated',
  ]},
  { title: 'Priority signals', keys: ['weapon_observed', 'active_crime'] },
];

const SCORE_COLORS: Record<number, { bg: string; border: string; text: string }> = {
  0: { bg: 'rgba(75, 85, 99, 0.10)',  border: 'rgba(75, 85, 99, 0.30)',  text: '#9CA3AF' },
  1: { bg: 'rgba(232, 159, 42, 0.08)', border: 'rgba(232, 159, 42, 0.25)', text: '#E89F2A' },
  2: { bg: 'rgba(132, 204, 22, 0.08)', border: 'rgba(132, 204, 22, 0.25)', text: '#84CC16' },
  3: { bg: 'rgba(244, 114, 22, 0.10)', border: 'rgba(244, 114, 22, 0.30)', text: '#F47216' },
  4: { bg: 'rgba(239, 68, 68, 0.12)',  border: 'rgba(239, 68, 68, 0.40)',  text: '#EF4444' },
};

export default function AVSFactorChecklist({ factors, onToggle, preview }: Props) {
  const colors = SCORE_COLORS[preview.score];

  return (
    <div style={{
      marginTop: 14,
      padding: 12,
      borderRadius: 6,
      background: 'rgba(255,255,255,0.02)',
      border: '1px solid rgba(255,255,255,0.06)',
    }}>
      {/* Header with live score preview */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10 }}>
        <div style={{
          fontSize: 10, fontWeight: 700, letterSpacing: 1.2, color: '#9CA3AF',
          flex: 1,
        }}>
          TMA-AVS-01 VALIDATION FACTORS
        </div>
        <div style={{
          padding: '4px 10px',
          borderRadius: 4,
          fontSize: 11, fontWeight: 700, letterSpacing: 0.6,
          background: colors.bg,
          border: `1px solid ${colors.border}`,
          color: colors.text,
        }}>
          {preview.label} ({preview.score})
          {preview.dispatch && (
            <span style={{ marginLeft: 6, fontSize: 9, opacity: 0.85 }}>· DISPATCH</span>
          )}
        </div>
      </div>

      {/* Three priority bands */}
      {BANDS.map((band) => (
        <div key={band.title} style={{ marginBottom: 8 }}>
          <div style={{
            fontSize: 9, fontWeight: 600, letterSpacing: 1, color: '#6B7280',
            marginBottom: 4, textTransform: 'uppercase',
          }}>
            {band.title}
          </div>
          <div style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
            gap: 4,
          }}>
            {band.keys.map((key) => {
              const checked = factors[key];
              const meta = FACTOR_LABELS[key];
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => onToggle(key)}
                  title={meta.help}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    padding: '6px 8px',
                    borderRadius: 4,
                    background: checked ? 'rgba(132, 204, 22, 0.10)' : 'rgba(255,255,255,0.02)',
                    border: `1px solid ${checked ? 'rgba(132, 204, 22, 0.35)' : 'rgba(255,255,255,0.08)'}`,
                    color: checked ? '#A3E635' : '#9CA3AF',
                    fontFamily: 'inherit',
                    fontSize: 12,
                    fontWeight: checked ? 600 : 400,
                    textAlign: 'left',
                    cursor: 'pointer',
                    transition: 'all 0.1s',
                  }}
                >
                  <span style={{
                    width: 12, height: 12,
                    borderRadius: 2,
                    border: `1px solid ${checked ? '#84CC16' : '#4A5268'}`,
                    background: checked ? '#84CC16' : 'transparent',
                    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                    flexShrink: 0,
                    color: '#0a0f08', fontSize: 9, fontWeight: 800,
                  }}>
                    {checked ? '✓' : ''}
                  </span>
                  <span style={{ flex: 1, lineHeight: 1.2 }}>{meta.label}</span>
                </button>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}
