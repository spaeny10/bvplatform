'use client';

import { useState, useEffect } from 'react';
import { getScheduledReports, toggleScheduledReport } from '@/lib/ironsight-api';
import type { ScheduledReport } from '@/types/ironsight';

const TYPE_ICONS: Record<string, string> = { safety: '🛡️', compliance: '✅', incidents: '🚨', executive: '📈' };
const FREQ_LABELS: Record<string, string> = { daily: 'Daily', weekly: 'Weekly', monthly: 'Monthly' };

export default function ReportSchedulerPanel() {
  const [reports, setReports] = useState<ScheduledReport[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getScheduledReports().then(data => { setReports(data); setLoading(false); });
  }, []);

  const handleToggle = async (reportId: string, enabled: boolean) => {
    setReports(prev => prev.map(r => r.id === reportId ? { ...r, enabled } : r));
    await toggleScheduledReport(reportId, enabled);
  };

  return (
    <div className="admin-card" style={{ marginTop: 16 }}>
      <div className="admin-card-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 16 }}>📅</span>
          <div>
            <div className="admin-card-title">Scheduled Reports</div>
            <div style={{ fontSize: 10, color: '#4A5268', marginTop: 1 }}>{reports.filter(r => r.enabled).length} active</div>
          </div>
        </div>
      </div>

      {loading && <div style={{ padding: 30, textAlign: 'center', color: '#4A5268' }}>Loading…</div>}

      {!loading && reports.map(report => (
        <div key={report.id} style={{
          display: 'flex', alignItems: 'center', gap: 12, padding: '10px 16px',
          borderBottom: '1px solid rgba(255,255,255,0.04)',
          opacity: report.enabled ? 1 : 0.4,
        }}>
          <span style={{ fontSize: 18 }}>{TYPE_ICONS[report.type] || '📄'}</span>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: '#E4E8F0' }}>{report.name}</div>
            <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>
              {FREQ_LABELS[report.frequency]} · {report.site_ids.length || 'All'} sites · {report.recipients.length} recipients
            </div>
            {report.last_run && (
              <div style={{ fontSize: 9, color: '#4A5268' }}>
                Last: {new Date(report.last_run).toLocaleDateString()} · Next: {new Date(report.next_run).toLocaleDateString()}
              </div>
            )}
          </div>
          <span style={{
            fontSize: 8, padding: '2px 6px', borderRadius: 2, fontWeight: 600,
            letterSpacing: 0.5, textTransform: 'uppercase' as const,
            background: `rgba(${report.frequency === 'daily' ? '0,212,255' : report.frequency === 'weekly' ? '168,85,247' : '0,229,160'},0.1)`,
            color: report.frequency === 'daily' ? '#E8732A' : report.frequency === 'weekly' ? '#a855f7' : '#22C55E',
            border: `1px solid rgba(${report.frequency === 'daily' ? '0,212,255' : report.frequency === 'weekly' ? '168,85,247' : '0,229,160'},0.25)`,
          }}>
            {report.frequency}
          </span>
          <label style={{ cursor: 'pointer', display: 'flex', alignItems: 'center' }}>
            <input type="checkbox" checked={report.enabled} onChange={e => handleToggle(report.id, e.target.checked)} style={{ accentColor: '#8b5cf6', width: 14, height: 14 }} />
          </label>
        </div>
      ))}
    </div>
  );
}
