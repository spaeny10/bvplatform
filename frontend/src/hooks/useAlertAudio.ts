'use client';

import { useEffect, useRef } from 'react';
import type { AlertEvent } from '@/types/ironsight';

// Web Audio API tone generator — no external files needed
function playTone(type: 'critical' | 'high' | 'sla_breach') {
  try {
    const AudioCtx = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
    const ctx = new AudioCtx();

    const sequences: { freq: number; start: number; dur: number }[] =
      type === 'critical'
        ? [
            { freq: 1047, start: 0,    dur: 0.12 },
            { freq: 1047, start: 0.16, dur: 0.12 },
            { freq: 1319, start: 0.32, dur: 0.22 },
          ]
        : type === 'high'
        ? [
            { freq: 880, start: 0,    dur: 0.15 },
            { freq: 880, start: 0.22, dur: 0.15 },
          ]
        : [
            // SLA breach: urgent descending
            { freq: 1174, start: 0,    dur: 0.1 },
            { freq: 988,  start: 0.12, dur: 0.1 },
            { freq: 784,  start: 0.24, dur: 0.1 },
            { freq: 1174, start: 0.38, dur: 0.1 },
            { freq: 988,  start: 0.5,  dur: 0.1 },
            { freq: 784,  start: 0.62, dur: 0.18 },
          ];

    sequences.forEach(({ freq, start, dur }) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.type = 'square';
      osc.frequency.value = freq;
      gain.gain.setValueAtTime(0, ctx.currentTime + start);
      gain.gain.linearRampToValueAtTime(0.07, ctx.currentTime + start + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + start + dur);
      osc.start(ctx.currentTime + start);
      osc.stop(ctx.currentTime + start + dur + 0.01);
    });

    // Auto-close context after sounds finish
    setTimeout(() => ctx.close(), 2000);
  } catch {
    // AudioContext not supported or blocked — silent fail
  }
}

interface UseAlertAudioOptions {
  muted: boolean;
}

export function useAlertAudio(alerts: AlertEvent[], { muted }: UseAlertAudioOptions) {
  const prevCriticalRef = useRef(0);
  const prevHighRef = useRef(0);
  const prevSlaRef = useRef(0);
  const initializedRef = useRef(false);

  useEffect(() => {
    const criticalUnacked = alerts.filter(a => !a.acknowledged && a.severity === 'critical').length;
    const highUnacked = alerts.filter(a => !a.acknowledged && a.severity === 'high').length;
    const slaBreached = alerts.filter(a =>
      !a.acknowledged && a.sla_deadline_ms && Date.now() > a.sla_deadline_ms
    ).length;

    // Skip audio on first mount (don't replay existing alerts)
    if (!initializedRef.current) {
      prevCriticalRef.current = criticalUnacked;
      prevHighRef.current = highUnacked;
      prevSlaRef.current = slaBreached;
      initializedRef.current = true;
      return;
    }

    if (muted) return;

    if (criticalUnacked > prevCriticalRef.current) {
      playTone('critical');
    } else if (highUnacked > prevHighRef.current) {
      playTone('high');
    } else if (slaBreached > prevSlaRef.current) {
      playTone('sla_breach');
    }

    prevCriticalRef.current = criticalUnacked;
    prevHighRef.current = highUnacked;
    prevSlaRef.current = slaBreached;
  }, [alerts, muted]);
}
