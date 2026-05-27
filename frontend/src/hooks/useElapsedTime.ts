'use client';

// Tiny hook extracted from ActiveAlarmView.tsx (P1-B-11). Returns the
// elapsed milliseconds since `startMs`, updated every second. Used by
// the alarm SLA timer; any other "running clock" UI can reuse this
// rather than reinventing the setInterval pattern.

import { useEffect, useState } from 'react';

export function useElapsedTime(startMs: number): number {
  const [elapsedMs, setElapsedMs] = useState(Date.now() - startMs);
  useEffect(() => {
    const timer = setInterval(() => setElapsedMs(Date.now() - startMs), 1000);
    return () => clearInterval(timer);
  }, [startMs]);
  return elapsedMs;
}
