'use client';

import { useState, useEffect, useMemo } from 'react';
import { getAuditLog } from '@/lib/ironsight-api';
import type { AuditEntry, AuditAction } from '@/types/ironsight';

const ACTION_LABELS: Record<AuditAction, { icon: string; label: string; color: string }> = {
  alert_claimed: { icon: '⚡', label: 'Alert Claimed', color: '#E8732A' },
  alert_released: { icon: '↩', label: 'Alert Released', color: '#4A5268' },
  alert_acknowledged: { icon: '✓', label: 'Alert Acknowledged', color: '#22C55E' },
  alert_escalated: { icon: '🔺', label: 'Alert Escalated', color: '#EF4444' },
  site_locked: { icon: '🔒', label: 'Site Locked', color: '#22C55E' },
  site_unlocked: { icon: '🔓', label: 'Site Unlocked', color: '#4A5268' },
  shift_handoff_created: { icon: '📋', label: 'Shift Handoff', color: '#a855f7' },
  shift_handoff_accepted: { icon: '✓', label: 'Handoff Accepted', color: '#22C55E' },
  sop_viewed: { icon: '📖', label: 'SOP Viewed', color: '#8891A5' },
  incident_created: { icon: '🚨', label: 'Incident Created', color: '#EF4444' },
  incident_updated: { icon: '📝', label: 'Incident Updated', color: '#E89B2A' },
  evidence_exported: { icon: '📦', label: 'Evidence Exported', color: '#a855f7' },
  ptz_command: { icon: '🎮', label: 'PTZ Command', color: '#E8732A' },
  zone_edited: { icon: '🗺️', label: 'Zone Edited', color: '#EF4444' },
  login: { icon: '🔑', label: 'Login', color: '#22C55E' },
  logout: { icon: '🚪', label: 'Logout', color: '#4A5268' },
};

export default function AuditLogPanel() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [filterOperator, setFilterOperator] = useState('');
  const [filterAction, setFilterAction] = useState('');

  useEffect(() => {
    getAuditLog().then(data => { setEntries(data); setLoading(false); });
  }, []);

  const filtered = useMemo(() => {
    let result = entries;
    if (filterOperator) result = result.filter(e => e.operator_callsign === filterOperator);
    if (filterAction) result = result.filter(e => e.action === filterAction);
    return result;
  }, [entries, filterOperator, filterAction]);

  const operators = useMemo(() => Array.from(new Set(entries.map(e => e.operator_callsign))), [entries]);
  const actions = useMemo(() => Array.from(new Set(entries.map(e => e.action))), [entries]);

  return (
    <div className="admin-card" style={{ marginTop: 16 }}>
      <div className="admin-card-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 16 }}>📜</span>
          <div>
            <div className="admin-card-title">Audit Trail</div>
            <div style={{ fontSize: 10, color: '#4A5268', marginTop: 1 }}>{entries.length} entries</div>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 6 }}>
          <select className="admin-input" value={filterOperator} onChange={e => setFilterOperator(e.target.value)} style={{ width: 100, fontSize: 10, padding: '4px 8px', cursor: 'pointer' }}>
            <option value="">All operators</option>
            {operators.map(o => <option key={o} value={o}>{o}</option>)}
          </select>
          <select className="admin-input" value={filterAction} onChange={e => setFilterAction(e.target.value)} style={{ width: 130, fontSize: 10, padding: '4px 8px', cursor: 'pointer' }}>
            <option value="">All actions</option>
            {actions.map(a => <option key={a} value={a}>{ACTION_LABELS[a as AuditAction]?.label || a}</option>)}
          </select>
        </div>
      </div>
      <div style={{ maxHeight: 320, overflowY: 'auto', scrollbarWidth: 'thin' as const }}>
        {loading && <div style={{ padding: 30, textAlign: 'center', color: '#4A5268' }}>Loading…</div>}
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
                  <span style={{ fontWeight: 700, color: '#E8732A' }}>{entry.operator_callsign}</span>
                  {' '}<span style={{ color: info.color }}>{info.label}</span>
                  {' '}<span style={{ color: '#4A5268' }}>on</span>
                  {' '}<span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 10, color: '#8891A5' }}>{entry.entity_id}</span>
                </div>
                {entry.metadata && (
                  <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
                    {Object.entries(entry.metadata).map(([k, v]) => `${k}=${v}`).join(' · ')}
                  </div>
                )}
              </div>
              <span style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", flexShrink: 0 }}>
                {new Date(entry.ts).toLocaleTimeString('en-US', { hour12: false })}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
