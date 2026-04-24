'use client';

import { useState, useCallback } from 'react';
import { generateEvidencePackage } from '@/lib/ironsight-api';
import type { EvidencePackage } from '@/types/ironsight';

const EXPIRY_OPTIONS = [
  { value: '1h', label: '1 Hour' },
  { value: '1d', label: '1 Day' },
  { value: '1w', label: '1 Week' },
  { value: '1m', label: '1 Month' },
  { value: 'never', label: 'Never' },
];

interface Props { incidentId: string; }

export default function EvidenceExportButton({ incidentId }: Props) {
  const [exporting, setExporting] = useState(false);
  const [result, setResult] = useState<EvidencePackage | null>(null);
  const [showMenu, setShowMenu] = useState(false);
  const [showShareModal, setShowShareModal] = useState(false);
  const [shareExpiry, setShareExpiry] = useState('1w');
  const [shareLink, setShareLink] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);

  const handleExport = async () => {
    setExporting(true);
    const pkg = await generateEvidencePackage(incidentId, 'Generated from incident detail view');
    setResult(pkg);
    setExporting(false);
    setShowMenu(false);
  };

  const handleGenerateLink = useCallback(async () => {
    setGenerating(true);
    // In production: POST /api/v1/evidence/share { incident_id, expires_in: shareExpiry }
    // Backend generates token, triggers cloud sync from NVR, returns URL
    await new Promise(r => setTimeout(r, 800));
    const token = `ev-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
    setShareLink(`${window.location.origin}/evidence/${token}`);
    setGenerating(false);
  }, [shareExpiry]);

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
                    Generate a shareable link for law enforcement or external parties.
                    The clip will be synced to cloud storage for reliable access.
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
                    {generating ? '⏳ Syncing to cloud & generating...' : '🔗 Generate Link'}
                  </button>
                </>
              ) : (
                <>
                  <div style={{ fontSize: 11, color: '#22C55E', fontWeight: 600, marginBottom: 10 }}>
                    ✓ Link generated — clip synced to cloud
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
                    Expires: {shareExpiry === 'never' ? 'Never' : `${EXPIRY_OPTIONS.find(o => o.value === shareExpiry)?.label}`}
                    {' · '}You can revoke this link at any time from the admin panel.
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </>
    );
  }

  // ── Result state ──
  if (result) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{
          fontSize: 10, padding: '4px 10px', background: 'rgba(0,229,160,0.1)',
          border: '1px solid rgba(0,229,160,0.25)', borderRadius: 4,
          color: '#22C55E', fontWeight: 600,
        }}>
          ✓ Evidence Ready
        </span>
        <button onClick={() => setResult(null)} style={{
          fontSize: 9, padding: '3px 8px', background: 'rgba(255,255,255,0.04)',
          border: '1px solid rgba(255,255,255,0.08)', borderRadius: 3,
          color: '#8891A5', cursor: 'pointer',
        }}>
          📥 Download MP4
        </button>
      </div>
    );
  }

  // ── Split button ──
  return (
    <div style={{ position: 'relative', display: 'flex' }}>
      <button onClick={handleExport} disabled={exporting} style={{
        padding: '6px 12px', fontSize: 11, fontWeight: 600,
        background: exporting ? 'rgba(255,255,255,0.04)' : 'rgba(168,85,247,0.1)',
        border: `1px solid ${exporting ? 'rgba(255,255,255,0.06)' : 'rgba(168,85,247,0.3)'}`,
        borderRadius: '4px 0 0 4px', color: exporting ? '#4A5268' : '#a855f7',
        cursor: exporting ? 'wait' : 'pointer', fontFamily: 'inherit',
        display: 'flex', alignItems: 'center', gap: 6,
      }}>
        {exporting ? '⏳ Packaging…' : '📦 Export MP4'}
      </button>
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

      {showMenu && (
        <div style={{
          position: 'absolute', top: '100%', right: 0, marginTop: 4,
          background: '#0E1117', border: '1px solid rgba(255,255,255,0.08)',
          borderRadius: 6, padding: 4, minWidth: 180,
          boxShadow: '0 8px 24px rgba(0,0,0,0.5)', zIndex: 100,
        }}>
          <button onClick={handleExport} style={{
            display: 'block', width: '100%', padding: '8px 10px', borderRadius: 3,
            background: 'transparent', border: 'none', color: '#8891A5',
            fontSize: 11, textAlign: 'left', cursor: 'pointer', fontFamily: 'inherit',
          }}>
            📥 Download MP4
          </button>
          <button onClick={() => { setShowShareModal(true); setShowMenu(false); setShareLink(null); }} style={{
            display: 'block', width: '100%', padding: '8px 10px', borderRadius: 3,
            background: 'transparent', border: 'none', color: '#8891A5',
            fontSize: 11, textAlign: 'left', cursor: 'pointer', fontFamily: 'inherit',
          }}>
            🔗 Generate Secure Link
          </button>
        </div>
      )}
    </div>
  );
}
