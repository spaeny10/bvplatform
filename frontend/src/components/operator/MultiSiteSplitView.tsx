'use client';

import { useState, useMemo } from 'react';
import { MOCK_SITES, MOCK_SITE_DETAIL } from '@/lib/ironsight-mock';

interface Props {
  onClose: () => void;
  onSiteSelect: (siteId: string) => void;
}

const MOCK_DET_TYPES = ['person-ok', 'person-violation', 'vehicle', 'equipment'] as const;

function MiniCameraCell({ camName, camId, hasViolation }: { camName: string; camId: string; hasViolation: boolean }) {
  return (
    <div style={{
      position: 'relative',
      background: hasViolation
        ? 'linear-gradient(135deg, #14100a 0%, #1a0e0e 100%)'
        : 'linear-gradient(135deg, #0a1510 0%, #0e1206 100%)',
      borderRadius: 4,
      border: `1px solid ${hasViolation ? 'rgba(255,51,85,0.25)' : 'rgba(255,255,255,0.04)'}`,
      overflow: 'hidden',
      aspectRatio: '16/9',
      cursor: 'pointer',
      transition: 'border-color 0.2s',
    }}>
      {/* Simulated scene gradient */}
      <div style={{
        position: 'absolute', inset: 0,
        background: hasViolation
          ? 'radial-gradient(ellipse at 40% 50%, rgba(255,51,85,0.05) 0%, transparent 70%)'
          : 'radial-gradient(ellipse at 40% 50%, rgba(0,229,160,0.03) 0%, transparent 70%)',
      }} />

      {/* Camera ID */}
      <div style={{
        position: 'absolute', top: 3, left: 4,
        fontSize: 7, fontWeight: 700, color: '#E4E8F0',
        fontFamily: "'JetBrains Mono', monospace",
      }}>
        {camId.toUpperCase()}
      </div>

      {/* LIVE badge */}
      <div style={{
        position: 'absolute', top: 3, right: 4,
        fontSize: 6, fontWeight: 700, padding: '1px 4px',
        background: 'rgba(255,51,85,0.8)', color: '#fff',
        borderRadius: 2, letterSpacing: 0.5,
      }}>
        ● LIVE
      </div>

      {/* Camera name */}
      <div style={{
        position: 'absolute', bottom: 3, left: 4,
        fontSize: 7, color: '#8891A5',
      }}>
        {camName}
      </div>

      {/* Violation indicator */}
      {hasViolation && (
        <div style={{
          position: 'absolute', bottom: 3, right: 4,
          fontSize: 6, fontWeight: 700, padding: '1px 4px',
          background: 'rgba(255,51,85,0.15)', color: '#EF4444',
          border: '1px solid rgba(255,51,85,0.3)',
          borderRadius: 2,
        }}>
          ⚠
        </div>
      )}
    </div>
  );
}

export default function MultiSiteSplitView({ onClose, onSiteSelect }: Props) {
  const [selectedSites, setSelectedSites] = useState<string[]>(
    MOCK_SITES.slice(0, 4).map(s => s.id)
  );
  const [layout, setLayout] = useState<'2x2' | '1x4' | '3x1'>('2x2');

  const siteDetails = useMemo(() => {
    return selectedSites.map(id => ({
      ...MOCK_SITE_DETAIL(id),
      summary: MOCK_SITES.find(s => s.id === id)!,
    }));
  }, [selectedSites]);

  const gridStyle: React.CSSProperties = {
    display: 'grid',
    gap: 8,
    flex: 1,
    gridTemplateColumns: layout === '2x2' ? '1fr 1fr' : layout === '1x4' ? '1fr' : '1fr 1fr 1fr',
    gridTemplateRows: layout === '2x2' ? '1fr 1fr' : layout === '1x4' ? 'repeat(4, 1fr)' : '1fr',
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 7000,
      background: '#080c10',
      display: 'flex', flexDirection: 'column',
      animation: 'cam-fullscreen-enter 0.2s ease-out',
    }}>
      {/* Toolbar */}
      <div style={{
        height: 40, display: 'flex', alignItems: 'center', padding: '0 12px',
        background: 'rgba(12,17,24,0.95)', borderBottom: '1px solid rgba(255,255,255,0.06)',
        gap: 8, flexShrink: 0,
      }}>
        <span style={{
          fontSize: 11, fontWeight: 700, letterSpacing: 1.5,
          color: '#E8732A', fontFamily: "'JetBrains Mono', monospace",
        }}>
          ◇ MULTI-SITE VIEW
        </span>

        <span style={{ fontSize: 10, color: '#4A5268', marginLeft: 8 }}>
          {selectedSites.length} sites active
        </span>

        <div style={{ marginLeft: 'auto', display: 'flex', gap: 4 }}>
          {(['2x2', '1x4', '3x1'] as const).map(l => (
            <button
              key={l}
              onClick={() => setLayout(l)}
              style={{
                padding: '3px 10px', borderRadius: 3, fontSize: 10,
                background: layout === l ? 'rgba(0,212,255,0.1)' : 'rgba(255,255,255,0.03)',
                border: `1px solid ${layout === l ? 'rgba(0,212,255,0.3)' : 'rgba(255,255,255,0.06)'}`,
                color: layout === l ? '#E8732A' : '#8891A5',
                cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace", fontWeight: 600,
              }}
            >
              {l === '2x2' ? '◫ 2×2' : l === '1x4' ? '▬ Stack' : '▤ Triple'}
            </button>
          ))}
          <button
            onClick={onClose}
            style={{
              padding: '3px 10px', borderRadius: 3, fontSize: 10,
              background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.06)',
              color: '#4A5268', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ✕ Close
          </button>
        </div>
      </div>

      {/* Site Grid */}
      <div style={{ ...gridStyle, padding: 8, overflow: 'hidden' }}>
        {siteDetails.map(site => {
          const statusColor = site.summary.status === 'critical' ? '#EF4444'
            : site.summary.status === 'active' ? '#22C55E' : '#4A5268';

          return (
            <div
              key={site.id}
              style={{
                background: '#0E1117',
                border: '1px solid rgba(255,255,255,0.04)',
                borderRadius: 6,
                display: 'flex',
                flexDirection: 'column',
                overflow: 'hidden',
                cursor: 'pointer',
              }}
              onClick={() => { onSiteSelect(site.id); onClose(); }}
            >
              {/* Site header */}
              <div style={{
                padding: '6px 10px',
                borderBottom: '1px solid rgba(255,255,255,0.04)',
                display: 'flex', alignItems: 'center', gap: 8,
                background: 'rgba(0,0,0,0.2)',
              }}>
                <div style={{
                  width: 6, height: 6, borderRadius: '50%',
                  background: statusColor,
                  boxShadow: `0 0 6px ${statusColor}`,
                }} />
                <span style={{
                  fontSize: 10, fontWeight: 700, color: '#E4E8F0',
                  letterSpacing: 0.5, flex: 1,
                }}>
                  {site.name}
                </span>
                <span style={{
                  fontSize: 8, color: '#4A5268',
                  fontFamily: "'JetBrains Mono', monospace",
                }}>
                  {site.id}
                </span>
                <span style={{
                  fontSize: 8, fontWeight: 700, padding: '1px 6px', borderRadius: 8,
                  background: site.summary.compliance_score >= 90 ? 'rgba(0,229,160,0.1)' : site.summary.compliance_score >= 75 ? 'rgba(255,204,0,0.1)' : 'rgba(255,51,85,0.1)',
                  color: site.summary.compliance_score >= 90 ? '#22C55E' : site.summary.compliance_score >= 75 ? '#E89B2A' : '#EF4444',
                  border: `1px solid ${site.summary.compliance_score >= 90 ? 'rgba(0,229,160,0.2)' : site.summary.compliance_score >= 75 ? 'rgba(255,204,0,0.2)' : 'rgba(255,51,85,0.2)'}`,
                  fontFamily: "'JetBrains Mono', monospace",
                }}>
                  {site.summary.compliance_score}%
                </span>
              </div>

              {/* Camera mini-grid */}
              <div style={{
                flex: 1,
                display: 'grid',
                gridTemplateColumns: 'repeat(3, 1fr)',
                gap: 3,
                padding: 4,
                overflow: 'hidden',
              }}>
                {site.cameras.slice(0, 6).map((cam, ci) => (
                  <MiniCameraCell
                    key={cam.id}
                    camId={cam.id}
                    camName={cam.name}
                    hasViolation={cam.has_alert}
                  />
                ))}
              </div>

              {/* Site footer stats */}
              <div style={{
                padding: '4px 10px',
                borderTop: '1px solid rgba(255,255,255,0.04)',
                display: 'flex', gap: 12,
                fontSize: 8, color: '#4A5268',
                fontFamily: "'JetBrains Mono', monospace",
                background: 'rgba(0,0,0,0.15)',
              }}>
                <span>📹 {site.cameras.length} cams</span>
                <span>👷 {site.summary.workers_on_site} workers</span>
                <span style={{
                  color: site.summary.open_incidents > 0 ? '#EF4444' : '#22C55E',
                }}>
                  ⚠ {site.summary.open_incidents} incidents
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
