'use client';

// Admin Audit Trail panel.
//
// F-03 (2026-06-09 review): this panel previously called
// GET /api/v1/audit — a route that does not exist — through a
// .then() with no .catch(), so the ungated Audit tab sat on
// "Loading…" forever. It now uses the real, registered endpoint
// (GET /api/audit via lib/api.ts queryAuditLog) and renders the
// backend's actual envelope: {entries,total,limit,offset} with
// username/target_type/created_at fields. This is the only UI path
// to real audit data, so it gets an explicit error state instead of
// a silent spinner.

import { useState, useEffect, useMemo } from 'react';
import { queryAuditLog, type AuditEntry } from '@/lib/api';

// Server-side action names (audit_log.action) → display metadata.
// Unknown actions fall back to a neutral bullet so new backend
// actions are never invisible here.
const ACTION_LABELS: Record<string, { icon: string; label: string; color: string }> = {
  login: { icon: '🔑', label: 'Login', color: '#22C55E' },
  login_failed: { icon: '⛔', label: 'Login Failed', color: '#EF4444' },
  logout: { icon: '🚪', label: 'Logout', color: '#4A5268' },
  create: { icon: '➕', label: 'Created', color: '#22C55E' },
  update: { icon: '📝', label: 'Updated', color: '#E89B2A' },
  delete: { icon: '🗑️', label: 'Deleted', color: '#EF4444' },
  evidence_export: { icon: '📦', label: 'Evidence Exported', color: '#a855f7' },
  revoke_evidence_share: { icon: '🚫', label: 'Share Revoked', color: '#EF4444' },
  deterrence: { icon: '🚨', label: 'Deterrence Fired', color: '#E8732A' },
  media_access: { icon: '🎞️', label: 'Media Accessed', color: '#8891A5' },
  reboot: { icon: '🔄', label: 'Camera Reboot', color: '#E89B2A' },
  mfa_reset: { icon: '🔓', label: 'MFA Reset', color: '#EF4444' },
};

export default function AuditLogPanel() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filterUser, setFilterUser] = useState('');
  const [filterAction, setFilterAction] = useState('');

  useEffect(() => {
    let alive = true;
    setLoading(true);
    queryAuditLog({ limit: 200 })
      .then(res => {
        if (!alive) return;
        setEntries(res.entries ?? []);
        setTotal(res.total ?? (res.entries?.length ?? 0));
        setError(null);
      })
      .catch(err => {
        if (!alive) return;
        setError(err instanceof Error ? err.message : 'Failed to load audit log');
      })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, []);

  const filtered = useMemo(() => {
    let result = entries;
    if (filterUser) result = result.filter(e => e.username === filterUser);
    if (filterAction) result = result.filter(e => e.action === filterAction);
    return result;
  }, [entries, filterUser, filterAction]);

  const users = useMemo(() => Array.from(new Set(entries.map(e => e.username).filter(Boolean))), [entries]);
  const actions = useMemo(() => Array.from(new Set(entries.map(e => e.action))), [entries]);

  return (
    <div className="admin-card" style={{ marginTop: 16 }}>
      <div className="admin-card-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 16 }}>📜</span>
          <div>
            <div className="admin-card-title">Audit Trail</div>
            <div style={{ fontSize: 10, color: '#4A5268', marginTop: 1 }}>
              {entries.length} of {total} entries
            </div>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 6 }}>
          <select className="admin-input" value={filterUser} onChange={e => setFilterUser(e.target.value)} style={{ width: 110, fontSize: 10, padding: '4px 8px', cursor: 'pointer' }}>
            <option value="">All users</option>
            {users.map(u => <option key={u} value={u}>{u}</option>)}
          </select>
          <select className="admin-input" value={filterAction} onChange={e => setFilterAction(e.target.value)} style={{ width: 130, fontSize: 10, padding: '4px 8px', cursor: 'pointer' }}>
            <option value="">All actions</option>
            {actions.map(a => <option key={a} value={a}>{ACTION_LABELS[a]?.label || a}</option>)}
          </select>
        </div>
      </div>
      <div style={{ maxHeight: 320, overflowY: 'auto', scrollbarWidth: 'thin' as const }}>
        {loading && <div style={{ padding: 30, textAlign: 'center', color: '#4A5268' }}>Loading…</div>}
        {error && (
          <div style={{ padding: 30, textAlign: 'center', color: '#EF4444', fontSize: 11 }}>
            ⚠ {error}
          </div>
        )}
        {!loading && !error && filtered.length === 0 && (
          <div style={{ padding: 30, textAlign: 'center', color: '#4A5268', fontSize: 11 }}>
            No audit entries match.
          </div>
        )}
        {filtered.map(entry => {
          const info = ACTION_LABELS[entry.action] || { icon: '•', label: entry.action, color: '#4A5268' };
          return (
            <div key={entry.id} style={{
              display: 'flex', alignItems: 'center', gap: 10, padding: '8px 16px',
              borderBottom: '1px solid rgba(255,255,255,0.03)',
            }}>
              <span style={{ fontSize: 14, width: 20, textAlign: 'center' }}>{info.icon}</span>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: 11, color: '#E4E8F0' }}>
                  <span style={{ fontWeight: 700, color: '#E8732A' }}>{entry.username || entry.user_id || 'system'}</span>
                  {' '}<span style={{ color: info.color }}>{info.label}</span>
                  {' '}<span style={{ color: '#4A5268' }}>{entry.target_type}</span>
                  {' '}<span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 10, color: '#8891A5' }}>{entry.target_id}</span>
                </div>
                {entry.details && (
                  <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {entry.details}
                  </div>
                )}
              </div>
              <span style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", flexShrink: 0 }} title={entry.ip_address}>
                {new Date(entry.created_at).toLocaleString('en-US', { hour12: false })}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
