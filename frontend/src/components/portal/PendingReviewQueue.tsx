'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import type { PendingReviewEntry } from '@/types/ironsight';
import { getPendingReview, submitReview } from '@/lib/api';

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

// Derive a rough severity from YOLO confidence for badge colour.
function confidenceToSeverity(confidence: number): string {
  if (confidence >= 0.85) return 'critical';
  if (confidence >= 0.70) return 'high';
  if (confidence >= 0.55) return 'medium';
  return 'low';
}

function formatRelative(iso: string): string {
  const d = Date.now() - new Date(iso).getTime();
  if (d < 3600000) return `${Math.floor(d / 60000)}m ago`;
  if (d < 86400000) return `${Math.floor(d / 3600000)}h ago`;
  return `${Math.floor(d / 86400000)}d ago`;
}

// ── Skeleton loading rows ──────────────────────────────────────────────────────
function SkeletonRows() {
  return (
    <>
      {[0, 1, 2].map(i => (
        <div key={i} style={{
          padding: '12px 16px', borderBottom: '1px solid var(--border, #e8e4dc)',
          display: 'flex', gap: 12, alignItems: 'flex-start',
        }}>
          <div style={{
            width: 80, height: 52, borderRadius: 4,
            background: 'var(--bg-warm, #f0ede8)', flexShrink: 0,
          }} />
          <div style={{ flex: 1 }}>
            <div style={{
              height: 10, width: '40%', background: 'var(--bg-warm, #f0ede8)',
              borderRadius: 3, marginBottom: 6,
            }} />
            <div style={{
              height: 8, width: '80%', background: 'var(--bg-warm, #f0ede8)',
              borderRadius: 3, marginBottom: 4,
            }} />
            <div style={{
              height: 8, width: '60%', background: 'var(--bg-warm, #f0ede8)',
              borderRadius: 3,
            }} />
          </div>
        </div>
      ))}
    </>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────
export default function PendingReviewQueue() {
  const [entries, setEntries] = useState<PendingReviewEntry[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [correctionTarget, setCorrectionTarget] = useState<string | null>(null);
  const [correctionValue, setCorrectionValue] = useState('');
  const [toast, setToast] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(true);

  // WS listener ref — updated on each render so the closure captures current entries.
  const entriesRef = useRef(entries);
  entriesRef.current = entries;

  // ── Load on mount ────────────────────────────────────────────────────────────
  const loadQueue = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await getPendingReview({ status: 'pending' });
      setEntries(data.entries ?? []);
    } catch (e) {
      setError('Could not load safety findings');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    loadQueue();
  }, [loadQueue]);

  // ── WebSocket subscription: prepend new ppe_detected entries ─────────────────
  useEffect(() => {
    // The WS connection is established at the page/layout level. We listen
    // for ppe_detected messages on the global WS via a custom event emitted
    // by the layout's WS handler, or fall back to polling if the WS mechanism
    // uses a different bridge. For Phase 2 the simplest integration is to
    // listen on window for the 'ws:message' custom event pattern the layout
    // already dispatches, or attach directly if a WSContext is available.
    //
    // We use a window-level event listener as the integration point because
    // the existing WS context in the portal is (per code inspection) shared
    // at the layout level. If the WS context exposes a subscribe method,
    // replace this with that.
    const handler = (event: Event) => {
      // Cast to unknown record so we can access top-level fields (type, camera_id,
      // organization_id) alongside the nested data object without TypeScript errors.
      const msg = (event as CustomEvent<Record<string, unknown>>).detail;
      if (!msg || msg['type'] !== 'ppe_detected') return;
      const data = msg['data'] as Record<string, unknown> | undefined;
      if (!data) return;

      // Construct a minimal PendingReviewEntry from the WS payload.
      // The full entry details come from the next loadQueue() call.
      // For immediate UX, we add a placeholder with the WS data.
      const partial: PendingReviewEntry = {
        id: String(data['queue_entry_id'] ?? ''),
        camera_id: String(msg['camera_id'] ?? ''),
        camera_name: '',
        detection_class: String(data['detection_class'] ?? ''),
        missing_label: String(data['missing_label'] ?? ''),
        confidence: Number(data['confidence'] ?? 0),
        bounding_boxes: [],
        frame_url: data['queue_entry_id']
          ? `/api/v1/portal/pending-review/${data['queue_entry_id']}/frame`
          : '',
        status: 'pending',
        created_at: String(data['created_at'] ?? new Date().toISOString()),
      };

      if (!partial.id) return; // ignore malformed events

      setEntries(prev => {
        // Avoid duplicates (e.g. from race between WS + initial load).
        if (prev.some(e => e.id === partial.id)) return prev;
        return [partial, ...prev];
      });
    };

    window.addEventListener('ironsight:ws:message', handler as EventListener);
    return () => window.removeEventListener('ironsight:ws:message', handler as EventListener);
  }, []);

  const pendingCount = entries.filter(e => e.status === 'pending').length;

  // ── Validate (true positive) ─────────────────────────────────────────────────
  const handleValidate = useCallback(async (id: string) => {
    try {
      await submitReview(id, { status: 'reviewed_violation' });
      setEntries(prev => prev.filter(e => e.id !== id));
      setToast('Validated — added to compliance metrics');
      setTimeout(() => setToast(null), 2500);
    } catch (e) {
      setToast('Failed to submit review — please try again');
      setTimeout(() => setToast(null), 3000);
    }
  }, []);

  // ── Open correction micro-interaction ────────────────────────────────────────
  const handleFalsePositive = useCallback((id: string) => {
    setCorrectionTarget(id);
  }, []);

  // ── Submit false-positive correction ────────────────────────────────────────
  const handleSubmitCorrection = useCallback(async (id: string) => {
    try {
      await submitReview(id, {
        status: 'dismissed',
        notes: correctionValue,
      });
      setEntries(prev => prev.filter(e => e.id !== id));
      setCorrectionTarget(null);
      setCorrectionValue('');
      setToast('False positive recorded — training data captured');
      setTimeout(() => setToast(null), 3000);
    } catch (e) {
      setToast('Failed to submit correction — please try again');
      setTimeout(() => setToast(null), 3000);
    }
  }, [correctionValue]);

  const pending = entries.filter(e => e.status === 'pending');

  return (
    <div className="portal-card" style={{ animation: 'portal-fadeUp 0.3s ease both' }}>
      {/* Header */}
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

      {/* Collapsed notice */}
      {!expanded && pendingCount > 0 && (
        <div style={{
          display: 'flex', alignItems: 'center', gap: 8,
          padding: '7px 20px', background: 'rgba(200,75,47,0.05)',
        }}>
          <span style={{ fontSize: 12 }}>⚠</span>
          <span style={{ fontSize: 11, color: 'var(--text-secondary)', flex: 1 }}>
            <strong style={{ color: 'var(--accent, #c84b2f)' }}>{pendingCount} AI safety detection{pendingCount !== 1 ? 's' : ''}</strong>
            {' '}awaiting review — unreviewed findings are excluded from compliance scores.
          </span>
          <button
            onClick={e => { e.stopPropagation(); setExpanded(true); }}
            style={{
              padding: '3px 10px', borderRadius: 4, fontSize: 10, fontWeight: 600,
              background: 'rgba(200,75,47,0.08)', border: '1px solid rgba(200,75,47,0.22)',
              color: 'var(--accent, #c84b2f)', cursor: 'pointer', fontFamily: 'inherit', flexShrink: 0,
            }}
          >
            Review →
          </button>
        </div>
      )}

      {/* Loading state */}
      {expanded && isLoading && <SkeletonRows />}

      {/* Error state */}
      {expanded && !isLoading && error && (
        <div style={{ padding: '20px 16px', textAlign: 'center' }}>
          <div style={{ fontSize: 11, color: 'var(--accent, #c84b2f)', marginBottom: 8 }}>{error}</div>
          <button
            onClick={loadQueue}
            style={{
              padding: '5px 14px', borderRadius: 4, fontSize: 10, fontWeight: 600,
              background: 'var(--accent-light)', border: '1px solid var(--accent)',
              color: 'var(--accent)', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            Retry
          </button>
        </div>
      )}

      {/* Empty state */}
      {expanded && !isLoading && !error && pending.length === 0 && (
        <div style={{ padding: '24px 16px', textAlign: 'center', color: 'var(--text-dim)', fontSize: 12 }}>
          <div style={{ fontWeight: 600, marginBottom: 4 }}>No PPE alerts to review</div>
          <div>Your team is fully compliant — check back as new footage is analyzed.</div>
        </div>
      )}

      {/* Entries */}
      {expanded && !isLoading && !error && pending.map(entry => {
        const severity = confidenceToSeverity(entry.confidence);
        return (
          <div key={entry.id} style={{
            padding: '12px 16px', borderBottom: '1px solid var(--border, #e8e4dc)',
            display: 'flex', gap: 12, alignItems: 'flex-start',
          }}>
            {/* Thumbnail */}
            <div style={{
              width: 80, height: 52, borderRadius: 4, flexShrink: 0,
              background: 'linear-gradient(135deg, #141a0c, #1a200e 50%, #0a0f08)',
              position: 'relative', overflow: 'hidden',
            }}>
              {entry.frame_url && (
                <img
                  src={entry.frame_url}
                  alt="PPE detection frame"
                  style={{ width: '100%', height: '100%', objectFit: 'cover', opacity: 0.9 }}
                  onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }}
                />
              )}
              {/* Detection box overlay */}
              {entry.bounding_boxes.length > 0 && (
                <div style={{
                  position: 'absolute',
                  left: `${entry.bounding_boxes[0].bbox_normalized.x1 * 100}%`,
                  top: `${entry.bounding_boxes[0].bbox_normalized.y1 * 100}%`,
                  width: `${(entry.bounding_boxes[0].bbox_normalized.x2 - entry.bounding_boxes[0].bbox_normalized.x1) * 100}%`,
                  height: `${(entry.bounding_boxes[0].bbox_normalized.y2 - entry.bounding_boxes[0].bbox_normalized.y1) * 100}%`,
                  border: `1.5px solid ${SEV_COLORS[severity] || '#9a6f00'}`,
                  borderRadius: 1,
                }} />
              )}
              <span style={{
                position: 'absolute', bottom: 2, right: 2, fontSize: 7,
                padding: '1px 3px', borderRadius: 2,
                background: 'rgba(0,0,0,0.7)', color: '#fff',
                fontFamily: "'JetBrains Mono', monospace",
              }}>
                {Math.round(entry.confidence * 100)}%
              </span>
            </div>

            {/* Content */}
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 3 }}>
                <span style={{
                  padding: '1px 6px', borderRadius: 2, fontSize: 9, fontWeight: 700,
                  textTransform: 'uppercase', letterSpacing: 0.5,
                  background: `${SEV_COLORS[severity]}12`,
                  color: SEV_COLORS[severity],
                  border: `1px solid ${SEV_COLORS[severity]}25`,
                }}>
                  {severity}
                </span>
                <span style={{ fontSize: 10, color: 'var(--text-dim)', fontFamily: "'JetBrains Mono', monospace" }}>
                  {entry.missing_label}
                </span>
                <span style={{ fontSize: 9, color: 'var(--text-dim)', marginLeft: 'auto' }}>
                  {formatRelative(entry.created_at)}
                </span>
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-secondary, #6b6560)', lineHeight: 1.5, marginBottom: 6 }}>
                Missing: <strong>{entry.missing_label}</strong>
                {' '}— class <code style={{ fontSize: 9 }}>{entry.detection_class}</code>
              </div>
              <div style={{ fontSize: 9, color: 'var(--text-dim)' }}>
                {entry.site_name && `${entry.site_name} · `}{entry.camera_name || entry.camera_id.slice(0, 8)}
              </div>

              {/* Correction micro-interaction */}
              {correctionTarget === entry.id && (
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
                      onClick={() => handleSubmitCorrection(entry.id)}
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
            {correctionTarget !== entry.id && (
              <div style={{ display: 'flex', gap: 4, flexShrink: 0 }}>
                <button
                  onClick={() => handleValidate(entry.id)}
                  title="Valid violation — add to compliance metrics"
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
                  onClick={() => handleFalsePositive(entry.id)}
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
        );
      })}

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
