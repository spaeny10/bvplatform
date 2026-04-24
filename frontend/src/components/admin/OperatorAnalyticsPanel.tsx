'use client';

import { useState, useEffect } from 'react';
import { getOperatorMetrics } from '@/lib/ironsight-api';
import type { OperatorMetrics } from '@/types/ironsight';

export default function OperatorAnalyticsPanel() {
  const [metrics, setMetrics] = useState<OperatorMetrics[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getOperatorMetrics().then(data => { setMetrics(data); setLoading(false); });
  }, []);

  const formatTime = (ms: number) => {
    if (ms < 60000) return `${(ms / 1000).toFixed(0)}s`;
    return `${(ms / 60000).toFixed(1)}m`;
  };

  const avgSLA = metrics.length > 0 ? Math.round(metrics.reduce((s, m) => s + m.sla_compliance_pct, 0) / metrics.length) : 0;
  const totalEvents = metrics.reduce((s, m) => s + m.events_handled, 0);

  return (
    <div className="admin-card" style={{ marginTop: 16 }}>
      <div className="admin-card-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 16 }}>📊</span>
          <div>
            <div className="admin-card-title">Operator Analytics</div>
            <div style={{ fontSize: 10, color: '#4A5268', marginTop: 1 }}>Current shift · {metrics.length} operators</div>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
          <div style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: avgSLA >= 95 ? '#22C55E' : avgSLA >= 85 ? '#E89B2A' : '#EF4444' }}>{avgSLA}%</div>
            <div style={{ fontSize: 8, color: '#4A5268', letterSpacing: 1 }}>AVG SLA</div>
          </div>
          <div style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: '#E8732A' }}>{totalEvents}</div>
            <div style={{ fontSize: 8, color: '#4A5268', letterSpacing: 1 }}>EVENTS</div>
          </div>
        </div>
      </div>

      {loading && <div style={{ padding: 30, textAlign: 'center', color: '#4A5268' }}>Loading…</div>}

      {!loading && (
        <table className="admin-table" style={{ borderRadius: 0, border: 'none' }}>
          <thead>
            <tr>
              <th>Operator</th>
              <th>Shift</th>
              <th>Events</th>
              <th>Avg Response</th>
              <th>Avg Resolve</th>
              <th>SLA</th>
              <th>Escalated</th>
            </tr>
          </thead>
          <tbody>
            {metrics.map(m => (
              <tr key={m.operator_id}>
                <td>
                  <div style={{ fontWeight: 600, color: '#E4E8F0' }}>{m.operator_callsign}</div>
                  <div style={{ fontSize: 10, color: '#4A5268' }}>{m.operator_name}</div>
                </td>
                <td style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 11 }}>{m.shift_hours.toFixed(1)}h</td>
                <td style={{ fontWeight: 600, color: '#E8732A' }}>{m.events_handled}</td>
                <td>
                  <span style={{
                    fontFamily: "'JetBrains Mono', monospace", fontSize: 11, fontWeight: 600,
                    color: m.avg_response_time_ms < 20000 ? '#22C55E' : m.avg_response_time_ms < 30000 ? '#E89B2A' : '#EF4444',
                  }}>
                    {formatTime(m.avg_response_time_ms)}
                  </span>
                </td>
                <td style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 11, color: '#8891A5' }}>
                  {formatTime(m.avg_resolve_time_ms)}
                </td>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <div style={{
                      width: 40, height: 4, borderRadius: 2, background: 'rgba(255,255,255,0.06)',
                      overflow: 'hidden',
                    }}>
                      <div style={{
                        width: `${m.sla_compliance_pct}%`, height: '100%', borderRadius: 2,
                        background: m.sla_compliance_pct >= 95 ? '#22C55E' : m.sla_compliance_pct >= 85 ? '#E89B2A' : '#EF4444',
                      }} />
                    </div>
                    <span style={{
                      fontSize: 11, fontWeight: 600, fontFamily: "'JetBrains Mono', monospace",
                      color: m.sla_compliance_pct >= 95 ? '#22C55E' : m.sla_compliance_pct >= 85 ? '#E89B2A' : '#EF4444',
                    }}>
                      {m.sla_compliance_pct}%
                    </span>
                  </div>
                </td>
                <td style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 11, color: m.alerts_escalated > 0 ? '#EF4444' : '#4A5268' }}>
                  {m.alerts_escalated}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
