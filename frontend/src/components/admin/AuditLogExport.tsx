'use client';

import { useState, useMemo, useCallback } from 'react';
import { BRAND } from '@/lib/branding';

// Slug used for downloaded CSV/JSON filenames. `BRAND.shortName` lowercased
// with spaces → hyphens, so "Sky Watch" becomes "sky-watch-audit-log...csv".
const DOWNLOAD_SLUG = BRAND.shortName.toLowerCase().replace(/\s+/g, '-');

interface AuditEntry {
  id: string;
  ts: number;
  action: string;
  user: string;
  role: string;
  detail: string;
  ip: string;
  severity: 'info' | 'warning' | 'critical';
}

function generateMockAuditLog(): AuditEntry[] {
  const actions = [
    { action: 'LOGIN', detail: 'Authenticated via SSO', severity: 'info' as const },
    { action: 'ALERT_CLAIMED', detail: 'Claimed alert ALT-2847 at Southgate Power', severity: 'info' as const },
    { action: 'ALERT_ESCALATED', detail: 'Alert ALT-2834 auto-escalated to L2', severity: 'warning' as const },
    { action: 'SITE_LOCKED', detail: 'Locked site TX-203 (Southgate Power Station)', severity: 'info' as const },
    { action: 'SITE_UNLOCKED', detail: 'Released lock on site BY-445', severity: 'info' as const },
    { action: 'INCIDENT_CREATED', detail: 'Created incident INC-0847 — Worker without harness', severity: 'warning' as const },
    { action: 'INCIDENT_RESOLVED', detail: 'Resolved incident INC-0841', severity: 'info' as const },
    { action: 'HANDOFF_INITIATED', detail: 'Shift handoff from OP-1 to OP-3', severity: 'info' as const },
    { action: 'CONFIG_CHANGED', detail: 'Updated escalation rule for critical alerts', severity: 'warning' as const },
    { action: 'USER_CREATED', detail: 'Created user account for emily@client.com', severity: 'info' as const },
    { action: 'ROLE_CHANGED', detail: 'Changed user jvance role to site_manager', severity: 'critical' as const },
    { action: 'SLA_BREACH', detail: 'SLA breached on ALT-2839 — 4m 23s response time', severity: 'critical' as const },
    { action: 'REPORT_EXPORTED', detail: 'Exported weekly compliance report (PDF)', severity: 'info' as const },
    { action: 'CAMERA_OFFLINE', detail: 'CAM-03 at Riverside Bridge went offline', severity: 'warning' as const },
    { action: 'LOGOUT', detail: 'Session ended', severity: 'info' as const },
  ];

  const users = [
    { name: 'Marcus Chen', role: 'soc_operator' },
    { name: 'Sarah Rodriguez', role: 'soc_supervisor' },
    { name: 'Admin', role: 'admin' },
    { name: 'J. Vance', role: 'site_manager' },
    { name: 'System', role: 'system' },
  ];

  const entries: AuditEntry[] = [];
  for (let i = 0; i < 60; i++) {
    const a = actions[Math.floor(Math.random() * actions.length)];
    const u = users[Math.floor(Math.random() * users.length)];
    entries.push({
      id: `AUD-${String(9000 + i).padStart(4, '0')}`,
      ts: Date.now() - i * (300000 + Math.random() * 600000),
      action: a.action,
      user: u.name,
      role: u.role,
      detail: a.detail,
      ip: `192.168.1.${100 + Math.floor(Math.random() * 50)}`,
      severity: a.severity,
    });
  }
  return entries.sort((a, b) => b.ts - a.ts);
}

function formatTimestamp(ts: number): string {
  return new Date(ts).toLocaleString('en-US', {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  });
}

function formatCSVDate(ts: number): string {
  return new Date(ts).toISOString();
}

const SEVERITY_COLORS: Record<string, { bg: string; text: string }> = {
  info: { bg: 'rgba(0,212,255,0.06)', text: '#E8732A' },
  warning: { bg: 'rgba(255,204,0,0.06)', text: '#E89B2A' },
  critical: { bg: 'rgba(255,51,85,0.06)', text: '#EF4444' },
};

interface Props {
  onClose?: () => void;
}

export default function AuditLogExport({ onClose }: Props) {
  const allEntries = useMemo(generateMockAuditLog, []);
  const [dateRange, setDateRange] = useState<'24h' | '7d' | '30d' | 'all'>('7d');
  const [actionFilter, setActionFilter] = useState<string>('all');
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');

  const filteredEntries = useMemo(() => {
    let entries = allEntries;

    // Date range
    const now = Date.now();
    if (dateRange === '24h') entries = entries.filter(e => now - e.ts < 86400000);
    else if (dateRange === '7d') entries = entries.filter(e => now - e.ts < 7 * 86400000);
    else if (dateRange === '30d') entries = entries.filter(e => now - e.ts < 30 * 86400000);

    // Action filter
    if (actionFilter !== 'all') entries = entries.filter(e => e.action === actionFilter);

    // Severity filter
    if (severityFilter !== 'all') entries = entries.filter(e => e.severity === severityFilter);

    // Search
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      entries = entries.filter(e =>
        e.detail.toLowerCase().includes(q) ||
        e.user.toLowerCase().includes(q) ||
        e.action.toLowerCase().includes(q) ||
        e.id.toLowerCase().includes(q)
      );
    }

    return entries;
  }, [allEntries, dateRange, actionFilter, severityFilter, searchQuery]);

  const uniqueActions = useMemo(() => Array.from(new Set(allEntries.map(e => e.action))).sort(), [allEntries]);

  const exportCSV = useCallback(() => {
    const headers = ['Timestamp', 'ID', 'Action', 'User', 'Role', 'Detail', 'IP', 'Severity'];
    const rows = filteredEntries.map(e => [
      formatCSVDate(e.ts), e.id, e.action, e.user, e.role, `"${e.detail}"`, e.ip, e.severity,
    ]);
    const csv = [headers.join(','), ...rows.map(r => r.join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${DOWNLOAD_SLUG}-audit-log-${new Date().toISOString().split('T')[0]}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }, [filteredEntries]);

  const exportJSON = useCallback(() => {
    const json = JSON.stringify({
      exported_at: new Date().toISOString(),
      total_entries: filteredEntries.length,
      filters: { dateRange, actionFilter, severityFilter, searchQuery },
      entries: filteredEntries.map(e => ({
        ...e,
        timestamp: new Date(e.ts).toISOString(),
      })),
    }, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${DOWNLOAD_SLUG}-audit-log-${new Date().toISOString().split('T')[0]}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }, [filteredEntries, dateRange, actionFilter, severityFilter, searchQuery]);

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 8000,
      background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      animation: 'cam-fullscreen-enter 0.2s ease-out',
    }}>
      <div style={{
        width: '90vw', maxWidth: 1100, maxHeight: '85vh',
        background: 'linear-gradient(180deg, #0E1117 0%, #080c10 100%)',
        border: '1px solid rgba(255,255,255,0.06)',
        borderRadius: 10,
        display: 'flex', flexDirection: 'column',
        boxShadow: '0 16px 64px rgba(0,0,0,0.5)',
        overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{
          padding: '16px 20px',
          borderBottom: '1px solid rgba(255,255,255,0.06)',
          display: 'flex', alignItems: 'center', gap: 12,
          background: 'rgba(0,0,0,0.2)',
        }}>
          <span style={{ fontSize: 16 }}>📋</span>
          <div style={{ flex: 1 }}>
            <div style={{
              fontSize: 13, fontWeight: 700, letterSpacing: 1,
              color: '#E4E8F0', textTransform: 'uppercase',
            }}>
              Audit Trail
            </div>
            <div style={{ fontSize: 9, color: '#4A5268', marginTop: 1 }}>
              {filteredEntries.length} entries · {dateRange === 'all' ? 'All time' : `Last ${dateRange}`}
            </div>
          </div>

          {/* Export buttons */}
          <button onClick={exportCSV} style={{
            padding: '6px 12px', borderRadius: 4,
            background: 'rgba(0,229,160,0.06)',
            border: '1px solid rgba(0,229,160,0.15)',
            color: '#22C55E', fontSize: 10, fontWeight: 600,
            cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
          }}>
            ↗ CSV
          </button>
          <button onClick={exportJSON} style={{
            padding: '6px 12px', borderRadius: 4,
            background: 'rgba(0,212,255,0.06)',
            border: '1px solid rgba(0,212,255,0.15)',
            color: '#E8732A', fontSize: 10, fontWeight: 600,
            cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
          }}>
            ↗ JSON
          </button>
          {onClose && (
            <button onClick={onClose} style={{
              width: 28, height: 28, borderRadius: 4,
              background: 'rgba(255,255,255,0.03)',
              border: '1px solid rgba(255,255,255,0.06)',
              color: '#4A5268', cursor: 'pointer', fontSize: 12,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>✕</button>
          )}
        </div>

        {/* Filters */}
        <div style={{
          padding: '10px 20px',
          borderBottom: '1px solid rgba(255,255,255,0.04)',
          display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap',
        }}>
          {/* Search */}
          <input
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            placeholder="Search audit log..."
            style={{
              flex: 1, minWidth: 180, padding: '6px 10px',
              background: 'rgba(255,255,255,0.02)',
              border: '1px solid rgba(255,255,255,0.06)',
              borderRadius: 4, color: '#E4E8F0', fontSize: 11,
              fontFamily: 'inherit', outline: 'none',
            }}
          />

          {/* Date range */}
          <div style={{ display: 'flex', gap: 3 }}>
            {(['24h', '7d', '30d', 'all'] as const).map(r => (
              <button key={r} onClick={() => setDateRange(r)} style={{
                padding: '4px 8px', borderRadius: 3, fontSize: 9,
                fontWeight: 600, cursor: 'pointer',
                background: dateRange === r ? 'rgba(0,212,255,0.08)' : 'transparent',
                border: `1px solid ${dateRange === r ? 'rgba(0,212,255,0.2)' : 'rgba(255,255,255,0.04)'}`,
                color: dateRange === r ? '#E8732A' : '#4A5268',
                fontFamily: "'JetBrains Mono', monospace",
              }}>
                {r.toUpperCase()}
              </button>
            ))}
          </div>

          {/* Severity filter */}
          <select
            value={severityFilter}
            onChange={e => setSeverityFilter(e.target.value)}
            style={{
              padding: '5px 8px', borderRadius: 4, fontSize: 10,
              background: '#0E1117', border: '1px solid rgba(255,255,255,0.06)',
              color: '#8891A5', cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            <option value="all">All Severity</option>
            <option value="info">Info</option>
            <option value="warning">Warning</option>
            <option value="critical">Critical</option>
          </select>

          {/* Action filter */}
          <select
            value={actionFilter}
            onChange={e => setActionFilter(e.target.value)}
            style={{
              padding: '5px 8px', borderRadius: 4, fontSize: 10,
              background: '#0E1117', border: '1px solid rgba(255,255,255,0.06)',
              color: '#8891A5', cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            <option value="all">All Actions</option>
            {uniqueActions.map(a => <option key={a} value={a}>{a}</option>)}
          </select>
        </div>

        {/* Table */}
        <div style={{ flex: 1, overflow: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 11 }}>
            <thead>
              <tr style={{
                position: 'sticky', top: 0,
                background: '#0a0e14',
                borderBottom: '1px solid rgba(255,255,255,0.06)',
              }}>
                {['Time', 'ID', 'Action', 'User', 'Detail', 'IP', 'Severity'].map(h => (
                  <th key={h} style={{
                    padding: '8px 12px', textAlign: 'left',
                    fontSize: 8, fontWeight: 700, letterSpacing: 1.5,
                    textTransform: 'uppercase', color: '#4A5268',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filteredEntries.map((entry, idx) => {
                const sev = SEVERITY_COLORS[entry.severity];
                return (
                  <tr key={entry.id} style={{
                    borderBottom: '1px solid rgba(255,255,255,0.02)',
                    transition: 'background 0.1s',
                    animation: `portal-fadeUp 0.2s ${idx * 0.02}s ease both`,
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                  >
                    <td style={{
                      padding: '8px 12px', color: '#4A5268',
                      fontFamily: "'JetBrains Mono', monospace", fontSize: 10,
                      whiteSpace: 'nowrap',
                    }}>
                      {formatTimestamp(entry.ts)}
                    </td>
                    <td style={{
                      padding: '8px 12px', color: '#8891A5',
                      fontFamily: "'JetBrains Mono', monospace", fontSize: 10,
                    }}>
                      {entry.id}
                    </td>
                    <td style={{ padding: '8px 12px' }}>
                      <span style={{
                        padding: '2px 6px', borderRadius: 3,
                        background: sev.bg, color: sev.text,
                        fontSize: 9, fontWeight: 700,
                        fontFamily: "'JetBrains Mono', monospace",
                      }}>
                        {entry.action}
                      </span>
                    </td>
                    <td style={{ padding: '8px 12px', color: '#E4E8F0', fontWeight: 500 }}>
                      {entry.user}
                      <span style={{ color: '#4A5268', fontSize: 9, marginLeft: 4 }}>
                        ({entry.role})
                      </span>
                    </td>
                    <td style={{
                      padding: '8px 12px', color: '#8891A5',
                      maxWidth: 300, overflow: 'hidden',
                      textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                    }}>
                      {entry.detail}
                    </td>
                    <td style={{
                      padding: '8px 12px', color: '#4A5268',
                      fontFamily: "'JetBrains Mono', monospace", fontSize: 10,
                    }}>
                      {entry.ip}
                    </td>
                    <td style={{ padding: '8px 12px' }}>
                      <div style={{
                        width: 6, height: 6, borderRadius: '50%',
                        background: sev.text,
                        boxShadow: entry.severity === 'critical' ? `0 0 6px ${sev.text}` : 'none',
                      }} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {/* Footer */}
        <div style={{
          padding: '8px 20px',
          borderTop: '1px solid rgba(255,255,255,0.04)',
          display: 'flex', justifyContent: 'space-between',
          fontSize: 9, color: '#4A5268',
          fontFamily: "'JetBrains Mono', monospace",
        }}>
          <span>{filteredEntries.length} of {allEntries.length} entries shown</span>
          <span>{BRAND.name} Audit Trail · SOC2 Compliant</span>
        </div>
      </div>
    </div>
  );
}
