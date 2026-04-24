'use client';

import { useState, useEffect, useRef } from 'react';

interface Props {
  deadlineMs: number;
  acknowledged?: boolean;
}

/**
 * Live SLA countdown timer. Displays time remaining until SLA breach.
 * Turns amber at <60s, red at <15s, pulses on breach.
 */
export default function SLATimer({ deadlineMs, acknowledged }: Props) {
  const [remaining, setRemaining] = useState(deadlineMs - Date.now());
  const rafRef = useRef<number>(0);

  useEffect(() => {
    if (acknowledged) return;

    const tick = () => {
      setRemaining(deadlineMs - Date.now());
      rafRef.current = requestAnimationFrame(tick);
    };
    rafRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafRef.current);
  }, [deadlineMs, acknowledged]);

  if (acknowledged) return null;

  const breached = remaining <= 0;
  const urgent = remaining > 0 && remaining < 15000;
  const warning = remaining > 0 && remaining < 60000;

  const absMs = Math.abs(remaining);
  const mins = Math.floor(absMs / 60000);
  const secs = Math.floor((absMs % 60000) / 1000);
  const display = breached
    ? `-${mins}:${secs.toString().padStart(2, '0')}`
    : `${mins}:${secs.toString().padStart(2, '0')}`;

  const color = breached ? '#EF4444' : urgent ? '#EF4444' : warning ? '#E89B2A' : '#22C55E';

  return (
    <span
      className="sla-timer"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 3,
        fontSize: 9,
        fontWeight: 700,
        fontFamily: "'JetBrains Mono', monospace",
        color,
        padding: '1px 5px',
        borderRadius: 2,
        background: `${color}10`,
        border: `1px solid ${color}30`,
        animation: breached ? 'sla-breach-pulse 0.6s ease-in-out infinite alternate' : urgent ? 'sla-breach-pulse 1.2s ease-in-out infinite alternate' : 'none',
        whiteSpace: 'nowrap',
      }}
    >
      ⏱ {display}
    </span>
  );
}
