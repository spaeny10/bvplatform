'use client';

// Audit Trail export modal.
//
// F-03 (2026-06-09 review): this modal previously generated 60 random
// synthetic entries client-side (fake users, fake IPs) and exported
// THOSE — while labeling the footer "SOC2 Compliant". It now loads
// real rows from GET /api/audit (lib/api.ts queryAuditLog) and the
// CSV/JSON exports contain exactly what the table shows. The fake
// "severity" concept (the backend has none) is gone.

import { useState, useMemo, useCallback, useEffect } from 'react';
import { BRAND } from '@/lib/branding';
import { queryAuditLog, type AuditEntry } from '@/lib/api';

// Slug used for downloaded CSV/JSON filenames. `BRAND.shortName` lowercased
// with spaces → hyphens, so "Sky Watch" becomes "sky-watch-audit-log...csv".
const DOWNLOAD_SLUG = BRAND.shortName.toLowerCase().replace(/\s+/g, '-');

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString('en-US', {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  });
}

interface Props {
  onClose?: () => void;
}

export default function AuditLogExport({ onClose }: Props) {
  const [allEntries, setAllEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [dateRange, setDateRange] = useState<'24h' | '7d' | '30d' | 'all'>('7d');
  const [actionFilter, setActionFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');

  useEffect(() => {
    let alive = true;
    queryAuditLog({ limit: 1000 })
      .then(res => {
        if (!alive) return;
        setAllEntries(res.entries ?? []);
        setError(null);
      })
      .catch(err => {
        if (!alive) return;
        setError(err instanceof Error ? err.message : 'Failed to load audit log');
      })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, []);

  const filteredEntries = useMemo(() => {
    let entries = allEntries;

    // Date range (client-side on created_at; server returns newest-first)
    const now = Date.now();
    const cutoff = dateRange === '24h' ? 86400000
      : dateRange === '7d' ? 7 * 86400000
      : dateRange === '30d' ? 30 * 86400000
      : Infinity;
    if (cutoff !== Infinity) {
      entries = entries.filter(e => now - new Date(e.created_at).getTime() < cutoff);
    }

    if (actionFilter !== 'all') entries = entries.filter(e => e.action === actionFilter);

    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      entries = entries.filter(e =>
        (e.details ?? '').toLowerCase().includes(q) ||
        (e.username ?? '').toLowerCase().includes(q) ||
        e.action.toLowerCase().includes(q) ||
        (e.target_type ?? '').toLowerCase().includes(q) ||
        (e.target_id ?? '').toLowerCase().includes(q)
      );
    }

    return entries;
  }, [allEntries, dateRange, actionFilter, searchQuery]);

  const uniqueActions = useMemo(() => Array.from(new Set(allEntries.map(e => e.action))).sort(), [allEntries]);

  const exportCSV = useCallback(() => {
    const headers = ['Timestamp', 'ID', 'Action', 'User', 'TargetType', 'TargetID', 'Details', 'IP'];
    const esc = (v: string) => `"${(v ?? '').replace(/"/g, '""')}"`;
    const rows = filteredEntries.map(e => [
      new Date(e.created_at).toISOString(), String(e.id), e.action,
      esc(e.username ?? e.user_id), e.target_type, esc(e.target_id), esc(e.details), e.ip_address,
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
      filters: { dateRange, actionFilter, searchQuery },
      entries: filteredEntries,
    }, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${DOWNLOAD_SLUG}-audit-log-${new Date().toISOString().split('T')[0]}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }, [filteredEntries, dateRange, actionFilter, searchQuery]);

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
          <button onClick={exportCSV} disabled={loading || !!error} style={{
            padding: '6px 12px', borderRadius: 4,
            background: 'rgba(0,229,160,0.06)',
            border: '1px solid rgba(0,229,160,0.15)',
            color: '#22C55E', fontSize: 10, fontWeight: 600,
            cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
          }}>
            ↗ CSV
          </button>
          <button onClick={exportJSON} disabled={loading || !!error} style={{
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
          {loading && <div style={{ padding: 40, textAlign: 'center', color: '#4A5268' }}>Loading…</div>}
          {error && <div style={{ padding: 40, textAlign: 'center', color: '#EF4444', fontSize: 11 }}>⚠ {error}</div>}
          {!loading && !error && (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 11 }}>
            <thead>
              <tr style={{
                position: 'sticky', top: 0,
                background: '#0a0e14',
                borderBottom: '1px solid rgba(255,255,255,0.06)',
              }}>
                {['Time', 'ID', 'Action', 'User', 'Target', 'Details', 'IP'].map(h => (
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
              {filteredEntries.map((entry, idx) => (
                <tr key={entry.id} style={{
                  borderBottom: '1px solid rgba(255,255,255,0.02)',
                  transition: 'background 0.1s',
                  animation: `portal-fadeUp 0.2s ${Math.min(idx, 25) * 0.02}s ease both`,
                }}
                onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                >
                  <td style={{
                    padding: '8px 12px', color: '#4A5268',
                    fontFamily: "'JetBrains Mono', monospace", fontSize: 10,
                    whiteSpace: 'nowrap',
                  }}>
                    {formatTimestamp(entry.created_at)}
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
                      background: 'rgba(0,212,255,0.06)', color: '#E8732A',
                      fontSize: 9, fontWeight: 700,
                      fontFamily: "'JetBrains Mono', monospace",
                    }}>
                      {entry.action}
                    </span>
                  </td>
                  <td style={{ padding: '8px 12px', color: '#E4E8F0', fontWeight: 500 }}>
                    {entry.username || entry.user_id || 'system'}
                  </td>
                  <td style={{
                    padding: '8px 12px', color: '#8891A5',
                    fontFamily: "'JetBrains Mono', monospace", fontSize: 10,
                    maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                  }}>
                    {entry.target_type}{entry.target_id ? `:${entry.target_id}` : ''}
                  </td>
                  <td style={{
                    padding: '8px 12px', color: '#8891A5',
                    maxWidth: 300, overflow: 'hidden',
                    textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                  }}>
                    {entry.details}
                  </td>
                  <td style={{
                    padding: '8px 12px', color: '#4A5268',
                    fontFamily: "'JetBrains Mono', monospace", fontSize: 10,
                  }}>
                    {entry.ip_address}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          )}
        </div>

        {/* Footer */}
        <div style={{
          padding: '8px 20px',
          borderTop: '1px solid rgba(255,255,255,0.04)',
          display: 'flex', justifyContent: 'space-between',
          fontSize: 9, color: '#4A5268',
          fontFamily: "'JetBrains Mono', monospace",
        }}>
          <span>{filteredEntries.length} of {allEntries.length} loaded entries shown</span>
          <span>{BRAND.name} Audit Trail · append-only server log</span>
        </div>
      </div>
    </div>
  );
}
