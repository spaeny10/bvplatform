'use client';

import { useState, useEffect } from 'react';
import { useOperatorStore } from '@/stores/operator-store';

interface FleetStatusBarProps {
  onlineCameras: number;
  degradedCameras: number;
  criticalAlerts: number;
  slaBreaches?: number;
  queueDepth?: number;
}

export default function FleetStatusBar({
  onlineCameras,
  degradedCameras,
  criticalAlerts,
  slaBreaches = 0,
  queueDepth = 0,
}: FleetStatusBarProps) {
  const [time, setTime] = useState('');
  const currentOperator = useOperatorStore((s) => s.currentOperator);
  const siteLocks = useOperatorStore((s) => s.siteLocks);

  const myLockedCount = Object.values(siteLocks).filter(
    (l) => l.operator_id === currentOperator?.id
  ).length;

  useEffect(() => {
    const update = () => setTime(new Date().toLocaleTimeString('en-US', { hour12: false }));
    update();
    const t = setInterval(update, 1000);
    return () => clearInterval(t);
  }, []);

  return (
    <>
      <div className="op-status-cluster">
        <div className="op-status-pill online">
          <span className="op-dot" /> {onlineCameras} ONLINE
        </div>
        {degradedCameras > 0 && (
          <div className="op-status-pill degraded">
            <span className="op-dot blink" /> {degradedCameras} DEGRADED
          </div>
        )}
        {criticalAlerts > 0 && (
          <div className="op-status-pill critical">
            <span className="op-dot blink" /> {criticalAlerts} CRITICAL
          </div>
        )}
      </div>

      {queueDepth > 0 && (
        <div className="op-status-pill" style={{
          background: queueDepth >= 3 ? 'rgba(255,51,85,0.1)' : 'rgba(255,204,0,0.1)',
          color: queueDepth >= 3 ? '#EF4444' : '#E89B2A',
          border: `1px solid ${queueDepth >= 3 ? 'rgba(255,51,85,0.25)' : 'rgba(255,204,0,0.25)'}`,
          animation: queueDepth >= 3 ? 'op-alert-blink 0.8s infinite' : undefined,
        }}>
          📋 {queueDepth} IN QUEUE
        </div>
      )}

      {slaBreaches > 0 && (
        <div className="op-status-pill critical" style={{ animation: 'op-alert-blink 0.8s infinite' }}>
          ⏱ {slaBreaches} SLA BREACH
        </div>
      )}

      {myLockedCount > 0 && (
        <div className="op-locked-count">
          🔒 {myLockedCount} LOCKED
        </div>
      )}

      <div className="op-time-display">{time}</div>

      <div className="op-operator-badge">
        <div className="op-avatar">
          {currentOperator ? currentOperator.callsign.slice(-1) : '?'}
        </div>
        <div>
          <div style={{ fontSize: 11, fontWeight: 600 }}>
            {currentOperator?.callsign || 'OFFLINE'}
          </div>
          <div style={{ fontSize: 8, color: 'var(--sg-text-dim)', fontFamily: "'JetBrains Mono', monospace" }}>
            {currentOperator?.name || '—'}
          </div>
        </div>
      </div>
    </>
  );
}
