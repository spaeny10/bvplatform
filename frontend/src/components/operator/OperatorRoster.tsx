'use client';

import { useState, useEffect, useMemo } from 'react';
import { getOperatorPresence } from '@/lib/ironsight-api';
import { useOperatorStore } from '@/stores/operator-store';
import type { OperatorPresence } from '@/types/ironsight';

export default function OperatorRoster() {
  const [presence, setPresence] = useState<OperatorPresence[]>([]);
  const currentOperator = useOperatorStore(s => s.currentOperator);
  const operatorStatus = useOperatorStore(s => s.operatorStatus);

  useEffect(() => {
    getOperatorPresence().then(setPresence);
    const t = setInterval(() => getOperatorPresence().then(setPresence), 10000);
    return () => clearInterval(t);
  }, []);

  // Merge own live status immediately — don't wait for next poll
  const mergedPresence = useMemo(() => {
    const ownPresenceStatus: OperatorPresence['status'] =
      operatorStatus === 'break' ? 'break'
      : operatorStatus === 'away' ? 'off_shift'
      : 'on_shift';
    return presence.map(p =>
      p.operator_id === currentOperator?.id ? { ...p, status: ownPresenceStatus } : p
    );
  }, [presence, currentOperator?.id, operatorStatus]);

  const statusColors: Record<string, string> = {
    on_shift: '#22C55E', break: '#E89B2A', off_shift: '#4A5268',
  };

  const statusLabels: Record<string, string> = {
    on_shift: 'on shift', break: 'break', off_shift: 'off shift',
  };

  return (
    <div style={{ padding: '8px 12px' }}>
      <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#4A5268', marginBottom: 6 }}>
        SOC Operators ({mergedPresence.filter(p => p.status === 'on_shift').length} on shift)
      </div>
      {mergedPresence.map(op => {
        const isMe = op.operator_id === currentOperator?.id;
        return (
          <div key={op.operator_id} style={{
            display: 'flex', alignItems: 'center', gap: 8, padding: '5px 0',
            borderBottom: '1px solid rgba(255,255,255,0.03)',
          }}>
            <div style={{
              width: 6, height: 6, borderRadius: '50%',
              background: statusColors[op.status] || '#4A5268',
              boxShadow: op.status === 'on_shift' ? `0 0 6px ${statusColors[op.status]}` : 'none',
            }} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 11, fontWeight: isMe ? 700 : 500, color: isMe ? '#E8732A' : '#E4E8F0' }}>
                {op.operator_callsign} {isMe && '(you)'}
              </div>
              {op.viewing_site_id && (
                <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
                  👁 {op.viewing_site_id}{op.viewing_camera_id ? ` · ${op.viewing_camera_id}` : ''}
                </div>
              )}
            </div>
            <span style={{
              fontSize: 8, padding: '1px 5px', borderRadius: 2, fontWeight: 600,
              textTransform: 'uppercase' as const, letterSpacing: 0.5,
              color: statusColors[op.status],
              border: `1px solid ${statusColors[op.status]}30`,
            }}>
              {statusLabels[op.status] ?? op.status.replace('_', ' ')}
            </span>
          </div>
        );
      })}
    </div>
  );
}
