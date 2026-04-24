'use client';

import { useState, useCallback } from 'react';
import type { Severity } from '@/types/ironsight';

// ── vLM Safety Finding (auto-generated) ──
export interface SafetyFinding {
  id: string;
  site_id: string;
  site_name: string;
  camera_id: string;
  camera_name: string;
  ts: number;
  type: string;               // "no_hard_hat", "no_harness", etc.
  severity: Severity;
  caption: string;             // vLM-generated description
  confidence: number;          // 0-1
  thumbnail_url?: string;
  validation_status: 'pending' | 'true' | 'false';
  validated_by?: string;
  validated_at?: number;
  correction?: string;         // user correction on false
}

// ── Mock pending findings ──
const MOCK_FINDINGS: SafetyFinding[] = [
  { id: 'vlm-001', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-05', camera_name: 'Scaffold Tower', ts: Date.now() - 3600000, type: 'no_harness', severity: 'critical', caption: 'Worker at elevation on scaffold without fall protection harness visible. Height estimated 4+ meters.', confidence: 0.89, validation_status: 'pending' },
  { id: 'vlm-002', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-02', camera_name: 'Crane Zone A', ts: Date.now() - 7200000, type: 'no_hard_hat', severity: 'high', caption: 'Individual near active crane operation without hard hat. PPE non-compliance detected.', confidence: 0.92, validation_status: 'pending' },
  { id: 'vlm-003', site_id: 'GA-091', site_name: 'Atlanta Interchange', camera_id: 'cam-03', camera_name: 'Foundation', ts: Date.now() - 14400000, type: 'zone_breach', severity: 'medium', caption: 'Person detected inside exclusion zone near excavation pit. No safety spotter visible.', confidence: 0.76, validation_status: 'pending' },
  { id: 'vlm-004', site_id: 'CA-089', site_name: 'Marina Bay Tower', camera_id: 'cam-01', camera_name: 'North Perimeter', ts: Date.now() - 21600000, type: 'no_hi_vis', severity: 'medium', caption: 'Worker near roadway without high-visibility vest. Low contrast against background.', confidence: 0.71, validation_status: 'pending' },
  { id: 'vlm-005', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-04', camera_name: 'South Loading', ts: Date.now() - 28800000, type: 'vehicle_hazard', severity: 'high', caption: 'Forklift operating in pedestrian area without spotter. Close proximity to workers.', confidence: 0.85, validation_status: 'pending' },
];

// ── Correction options for active learning ──
const CORRECTION_OPTIONS = [
  { value: 'animal', label: 'Animal' },
  { value: 'equipment', label: 'Equipment / Machinery' },
  { value: 'shadow', label: 'Shadow / Lighting' },
  { value: 'already_resolved', label: 'Already Resolved' },
  { value: 'wrong_classification', label: 'Wrong Classification' },
  { value: 'other', label: 'Other' },
];

const SEV_COLORS: Record<string, string> = {
  critical: '#c0311a',
  high: '#a05800',
  medium: '#9a6f00',
  low: '#1a4f8a',
};

function formatRelative(ts: number): string {
  const d = Date.now() - ts;
  if (d < 3600000) return `${Math.floor(d / 60000)}m ago`;
  if (d < 86400000) return `${Math.floor(d / 3600000)}h ago`;
  return `${Math.floor(d / 86400000)}d ago`;
}

export default function PendingReviewQueue() {
  const [findings, setFindings] = useState<SafetyFinding[]>(MOCK_FINDINGS);
  const [correctionTarget, setCorrectionTarget] = useState<string | null>(null);
  const [correctionValue, setCorrectionValue] = useState('');
  const [toast, setToast] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(true);

  const pendingCount = findings.filter(f => f.validation_status === 'pending').length;

  const handleValidate = useCallback((id: string, valid: boolean) => {
    if (!valid) {
      // Show correction micro-interaction
      setCorrectionTarget(id);
      return;
    }
    setFindings(prev => prev.map(f =>
      f.id === id ? { ...f, validation_status: 'true', validated_at: Date.now() } : f
    ));
    setToast('Validated — added to compliance metrics');
    setTimeout(() => setToast(null), 2500);
  }, []);

  const handleSubmitCorrection = useCallback((id: string) => {
    setFindings(prev => prev.map(f =>
      f.id === id ? { ...f, validation_status: 'false', correction: correctionValue, validated_at: Date.now() } : f
    ));
    setCorrectionTarget(null);
    setCorrectionValue('');
    setToast('False positive recorded — training data captured');
    setTimeout(() => setToast(null), 3000);
    // In production: POST to /api/v1/ai-telemetry with anonymized payload
  }, [correctionValue]);

  const pending = findings.filter(f => f.validation_status === 'pending');

  return (
    <div className="portal-card" style={{ animation: 'portal-fadeUp 0.3s ease both' }}>
      {/* Header — always visible, click to toggle */}
      <div
        className="portal-card-header"
        onClick={() => setExpanded(e => !e)}
        style={{
          cursor: 'pointer', userSelect: 'none',
          borderBottom: expanded ? '1px solid var(--border)' : 'none',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <div className="portal-card-title">Pending Safety Review</div>
          {pendingCount > 0 && (
            <span style={{
              padding: '2px 8px', borderRadius: 10, fontSize: 10, fontWeight: 700,
              background: 'var(--accent-light, rgba(200,75,47,0.08))',
              color: 'var(--accent, #c84b2f)',
              border: '1px solid rgba(200,75,47,0.2)',
            }}>
              {pendingCount}
            </span>
          )}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ fontSize: 10, color: 'var(--text-dim)' }}>
            AI-detected · Validate or reject
          </div>
          <span style={{
            fontSize: 12, color: 'var(--text-dim)', display: 'inline-block',
            transition: 'transform 0.2s', transform: expanded ? 'rotate(180deg)' : 'rotate(0deg)',
          }}>
            ▾
          </span>
        </div>
      </div>

      {/* Collapsed notice — compact bar shown when folded with pending items */}
      {!expanded && pendingCount > 0 && (
        <div style={{
          display: 'flex', alignItems: 'center', gap: 8,
          padding: '7px 20px',
          background: 'rgba(200,75,47,0.05)',
        }}>
          <span style={{ fontSize: 12 }}>⚠️</span>
          <span style={{ fontSize: 11, color: 'var(--text-secondary)', flex: 1 }}>
            <strong style={{ color: 'var(--accent, #c84b2f)' }}>{pendingCount} AI safety detection{pendingCount !== 1 ? 's' : ''}</strong>
            {' '}awaiting review — unreviewed findings are excluded from compliance scores.
          </span>
          <button
            onClick={e => { e.stopPropagation(); setExpanded(true); }}
            style={{
              padding: '3px 10px', borderRadius: 4, fontSize: 10, fontWeight: 600,
              background: 'rgba(200,75,47,0.08)',
              border: '1px solid rgba(200,75,47,0.22)',
              color: 'var(--accent, #c84b2f)',
              cursor: 'pointer', fontFamily: 'inherit', flexShrink: 0,
            }}
          >
            Review →
          </button>
        </div>
      )}

      {expanded && pending.length === 0 && (
        <div style={{ padding: '24px 16px', textAlign: 'center', color: 'var(--text-dim)', fontSize: 12 }}>
          All caught up — no pending safety findings to review.
        </div>
      )}

      {expanded && pending.map(finding => (
        <div key={finding.id} style={{
          padding: '12px 16px',
          borderBottom: '1px solid var(--border, #e8e4dc)',
          display: 'flex', gap: 12, alignItems: 'flex-start',
        }}>
          {/* Thumbnail placeholder */}
          <div style={{
            width: 80, height: 52, borderRadius: 4, flexShrink: 0,
            background: 'linear-gradient(135deg, #141a0c, #1a200e 50%, #0a0f08)',
            position: 'relative', overflow: 'hidden',
          }}>
            {/* Mock detection box */}
            <div style={{
              position: 'absolute', left: '30%', top: '20%', width: '40%', height: '60%',
              border: `1.5px solid ${SEV_COLORS[finding.severity] || '#9a6f00'}`,
              borderRadius: 1,
            }} />
            <span style={{
              position: 'absolute', bottom: 2, right: 2, fontSize: 7,
              padding: '1px 3px', borderRadius: 2,
              background: 'rgba(0,0,0,0.7)', color: '#fff',
              fontFamily: "'JetBrains Mono', monospace",
            }}>
              {Math.round(finding.confidence * 100)}%
            </span>
          </div>

          {/* Content */}
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 3 }}>
              <span style={{
                padding: '1px 6px', borderRadius: 2, fontSize: 9, fontWeight: 700,
                textTransform: 'uppercase', letterSpacing: 0.5,
                background: `${SEV_COLORS[finding.severity]}12`,
                color: SEV_COLORS[finding.severity],
                border: `1px solid ${SEV_COLORS[finding.severity]}25`,
              }}>
                {finding.severity}
              </span>
              <span style={{ fontSize: 10, color: 'var(--text-dim)', fontFamily: "'JetBrains Mono', monospace" }}>
                {finding.type.replace(/_/g, ' ')}
              </span>
              <span style={{ fontSize: 9, color: 'var(--text-dim)', marginLeft: 'auto' }}>
                {formatRelative(finding.ts)}
              </span>
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-secondary, #6b6560)', lineHeight: 1.5, marginBottom: 6 }}>
              {finding.caption}
            </div>
            <div style={{ fontSize: 9, color: 'var(--text-dim)' }}>
              {finding.site_name} · {finding.camera_name}
            </div>

            {/* Correction micro-interaction */}
            {correctionTarget === finding.id && (
              <div style={{
                marginTop: 8, padding: '10px 12px', borderRadius: 6,
                background: 'var(--bg-warm, #faf9f6)',
                border: '1px solid var(--border, #e8e4dc)',
              }}>
                <div style={{ fontSize: 10, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 6 }}>
                  Help us improve — what did the AI actually see?
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginBottom: 8 }}>
                  {CORRECTION_OPTIONS.map(opt => (
                    <button
                      key={opt.value}
                      onClick={() => setCorrectionValue(opt.value)}
                      style={{
                        padding: '4px 10px', borderRadius: 4, fontSize: 10,
                        background: correctionValue === opt.value ? 'var(--accent-light, rgba(200,75,47,0.08))' : 'var(--bg-card, #fff)',
                        border: `1px solid ${correctionValue === opt.value ? 'var(--accent, #c84b2f)' : 'var(--border, #e8e4dc)'}`,
                        color: correctionValue === opt.value ? 'var(--accent, #c84b2f)' : 'var(--text-secondary)',
                        cursor: 'pointer', fontFamily: 'inherit',
                      }}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
                <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                  <button
                    onClick={() => { setCorrectionTarget(null); setCorrectionValue(''); }}
                    style={{
                      padding: '5px 12px', borderRadius: 4, fontSize: 10, fontWeight: 600,
                      background: 'var(--bg-card)', border: '1px solid var(--border)',
                      color: 'var(--text-dim)', cursor: 'pointer', fontFamily: 'inherit',
                    }}
                  >
                    Cancel
                  </button>
                  <button
                    onClick={() => handleSubmitCorrection(finding.id)}
                    disabled={!correctionValue}
                    style={{
                      padding: '5px 12px', borderRadius: 4, fontSize: 10, fontWeight: 700,
                      background: correctionValue ? 'var(--accent-light)' : 'var(--bg-card)',
                      border: `1px solid ${correctionValue ? 'var(--accent)' : 'var(--border)'}`,
                      color: correctionValue ? 'var(--accent)' : 'var(--text-dim)',
                      cursor: correctionValue ? 'pointer' : 'not-allowed', fontFamily: 'inherit',
                    }}
                  >
                    Submit Correction
                  </button>
                </div>
              </div>
            )}
          </div>

          {/* Action buttons */}
          {correctionTarget !== finding.id && (
            <div style={{ display: 'flex', gap: 4, flexShrink: 0 }}>
              <button
                onClick={() => handleValidate(finding.id, true)}
                title="Valid — add to compliance metrics"
                style={{
                  width: 32, height: 32, borderRadius: 4,
                  background: 'rgba(26,122,74,0.06)', border: '1px solid rgba(26,122,74,0.2)',
                  color: 'var(--green, #1a7a4a)', fontSize: 14,
                  cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center',
                }}
              >
                ✓
              </button>
              <button
                onClick={() => handleValidate(finding.id, false)}
                title="False positive — help train AI"
                style={{
                  width: 32, height: 32, borderRadius: 4,
                  background: 'rgba(200,75,47,0.06)', border: '1px solid rgba(200,75,47,0.15)',
                  color: 'var(--accent, #c84b2f)', fontSize: 14,
                  cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center',
                }}
              >
                ✕
              </button>
            </div>
          )}
        </div>
      ))}

      {/* Toast notification */}
      {toast && (
        <div style={{
          position: 'fixed', bottom: 60, left: '50%', transform: 'translateX(-50%)',
          padding: '8px 20px', borderRadius: 6, fontSize: 11, fontWeight: 600,
          background: 'var(--text-primary, #1a1814)', color: 'var(--bg, #f4f2ee)',
          boxShadow: '0 4px 16px rgba(0,0,0,0.2)',
          zIndex: 10000, animation: 'portal-fadeUp 0.2s ease',
        }}>
          {toast}
        </div>
      )}
    </div>
  );
}
