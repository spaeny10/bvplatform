'use client';

import { useEffect, useRef, useState, useCallback, createContext, useContext } from 'react';
import type { AlertEvent } from '@/types/ironsight';

// ── Toast Item ────────────────────────────────────────────────

interface Toast {
  id: string;
  alert: AlertEvent;
  createdAt: number;
  exiting: boolean;
}

const SEVERITY_CONFIG: Record<string, { color: string; glow: string; icon: string; sound: boolean }> = {
  critical: { color: '#EF4444', glow: 'rgba(255,51,85,0.25)', icon: '🚨', sound: true },
  high: { color: '#EF4444', glow: 'rgba(255,107,53,0.2)', icon: '⚠️', sound: true },
  medium: { color: '#E89B2A', glow: 'rgba(255,204,0,0.15)', icon: '⚡', sound: false },
  low: { color: '#E8732A', glow: 'rgba(0,212,255,0.1)', icon: 'ℹ️', sound: false },
};

const TOAST_DURATION = 6000;
const MAX_TOASTS = 4;

// ── Audio Helper ──────────────────────────────────────────────

function playAlertChime(severity: string) {
  if (typeof window === 'undefined') return;
  try {
    const ctx = new AudioContext();
    const oscillator = ctx.createOscillator();
    const gain = ctx.createGain();

    oscillator.connect(gain);
    gain.connect(ctx.destination);

    if (severity === 'critical') {
      // Urgent double-beep
      oscillator.type = 'square';
      oscillator.frequency.setValueAtTime(880, ctx.currentTime);
      oscillator.frequency.setValueAtTime(0, ctx.currentTime + 0.1);
      oscillator.frequency.setValueAtTime(880, ctx.currentTime + 0.2);
      gain.gain.setValueAtTime(0.15, ctx.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.4);
      oscillator.start(ctx.currentTime);
      oscillator.stop(ctx.currentTime + 0.4);
    } else {
      // Single soft tone
      oscillator.type = 'sine';
      oscillator.frequency.setValueAtTime(660, ctx.currentTime);
      gain.gain.setValueAtTime(0.1, ctx.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.25);
      oscillator.start(ctx.currentTime);
      oscillator.stop(ctx.currentTime + 0.25);
    }
  } catch {
    // Audio not available — silent fallback
  }
}

// ── Browser Notification ──────────────────────────────────────

function sendBrowserNotification(alert: AlertEvent) {
  if (typeof window === 'undefined' || !('Notification' in window)) return;
  if (document.hasFocus()) return; // Only if tab is not focused

  if (Notification.permission === 'granted') {
    new Notification(`${alert.severity.toUpperCase()} — ${alert.site_name}`, {
      body: alert.description,
      icon: '/icon-192.png',
      tag: alert.id,
      requireInteraction: alert.severity === 'critical',
    });
  } else if (Notification.permission !== 'denied') {
    Notification.requestPermission();
  }
}

// ── Toast Provider ────────────────────────────────────────────

interface ToastContextValue {
  pushToast: (alert: AlertEvent) => void;
}

const ToastContext = createContext<ToastContextValue>({ pushToast: () => {} });

export function useAlertToast() {
  return useContext(ToastContext);
}

export function AlertToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const pushToast = useCallback((alert: AlertEvent) => {
    const config = SEVERITY_CONFIG[alert.severity];

    // Audio chime for critical/high
    if (config?.sound) {
      playAlertChime(alert.severity);
    }

    // Browser notification if tab not focused
    if (alert.severity === 'critical' || alert.severity === 'high') {
      sendBrowserNotification(alert);
    }

    const toast: Toast = {
      id: `toast-${alert.id}-${Date.now()}`,
      alert,
      createdAt: Date.now(),
      exiting: false,
    };

    setToasts(prev => [toast, ...prev].slice(0, MAX_TOASTS));

    // Auto-dismiss
    setTimeout(() => {
      setToasts(prev => prev.map(t => t.id === toast.id ? { ...t, exiting: true } : t));
      setTimeout(() => {
        setToasts(prev => prev.filter(t => t.id !== toast.id));
      }, 300);
    }, TOAST_DURATION);
  }, []);

  const dismissToast = useCallback((id: string) => {
    setToasts(prev => prev.map(t => t.id === id ? { ...t, exiting: true } : t));
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id));
    }, 300);
  }, []);

  return (
    <ToastContext.Provider value={{ pushToast }}>
      {children}

      {/* Toast Container */}
      {toasts.length > 0 && (
        <div style={{
          position: 'fixed', top: 60, right: 16, zIndex: 9990,
          display: 'flex', flexDirection: 'column', gap: 8,
          pointerEvents: 'none', width: 360,
        }}>
          {toasts.map((toast, i) => {
            const config = SEVERITY_CONFIG[toast.alert.severity] || SEVERITY_CONFIG.low;
            return (
              <div
                key={toast.id}
                style={{
                  pointerEvents: 'auto',
                  background: '#0E1117',
                  border: `1px solid ${config.color}40`,
                  borderLeft: `3px solid ${config.color}`,
                  borderRadius: 6,
                  padding: '10px 14px',
                  boxShadow: `0 8px 32px rgba(0,0,0,0.6), 0 0 20px ${config.glow}`,
                  backdropFilter: 'blur(12px)',
                  cursor: 'pointer',
                  transform: toast.exiting
                    ? 'translateX(120%)'
                    : `translateY(${i === 0 ? '0' : '0'})`,
                  opacity: toast.exiting ? 0 : 1,
                  transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
                  animation: toast.exiting ? 'none' : 'toast-slide-in 0.3s ease-out',
                }}
                onClick={() => dismissToast(toast.id)}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                  <span style={{ fontSize: 14 }}>{config.icon}</span>
                  <span style={{
                    fontSize: 9, fontWeight: 700, letterSpacing: 1.2,
                    textTransform: 'uppercase' as const,
                    color: config.color,
                  }}>{toast.alert.severity}</span>
                  <span style={{ fontSize: 10, color: '#8891A5', flex: 1, textAlign: 'right' }}>
                    {toast.alert.site_name}
                  </span>
                  <button
                    onClick={e => { e.stopPropagation(); dismissToast(toast.id); }}
                    style={{
                      background: 'none', border: 'none', color: '#4A5268',
                      fontSize: 12, cursor: 'pointer', padding: 0, lineHeight: 1,
                    }}
                  >✕</button>
                </div>
                <div style={{
                  fontSize: 11, color: '#E4E8F0', lineHeight: 1.4,
                  overflow: 'hidden', textOverflow: 'ellipsis',
                  display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' as const,
                }}>
                  {toast.alert.description}
                </div>
                <div style={{
                  fontSize: 9, color: '#4A5268', marginTop: 4,
                  fontFamily: "'JetBrains Mono', monospace",
                }}>
                  📷 {toast.alert.camera_name}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </ToastContext.Provider>
  );
}
