'use client';

import { useState, useEffect, useMemo } from 'react';
import { useOperatorStore } from '@/stores/operator-store';
import { getPendingHandoffs, acceptHandoff, createHandoff, getOperatorPresence } from '@/lib/ironsight-api';
import type { ShiftHandoff, OperatorPresence } from '@/types/ironsight';

interface Props { onClose: () => void; }

export default function ShiftHandoffModal({ onClose }: Props) {
  const currentOperator = useOperatorStore(s => s.currentOperator);
  const siteLocks = useOperatorStore(s => s.siteLocks);
  const alertFeed = useOperatorStore(s => s.alertFeed);

  const [tab, setTab] = useState<'incoming' | 'create'>('incoming');
  const [pendingHandoffs, setPendingHandoffs] = useState<ShiftHandoff[]>([]);
  const [presence, setPresence] = useState<OperatorPresence[]>([]);
  const [loading, setLoading] = useState(true);

  // Create form
  const [toOperator, setToOperator] = useState('');
  const [notes, setNotes] = useState('');
  const [creating, setCreating] = useState(false);

  // Operators on shift, excluding yourself
  const availableOperators = useMemo(() =>
    presence.filter(p => p.operator_id !== currentOperator?.id && p.status === 'on_shift'),
    [presence, currentOperator]
  );

  const myLockedSites = useMemo(() =>
    Object.values(siteLocks).filter(l => l.operator_id === currentOperator?.id).map(l => l.site_id),
    [siteLocks, currentOperator]
  );

  const myActiveAlerts = useMemo(() =>
    alertFeed.filter(a => a.assigned_operator_id === currentOperator?.id && !a.acknowledged).map(a => a.id),
    [alertFeed, currentOperator]
  );

  useEffect(() => {
    if (!currentOperator) return;
    Promise.all([
      getPendingHandoffs(currentOperator.id),
      getOperatorPresence(),
    ]).then(([handoffs, pres]) => {
      setPendingHandoffs(handoffs);
      setPresence(pres);
      setLoading(false);
    });
  }, [currentOperator]);

  const handleAccept = async (handoffId: string) => {
    await acceptHandoff(handoffId);
    setPendingHandoffs(prev => prev.filter(h => h.id !== handoffId));
  };

  const handleCreate = async () => {
    if (!currentOperator || !toOperator) return;
    const target = availableOperators.find(p => p.operator_id === toOperator);
    if (!target) return;
    setCreating(true);
    await createHandoff({
      from_operator_id: currentOperator.id,
      from_operator_callsign: currentOperator.callsign,
      to_operator_id: target.operator_id,
      to_operator_callsign: target.operator_callsign,
      locked_site_ids: myLockedSites,
      active_alert_ids: myActiveAlerts,
      notes,
    });
    setCreating(false);
    onClose();
  };

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal wide" onClick={e => e.stopPropagation()} style={{ width: 580 }}>
        <div className="admin-modal-header">
          <div className="admin-modal-title">Shift Handoff</div>
          <button className="admin-modal-close" onClick={onClose}>✕</button>
        </div>

        {/* Tabs */}
        <div style={{ display: 'flex', borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
          {(['incoming', 'create'] as const).map(t => (
            <button key={t} onClick={() => setTab(t)} style={{
              flex: 1, padding: '10px', fontSize: 11, fontWeight: 600, letterSpacing: 1,
              textTransform: 'uppercase', cursor: 'pointer', border: 'none', fontFamily: 'inherit',
              background: tab === t ? 'rgba(139,92,246,0.08)' : 'transparent',
              color: tab === t ? '#a78bfa' : '#4A5268',
              borderBottom: tab === t ? '2px solid #a78bfa' : '2px solid transparent',
            }}>
              {t === 'incoming' ? `Incoming (${pendingHandoffs.length})` : 'End Shift'}
            </button>
          ))}
        </div>

        <div className="admin-modal-body">
          {tab === 'incoming' && (
            <>
              {loading && <div style={{ textAlign: 'center', color: '#4A5268', padding: 20 }}>Loading…</div>}
              {!loading && pendingHandoffs.length === 0 && (
                <div style={{ textAlign: 'center', color: '#4A5268', padding: 30, fontSize: 12 }}>No pending handoffs</div>
              )}
              {pendingHandoffs.map(h => (
                <div key={h.id} style={{ padding: 14, border: '1px solid rgba(139,92,246,0.15)', borderRadius: 6, marginBottom: 10, background: 'rgba(139,92,246,0.03)' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                    <div>
                      <span style={{ fontWeight: 700, color: '#a78bfa' }}>{h.from_operator_callsign}</span>
                      <span style={{ color: '#4A5268' }}> → </span>
                      <span style={{ fontWeight: 700, color: '#22C55E' }}>{h.to_operator_callsign}</span>
                    </div>
                    <span style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
                      {new Date(h.created_at).toLocaleTimeString()}
                    </span>
                  </div>
                  <div style={{ fontSize: 11, color: '#8891A5', marginBottom: 6 }}>
                    📍 {h.locked_site_ids.length} sites · ⚡ {h.active_alert_ids.length} active alerts
                  </div>
                  {h.notes && (
                    <div style={{ fontSize: 11, color: '#E4E8F0', padding: 8, background: 'rgba(0,0,0,0.2)', borderRadius: 4, marginBottom: 8, lineHeight: 1.5, fontStyle: 'italic' }}>
                      &ldquo;{h.notes}&rdquo;
                    </div>
                  )}
                  <button className="admin-btn admin-btn-primary" style={{ fontSize: 10, padding: '4px 12px' }} onClick={() => handleAccept(h.id)}>
                    ✓ Accept Handoff
                  </button>
                </div>
              ))}
            </>
          )}

          {tab === 'create' && (
            <div>
              <div style={{ padding: 12, background: 'rgba(255,204,0,0.06)', border: '1px solid rgba(255,204,0,0.15)', borderRadius: 4, marginBottom: 14, fontSize: 11, color: '#E89B2A' }}>
                ⚠ This will transfer your {myLockedSites.length} locked site{myLockedSites.length !== 1 ? 's' : ''} and {myActiveAlerts.length} active alert{myActiveAlerts.length !== 1 ? 's' : ''} to the receiving operator.
              </div>
              <div className="admin-field">
                <label className="admin-label">Hand off to</label>
                <select className="admin-input" value={toOperator} onChange={e => setToOperator(e.target.value)} style={{ cursor: 'pointer' }}>
                  <option value="">Select operator…</option>
                  {availableOperators.map(op => (
                    <option key={op.operator_id} value={op.operator_id}>{op.operator_callsign}</option>
                  ))}
                </select>
              </div>
              <div className="admin-field">
                <label className="admin-label">Handoff Notes</label>
                <textarea className="admin-input" rows={5} placeholder="Describe active situations, pending actions, and anything the incoming operator needs to know…" value={notes} onChange={e => setNotes(e.target.value)} style={{ resize: 'vertical', fontFamily: 'inherit' }} />
              </div>
            </div>
          )}
        </div>

        {tab === 'create' && (
          <div className="admin-modal-footer">
            <button className="admin-btn admin-btn-ghost" onClick={onClose}>Cancel</button>
            <button className="admin-btn admin-btn-primary" onClick={handleCreate} disabled={!toOperator || creating}>
              {creating ? 'Sending…' : '📋 Send Handoff'}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
