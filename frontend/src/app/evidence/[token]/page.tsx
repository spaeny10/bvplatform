'use client';

import { useState, useEffect } from 'react';
import Logo from '@/components/shared/Logo';
import { BRAND } from '@/lib/branding';

interface EvidenceData {
  incident_id: string;
  site_name: string;
  camera_name: string;
  ts: number;
  clip_url: string;
  disposition: string;
  operator_notes: string;
  expires_at: string | null;
  status: 'valid' | 'expired' | 'revoked' | 'not_found';
}

// Mock evidence lookup
function mockFetchEvidence(token: string): EvidenceData {
  if (token === 'expired') {
    return { incident_id: '', site_name: '', camera_name: '', ts: 0, clip_url: '', disposition: '', operator_notes: '', expires_at: null, status: 'expired' };
  }
  return {
    incident_id: 'EVT-2026-0312',
    site_name: 'Southgate Power Station',
    camera_name: 'North Perimeter — Gate A',
    ts: Date.now() - 28800000,
    clip_url: '',
    disposition: 'Verified — Police Dispatched',
    operator_notes: 'Individual observed entering through Gate A at 02:14. Appeared to be carrying tools. Police dispatch confirmed at 02:17. Officers arrived 02:31. Individual fled on foot prior to arrival.',
    expires_at: new Date(Date.now() + 7 * 86400000).toISOString(),
    status: 'valid',
  };
}

export default function EvidenceViewerPage({ params }: { params: { token: string } }) {
  const [evidence, setEvidence] = useState<EvidenceData | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    // In production: GET /api/v1/evidence/:token
    const data = mockFetchEvidence(params.token);
    setEvidence(data);
    setLoading(false);
  }, [params.token]);

  if (loading) {
    return (
      <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: '#0A0C10', fontFamily: "'Inter', sans-serif", color: '#4A5268' }}>
        Loading evidence...
      </div>
    );
  }

  if (!evidence || evidence.status === 'not_found') {
    return (
      <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: '#0A0C10', fontFamily: "'Inter', sans-serif" }}>
        <div style={{ textAlign: 'center', maxWidth: 400 }}>
          <div style={{ fontSize: 48, marginBottom: 16 }}>🔒</div>
          <div style={{ fontSize: 18, fontWeight: 700, color: '#E4E8F0', marginBottom: 8 }}>Evidence Not Found</div>
          <div style={{ fontSize: 13, color: '#4A5268', lineHeight: 1.6 }}>
            This evidence link is invalid. Please contact the site manager to request access.
          </div>
        </div>
      </div>
    );
  }

  if (evidence.status === 'expired' || evidence.status === 'revoked') {
    return (
      <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: '#0A0C10', fontFamily: "'Inter', sans-serif" }}>
        <div style={{ textAlign: 'center', maxWidth: 440, padding: '0 20px' }}>
          <div style={{ fontSize: 48, marginBottom: 16 }}>⏰</div>
          <div style={{ fontSize: 18, fontWeight: 700, color: '#E4E8F0', marginBottom: 8 }}>
            {evidence.status === 'expired' ? 'Evidence Link Expired' : 'Evidence Link Revoked'}
          </div>
          <div style={{ fontSize: 13, color: '#4A5268', lineHeight: 1.6, marginBottom: 20 }}>
            This evidence link is no longer active. Please contact the Site Manager to request a new access link.
          </div>
          <div style={{
            padding: '12px 16px', borderRadius: 8,
            background: '#0E1117', border: '1px solid rgba(255,255,255,0.07)',
            fontSize: 11, color: '#8891A5',
          }}>
            Powered by <strong>{BRAND.name}</strong> — {BRAND.tagline}
          </div>
        </div>
      </div>
    );
  }

  // Valid evidence view
  return (
    <div style={{ minHeight: '100vh', background: '#0A0C10', fontFamily: "'Inter', sans-serif", color: '#E4E8F0' }}>
      {/* Header */}
      <div style={{
        background: '#E4E8F0', padding: '14px 24px',
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <Logo height={18} />
          <span style={{ fontSize: 10, color: 'rgba(248,247,245,0.4)', padding: '2px 8px', borderRadius: 2, border: '1px solid rgba(248,247,245,0.1)' }}>
            EVIDENCE VIEWER
          </span>
        </div>
        <span style={{
          fontSize: 9, color: 'rgba(248,247,245,0.35)',
          fontFamily: "'JetBrains Mono', monospace",
        }}>
          {evidence.incident_id}
        </span>
      </div>

      <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 20px' }}>
        {/* Evidence locked badge */}
        <div style={{
          display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16,
          padding: '8px 14px', borderRadius: 6,
          background: 'rgba(20,72,160,0.06)', border: '1px solid rgba(20,72,160,0.15)',
        }}>
          <span style={{ fontSize: 14 }}>🔒</span>
          <span style={{ fontSize: 11, fontWeight: 600, color: '#1448a0' }}>Evidence Locked</span>
          <span style={{ fontSize: 10, color: '#4A5268', marginLeft: 'auto' }}>
            {evidence.expires_at ? `Expires ${new Date(evidence.expires_at).toLocaleDateString()}` : 'No expiration'}
          </span>
        </div>

        {/* Video player area */}
        <div style={{
          background: '#0a0806', borderRadius: 8, overflow: 'hidden',
          marginBottom: 20, border: '1px solid rgba(255,255,255,0.07)',
        }}>
          <div style={{
            aspectRatio: '16/9', position: 'relative',
            background: 'linear-gradient(180deg, #080c08 0%, #101808 45%, #181c10 100%)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}>
            {/* Mock detection overlay */}
            <div style={{
              position: 'absolute', left: '35%', top: '20%', width: '12%', height: '50%',
              border: '2px solid #e05040', borderRadius: 2,
              boxShadow: '0 0 8px rgba(224,80,64,0.3)',
            }}>
              <span style={{
                position: 'absolute', bottom: '100%', left: -1, marginBottom: 2,
                fontSize: 9, fontWeight: 700, padding: '2px 6px', borderRadius: 2,
                background: 'rgba(0,0,0,0.85)', color: '#e05040',
                fontFamily: "'JetBrains Mono', monospace",
              }}>
                PERSON 94%
              </span>
            </div>

            {/* Timestamp overlay */}
            <div style={{
              position: 'absolute', bottom: 8, left: 12, right: 12,
              display: 'flex', justifyContent: 'space-between',
              fontSize: 10, color: 'rgba(255,255,255,0.4)',
              fontFamily: "'JetBrains Mono', monospace",
            }}>
              <span>{evidence.camera_name}</span>
              <span>{new Date(evidence.ts).toLocaleString('en-US', { hour12: false })}</span>
            </div>

            {/* REC badge */}
            <div style={{
              position: 'absolute', top: 8, right: 8,
              fontSize: 8, padding: '2px 6px', borderRadius: 2,
              background: 'rgba(20,72,160,0.8)', color: '#90b8f0',
              border: '1px solid rgba(90,140,220,0.3)',
              fontFamily: "'JetBrains Mono', monospace", fontWeight: 700,
            }}>
              🔒 EVIDENCE LOCKED
            </div>
          </div>
        </div>

        {/* Event details */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 20 }}>
          <div style={{
            padding: '14px 16px', borderRadius: 8,
            background: '#0E1117', border: '1px solid rgba(255,255,255,0.07)',
          }}>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase', color: '#4A5268', marginBottom: 6 }}>
              Event Details
            </div>
            <div style={{ fontSize: 12, lineHeight: 1.8, color: '#8891A5' }}>
              <div><strong>Site:</strong> {evidence.site_name}</div>
              <div><strong>Camera:</strong> {evidence.camera_name}</div>
              <div><strong>Time:</strong> {new Date(evidence.ts).toLocaleString('en-US', { hour12: false })}</div>
              <div><strong>Event ID:</strong> <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 11 }}>{evidence.incident_id}</span></div>
            </div>
          </div>

          <div style={{
            padding: '14px 16px', borderRadius: 8,
            background: '#0E1117', border: '1px solid rgba(255,255,255,0.07)',
          }}>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase', color: '#4A5268', marginBottom: 6 }}>
              Disposition
            </div>
            <div style={{
              padding: '6px 10px', borderRadius: 4, marginBottom: 8,
              background: 'rgba(192,49,26,0.06)', border: '1px solid rgba(192,49,26,0.16)',
              fontSize: 12, fontWeight: 600, color: '#c0311a',
            }}>
              {evidence.disposition}
            </div>
            <div style={{ fontSize: 11, color: '#8891A5', lineHeight: 1.6 }}>
              {evidence.operator_notes}
            </div>
          </div>
        </div>

        {/* Footer */}
        <div style={{
          textAlign: 'center', padding: '16px 0',
          borderTop: '1px solid rgba(255,255,255,0.07)',
          fontSize: 10, color: '#4A5268',
        }}>
          Powered by <strong>{BRAND.name}</strong> — {BRAND.tagline}
        </div>
      </div>
    </div>
  );
}
