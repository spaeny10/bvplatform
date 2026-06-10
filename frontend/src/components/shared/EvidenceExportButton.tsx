'use client';

import { useState, useCallback } from 'react';
import { createEvidenceShareLink, downloadAuthenticated, type EvidenceShareRequest } from '@/lib/ironsight-api';
import { resolveMediaURL } from '@/lib/media';
import { useFeatureFlag } from '@/lib/feature-flags';

// F-07: this component used to (a) POST /api/v1/incidents/{id}/evidence —
// a route that never existed, so "Export MP4" hung at "Packaging…" until
// reload — and (b) fabricate share tokens client-side after a fake 800 ms
// delay while claiming "clip synced to cloud". Both are gone:
//
//   - Export MP4 downloads the incident's real clip through the signed
//     media-mint pipeline (resolveMediaURL → /media/v1/<token>), with an
//     error state instead of a permanent spinner. Disabled with honest
//     copy when the incident has no clip.
//   - Generate Secure Link calls the real POST /api/v1/incidents/{id}/share
//     (supervisor/admin only, 90-day TTL ceiling) and is gated behind the
//     parked `evidence_sharing` flag so customers don't see it at MVP.

const EXPIRY_OPTIONS: { value: EvidenceShareRequest['expires_in']; label: string }[] = [
  { value: '1h', label: '1 Hour' },
  { value: '1d', label: '1 Day' },
  { value: '1w', label: '1 Week' },
  { value: '1m', label: '1 Month' },
];

interface Props {
  incidentId: string;
  /** The incident's clip_url as returned by the API (legacy
   *  /recordings/<cam>/<file>[#t=] shape or signed /media/v1/...).
   *  Absent/empty → the export button renders disabled. */
  clipUrl?: string;
}

export default function EvidenceExportButton({ incidentId, clipUrl }: Props) {
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);
  const [showMenu, setShowMenu] = useState(false);
  const [showShareModal, setShowShareModal] = useState(false);
  const [shareExpiry, setShareExpiry] = useState<EvidenceShareRequest['expires_in']>('1w');
  const [shareLink, setShareLink] = useState<string | null>(null);
  const [shareExpiresAt, setShareExpiresAt] = useState<string | null>(null);
  const [shareError, setShareError] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);

  const { enabled: sharingEnabled } = useFeatureFlag('evidence_sharing');

  const handleExport = async () => {
    if (!clipUrl || exporting) return;
    setExporting(true);
    setExportError(null);
    setShowMenu(false);
    try {
      // Strip any #t= seek fragment — irrelevant for a download.
      const resolved = await resolveMediaURL(clipUrl.split('#')[0]);
      if (!resolved) throw new Error('clip could not be resolved');
      await downloadAuthenticated(resolved, `incident-${incidentId}.mp4`);
    } catch (e) {
      setExportError(e instanceof Error ? e.message : String(e));
    } finally {
      setExporting(false);
    }
  };

  const handleGenerateLink = useCallback(async () => {
    setGenerating(true);
    setShareError(null);
    try {
      const result = await createEvidenceShareLink({ incident_id: incidentId, expires_in: shareExpiry });
      setShareLink(`${window.location.origin}${result.url}`);
      setShareExpiresAt(result.expires_at);
    } catch (e) {
      setShareError(e instanceof Error ? e.message : String(e));
    } finally {
      setGenerating(false);
    }
  }, [incidentId, shareExpiry]);

  const copyLink = () => {
    if (shareLink) navigator.clipboard.writeText(shareLink).catch(() => {});
  };

  // ── Share modal ──
  if (showShareModal) {
    return (
      <>
        <button onClick={() => setShowShareModal(false)} style={{
          padding: '6px 14px', fontSize: 11, fontWeight: 600,
          background: 'rgba(168,85,247,0.1)', border: '1px solid rgba(168,85,247,0.3)',
          borderRadius: 4, color: '#a855f7', cursor: 'pointer', fontFamily: 'inherit',
        }}>
          ← Back
        </button>
        <div style={{
          position: 'fixed', inset: 0, zIndex: 10000,
          background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }} onClick={() => setShowShareModal(false)}>
          <div
            onClick={e => e.stopPropagation()}
            style={{
              background: 'linear-gradient(180deg, #0E1117 0%, #080c10 100%)',
              border: '1px solid rgba(255,255,255,0.08)', borderRadius: 12,
              padding: 0, width: 420, boxShadow: '0 16px 64px rgba(0,0,0,0.6)',
            }}
          >
            <div style={{
              padding: '16px 20px', borderBottom: '1px solid rgba(255,255,255,0.06)',
              display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            }}>
              <div style={{ fontSize: 14, fontWeight: 700, color: '#E4E8F0' }}>🔗 Generate Secure Link</div>
              <button onClick={() => setShowShareModal(false)} style={{
                background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
                borderRadius: 4, padding: '4px 8px', color: '#4A5268', cursor: 'pointer', fontSize: 11,
              }}>✕</button>
            </div>

            <div style={{ padding: '16px 20px' }}>
              {!shareLink ? (
                <>
                  <div style={{ fontSize: 11, color: '#8891A5', marginBottom: 12, lineHeight: 1.5 }}>
                    Generate an expiring public link to this incident&apos;s evidence
                    for law enforcement or external parties. Every open is logged
                    for chain of custody.
                  </div>

                  <div style={{ marginBottom: 14 }}>
                    <label style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', letterSpacing: 0.5, display: 'block', marginBottom: 6 }}>
                      Link Expiration
                    </label>
                    <div style={{ display: 'flex', gap: 4 }}>
                      {EXPIRY_OPTIONS.map(opt => (
                        <button
                          key={opt.value}
                          onClick={() => setShareExpiry(opt.value)}
                          style={{
                            flex: 1, padding: '6px 0', borderRadius: 4, fontSize: 10, fontWeight: 600,
                            background: shareExpiry === opt.value ? 'rgba(0,212,255,0.1)' : 'rgba(255,255,255,0.02)',
                            border: `1px solid ${shareExpiry === opt.value ? 'rgba(0,212,255,0.3)' : 'rgba(255,255,255,0.06)'}`,
                            color: shareExpiry === opt.value ? '#E8732A' : '#4A5268',
                            cursor: 'pointer', fontFamily: 'inherit',
                          }}
                        >
                          {opt.label}
                        </button>
                      ))}
                    </div>
                  </div>

                  {shareError && (
                    <div style={{
                      marginBottom: 12, padding: '8px 10px', borderRadius: 4, fontSize: 10,
                      background: 'rgba(192,49,26,0.08)', border: '1px solid rgba(192,49,26,0.25)', color: '#e87060',
                    }}>
                      Failed to create link: {shareError}
                    </div>
                  )}

                  <button
                    onClick={handleGenerateLink}
                    disabled={generating}
                    style={{
                      width: '100%', padding: '10px', borderRadius: 6, fontSize: 12, fontWeight: 700,
                      background: 'rgba(0,212,255,0.1)', border: '1px solid rgba(0,212,255,0.3)',
                      color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
                      opacity: generating ? 0.5 : 1,
                    }}
                  >
                    {generating ? '⏳ Generating…' : '🔗 Generate Link'}
                  </button>
                </>
              ) : (
                <>
                  <div style={{ fontSize: 11, color: '#22C55E', fontWeight: 600, marginBottom: 10 }}>
                    ✓ Link created
                  </div>
                  <div style={{
                    display: 'flex', gap: 6, marginBottom: 12,
                    padding: '8px 10px', borderRadius: 4,
                    background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
                  }}>
                    <input
                      readOnly value={shareLink}
                      style={{
                        flex: 1, background: 'none', border: 'none', color: '#E8732A',
                        fontSize: 10, fontFamily: "'JetBrains Mono', monospace", outline: 'none',
                      }}
                    />
                    <button onClick={copyLink} style={{
                      padding: '4px 10px', borderRadius: 3, fontSize: 9, fontWeight: 600,
                      background: 'rgba(0,212,255,0.08)', border: '1px solid rgba(0,212,255,0.2)',
                      color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
                    }}>
                      📋 Copy
                    </button>
                  </div>
                  <div style={{ fontSize: 9, color: '#4A5268' }}>
                    Expires: {shareExpiresAt ? new Date(shareExpiresAt).toLocaleString() : 'per server policy'}
                    {' · '}Revocable from Reports → Evidence Shares.
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </>
    );
  }

  // ── Split button (share half only when evidence_sharing is on) ──
  return (
    <div style={{ position: 'relative', display: 'flex', alignItems: 'center', gap: 8 }}>
      {exportError && (
        <span style={{
          fontSize: 9, padding: '3px 8px', borderRadius: 3, maxWidth: 220,
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const,
          background: 'rgba(192,49,26,0.08)', border: '1px solid rgba(192,49,26,0.25)', color: '#e87060',
        }} title={exportError}>
          Export failed: {exportError}
        </span>
      )}
      <div style={{ display: 'flex' }}>
        <button
          onClick={handleExport}
          disabled={exporting || !clipUrl}
          title={clipUrl ? 'Download the incident clip as MP4' : 'No clip available for this incident'}
          style={{
            padding: '6px 12px', fontSize: 11, fontWeight: 600,
            background: exporting || !clipUrl ? 'rgba(255,255,255,0.04)' : 'rgba(168,85,247,0.1)',
            border: `1px solid ${exporting || !clipUrl ? 'rgba(255,255,255,0.06)' : 'rgba(168,85,247,0.3)'}`,
            borderRadius: sharingEnabled ? '4px 0 0 4px' : 4,
            color: exporting || !clipUrl ? '#4A5268' : '#a855f7',
            cursor: exporting ? 'wait' : clipUrl ? 'pointer' : 'not-allowed', fontFamily: 'inherit',
            display: 'flex', alignItems: 'center', gap: 6,
          }}
        >
          {exporting ? '⏳ Downloading…' : '📦 Export MP4'}
        </button>
        {sharingEnabled && (
          <button
            onClick={() => setShowMenu(v => !v)}
            style={{
              padding: '6px 6px', fontSize: 10,
              background: 'rgba(168,85,247,0.1)',
              border: '1px solid rgba(168,85,247,0.3)', borderLeft: 'none',
              borderRadius: '0 4px 4px 0', color: '#a855f7',
              cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ▾
          </button>
        )}
      </div>

      {sharingEnabled && showMenu && (
        <div style={{
          position: 'absolute', top: '100%', right: 0, marginTop: 4,
          background: '#0E1117', border: '1px solid rgba(255,255,255,0.08)',
          borderRadius: 6, padding: 4, minWidth: 180,
          boxShadow: '0 8px 24px rgba(0,0,0,0.5)', zIndex: 100,
        }}>
          <button
            onClick={() => {
              setShowShareModal(true);
              setShowMenu(false);
              setShareLink(null);
              setShareExpiresAt(null);
              setShareError(null);
            }}
            style={{
              display: 'block', width: '100%', padding: '8px 10px', borderRadius: 3,
              background: 'transparent', border: 'none', color: '#8891A5',
              fontSize: 11, textAlign: 'left', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            🔗 Generate Secure Link
          </button>
        </div>
      )}
    </div>
  );
}
