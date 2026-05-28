'use client';

import { useRef } from 'react';
import type { LastDisposition } from '@/stores/operator-store';
import { DISPOSITION_OPTIONS } from '@/stores/operator-store';
import SeverityPill from '@/components/shared/SeverityPill';
import SignedImage from '@/components/shared/SignedImage';

interface Props {
  lastDisposition: LastDisposition;
  operatorName: string;
  operatorCallsign: string;
  onClose: () => void;
}

function formatTs(ms: number): string {
  return new Date(ms).toLocaleString('en-US', {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  });
}

function formatDuration(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${s % 60}s`;
}

export default function EvidenceExportModal({ lastDisposition, operatorName, operatorCallsign, onClose }: Props) {
  const reportRef = useRef<HTMLDivElement>(null);
  const { alarm, dispositionCode, actionLog, resolvedAt, responseMs } = lastDisposition;
  const dispOption = DISPOSITION_OPTIONS.find(d => d.code === dispositionCode);
  const reportId = `RPT-${new Date(resolvedAt).getFullYear()}-${String(resolvedAt).slice(-6)}`;
  const isVerified = dispositionCode.startsWith('verified');

  const handlePrint = () => {
    const printContents = reportRef.current?.innerHTML;
    if (!printContents) return;
    const win = window.open('', '_blank', 'width=800,height=1000');
    if (!win) return;
    win.document.write(`
      <!DOCTYPE html>
      <html>
        <head>
          <title>${reportId} — Security Incident Report</title>
          <style>
            * { margin: 0; padding: 0; box-sizing: border-box; }
            body { font-family: 'Segoe UI', sans-serif; font-size: 11px; color: #1a1a2e; background: white; padding: 32px; }
            .report-header { display: flex; justify-content: space-between; align-items: flex-start; border-bottom: 2px solid #1a1a2e; padding-bottom: 14px; margin-bottom: 20px; }
            .report-title { font-size: 20px; font-weight: 800; letter-spacing: -0.5px; }
            .report-meta { text-align: right; font-size: 10px; color: #555; line-height: 1.6; }
            .section { margin-bottom: 20px; }
            .section-title { font-size: 9px; font-weight: 700; letter-spacing: 2px; text-transform: uppercase; color: #888; margin-bottom: 8px; border-bottom: 1px solid #eee; padding-bottom: 4px; }
            .detail-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 6px 20px; }
            .detail-item label { font-size: 8px; text-transform: uppercase; letter-spacing: 1px; color: #888; display: block; }
            .detail-item span { font-size: 11px; font-weight: 600; }
            .disposition-badge { display: inline-block; padding: 4px 12px; border-radius: 4px; font-weight: 700; font-size: 11px; background: ${isVerified ? '#fef2f2' : '#f0fdf4'}; color: ${isVerified ? '#ef4444' : '#22c55e'}; border: 1px solid ${isVerified ? '#fecaca' : '#bbf7d0'}; }
            .timeline-entry { display: flex; gap: 10px; padding: 4px 0; border-bottom: 1px solid #f5f5f5; }
            .timeline-dot { width: 6px; height: 6px; border-radius: 50%; background: ${isVerified ? '#ef4444' : '#22c55e'}; margin-top: 4px; flex-shrink: 0; }
            .timeline-dot.auto { background: #bbb; }
            .timeline-time { font-size: 9px; color: #888; font-family: monospace; white-space: nowrap; }
            .timeline-text { font-size: 10px; color: #333; font-style: normal; }
            .timeline-text.auto { color: #999; font-style: italic; }
            .snapshot-placeholder { width: 100%; height: 180px; background: #f5f7fa; border: 1px solid #e0e0e0; border-radius: 4px; display: flex; align-items: center; justify-content: center; color: #bbb; font-size: 10px; }
            .footer { margin-top: 32px; padding-top: 12px; border-top: 1px solid #ddd; display: flex; justify-content: space-between; font-size: 9px; color: #aaa; }
            .sig-line { border-bottom: 1px solid #333; width: 180px; margin-top: 32px; }
            .sig-label { font-size: 8px; color: #888; margin-top: 4px; }
            @media print { body { padding: 20px; } }
          </style>
        </head>
        <body>${printContents}</body>
      </html>
    `);
    win.document.close();
    win.focus();
    setTimeout(() => { win.print(); win.close(); }, 500);
  };

  return (
    <div
      onClick={e => { if (e.target === e.currentTarget) onClose(); }}
      style={{
        position: 'fixed', inset: 0, zIndex: 400,
        background: 'rgba(5,7,10,0.92)', backdropFilter: 'blur(16px)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        padding: '20px',
        overflowY: 'auto',
      }}
    >
      <div style={{
        width: 680, background: '#0E1117',
        border: '1px solid rgba(255,255,255,0.1)',
        borderRadius: 12,
        boxShadow: '0 32px 96px rgba(0,0,0,0.8)',
        overflow: 'hidden',
        maxHeight: '90vh', overflowY: 'auto',
      }}>
        {/* Modal header */}
        <div style={{
          padding: '12px 16px', position: 'sticky', top: 0, zIndex: 10,
          background: '#0E1117',
          borderBottom: '1px solid rgba(255,255,255,0.07)',
          display: 'flex', alignItems: 'center', gap: 10,
        }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 12, fontWeight: 700, color: '#E4E8F0' }}>Evidence Package</div>
            <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>{reportId}</div>
          </div>
          <button
            onClick={handlePrint}
            style={{
              padding: '7px 16px', borderRadius: 5, fontSize: 11, fontWeight: 700,
              background: 'rgba(232,115,42,0.12)', border: '1px solid rgba(232,115,42,0.3)',
              color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            🖨 Print / Download PDF
          </button>
          <button
            onClick={onClose}
            style={{
              padding: '6px 12px', borderRadius: 5, fontSize: 11,
              background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
              color: '#6B7590', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ✕
          </button>
        </div>

        {/* Printable report body */}
        <div ref={reportRef} style={{ padding: '24px 28px' }}>
          {/* Report header */}
          <div className="report-header" style={{
            display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start',
            borderBottom: '2px solid rgba(255,255,255,0.1)', paddingBottom: 16, marginBottom: 20,
          }}>
            <div>
              <div style={{ fontSize: 8, fontWeight: 700, letterSpacing: 2, color: '#E8732A', marginBottom: 4 }}>
                IRONSIGHT SECURITY
              </div>
              <div style={{ fontSize: 18, fontWeight: 800, color: '#E4E8F0', letterSpacing: -0.5 }}>
                Security Incident Report
              </div>
            </div>
            <div style={{ textAlign: 'right', fontSize: 9, color: '#4A5268', lineHeight: 1.8, fontFamily: "'JetBrains Mono', monospace" }}>
              <div style={{ fontWeight: 700, color: '#8891A5' }}>{reportId}</div>
              <div>Generated: {formatTs(Date.now())}</div>
              <div>Resolved: {formatTs(resolvedAt)}</div>
            </div>
          </div>

          {/* Snapshot */}
          {alarm.snapshot_url && (
            <div className="section" style={{ marginBottom: 20 }}>
              <div style={{ fontSize: 8, fontWeight: 700, letterSpacing: 1.5, color: '#4A5268', textTransform: 'uppercase', marginBottom: 8 }}>
                Event Capture
              </div>
              <div style={{ position: 'relative', borderRadius: 6, overflow: 'hidden', background: '#080a06', height: 200 }}>
                <SignedImage
                  src={alarm.snapshot_url}
                  alt="Event capture"
                  style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
                  onError={e => { (e.target as HTMLImageElement).parentElement!.style.display = 'none'; }}
                />
                <div style={{
                  position: 'absolute', bottom: 8, left: 8, right: 8,
                  display: 'flex', justifyContent: 'space-between',
                }}>
                  <span style={{
                    fontSize: 8, padding: '2px 8px', borderRadius: 3, fontWeight: 700,
                    background: 'rgba(0,0,0,0.8)', color: '#EF4444',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}>
                    ⚠ {alarm.type.replace(/_/g, ' ').toUpperCase()}
                  </span>
                  <span style={{
                    fontSize: 8, padding: '2px 8px', borderRadius: 3,
                    background: 'rgba(0,0,0,0.8)', color: 'rgba(255,255,255,0.5)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}>
                    {formatTs(alarm.ts)}
                  </span>
                </div>
              </div>
            </div>
          )}

          {/* Event details */}
          <div style={{ marginBottom: 20 }}>
            <div style={{ fontSize: 8, fontWeight: 700, letterSpacing: 1.5, color: '#4A5268', textTransform: 'uppercase', marginBottom: 10, borderBottom: '1px solid rgba(255,255,255,0.06)', paddingBottom: 4 }}>
              Event Details
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px 24px' }}>
              {[
                { label: 'Alarm ID', value: alarm.id },
                { label: 'Event Type', value: alarm.type.replace(/_/g, ' ') },
                { label: 'Site', value: alarm.site_name },
                { label: 'Camera', value: alarm.camera_name },
                { label: 'Severity', value: alarm.severity.toUpperCase() },
                { label: 'Triggered', value: formatTs(alarm.ts) },
                { label: 'Response Time', value: formatDuration(responseMs) },
                { label: 'Escalation Level', value: String(alarm.escalation_level || 0) },
              ].map(item => (
                <div key={item.label}>
                  <div style={{ fontSize: 8, textTransform: 'uppercase', letterSpacing: 1, color: '#4A5268', marginBottom: 2 }}>{item.label}</div>
                  <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>{item.value}</div>
                </div>
              ))}
            </div>
          </div>

          {/* Disposition */}
          <div style={{ marginBottom: 20 }}>
            <div style={{ fontSize: 8, fontWeight: 700, letterSpacing: 1.5, color: '#4A5268', textTransform: 'uppercase', marginBottom: 10, borderBottom: '1px solid rgba(255,255,255,0.06)', paddingBottom: 4 }}>
              Disposition
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <span style={{
                padding: '5px 14px', borderRadius: 5, fontWeight: 700, fontSize: 11,
                background: isVerified ? 'rgba(239,68,68,0.12)' : 'rgba(34,197,94,0.12)',
                border: `1px solid ${isVerified ? 'rgba(239,68,68,0.35)' : 'rgba(34,197,94,0.35)'}`,
                color: isVerified ? '#EF4444' : '#22C55E',
              }}>
                {dispOption?.label || dispositionCode}
              </span>
              <SeverityPill severity={alarm.severity} size="sm" />
            </div>
            {lastDisposition.actionLog.find(e => e.text.startsWith('Notes:')) && (
              <div style={{
                marginTop: 8, padding: '8px 12px', borderRadius: 4,
                background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)',
                fontSize: 10, color: '#8891A5', fontStyle: 'italic',
              }}>
                {lastDisposition.actionLog.find(e => e.text.startsWith('Notes:'))!.text.replace('Notes: ', '')}
              </div>
            )}
          </div>

          {/* Action log */}
          <div style={{ marginBottom: 24 }}>
            <div style={{ fontSize: 8, fontWeight: 700, letterSpacing: 1.5, color: '#4A5268', textTransform: 'uppercase', marginBottom: 10, borderBottom: '1px solid rgba(255,255,255,0.06)', paddingBottom: 4 }}>
              Operator Action Log ({actionLog.length} entries)
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
              {actionLog.map((entry, i) => (
                <div key={i} style={{ display: 'flex', gap: 8, alignItems: 'flex-start', padding: '4px 0', borderBottom: '1px solid rgba(255,255,255,0.03)' }}>
                  <div style={{
                    width: 6, height: 6, borderRadius: '50%', marginTop: 3, flexShrink: 0,
                    background: entry.auto ? 'rgba(74,82,104,0.5)' : '#E8732A',
                  }} />
                  <span style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", flexShrink: 0, width: 58 }}>
                    {new Date(entry.ts).toLocaleTimeString('en-US', { hour12: false })}
                  </span>
                  <span style={{ fontSize: 10, color: entry.auto ? '#4A5268' : '#8891A5', fontStyle: entry.auto ? 'italic' : 'normal', lineHeight: 1.4 }}>
                    {entry.text}
                  </span>
                </div>
              ))}
            </div>
          </div>

          {/* Operator signature block */}
          <div style={{
            padding: '14px 16px', borderRadius: 6,
            background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)',
            display: 'flex', justifyContent: 'space-between', alignItems: 'center',
          }}>
            <div>
              <div style={{ fontSize: 9, color: '#4A5268', marginBottom: 3 }}>Responding Operator</div>
              <div style={{ fontSize: 13, fontWeight: 700, color: '#E4E8F0' }}>{operatorName}</div>
              <div style={{ fontSize: 9, fontFamily: "'JetBrains Mono', monospace", color: '#E8732A' }}>{operatorCallsign}</div>
            </div>
            <div style={{ textAlign: 'right' }}>
              <div style={{ fontSize: 9, color: '#4A5268', marginBottom: 3 }}>Report Generated</div>
              <div style={{ fontSize: 11, color: '#8891A5', fontFamily: "'JetBrains Mono', monospace" }}>{formatTs(Date.now())}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
