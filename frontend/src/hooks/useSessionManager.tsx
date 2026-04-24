'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import { useAuth } from '@/contexts/AuthContext';

const SESSION_WARNING_MS = 5 * 60 * 1000;     // Show warning 5 min before expiry
const HEARTBEAT_INTERVAL_MS = 60 * 1000;       // Check every 60s
const IDLE_TIMEOUT_MS = 30 * 60 * 1000;        // 30 min idle → timeout warning
const ACTIVITY_DEBOUNCE_MS = 5000;             // Debounce activity events

export function useSessionManager() {
  const { user, logout } = useAuth();
  const [showWarning, setShowWarning] = useState(false);
  const [timeRemaining, setTimeRemaining] = useState(0);
  const lastActivityRef = useRef(Date.now());
  const sessionStartRef = useRef(Date.now());
  const warningDismissedRef = useRef(false);

  // Track user activity
  const recordActivity = useCallback(() => {
    lastActivityRef.current = Date.now();
  }, []);

  useEffect(() => {
    if (!user) return;
    sessionStartRef.current = Date.now();

    const events = ['mousedown', 'keydown', 'scroll', 'touchstart'];
    let debounceTimer: NodeJS.Timeout;

    const handler = () => {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(recordActivity, ACTIVITY_DEBOUNCE_MS);
    };

    events.forEach(e => window.addEventListener(e, handler, { passive: true }));

    // Heartbeat check — 8 hour session
    const SESSION_DURATION_MS = 8 * 3600 * 1000;
    const heartbeat = setInterval(() => {
      const now = Date.now();
      const sessionExpiry = sessionStartRef.current + SESSION_DURATION_MS;
      const remaining = sessionExpiry - now;
      const idleTime = now - lastActivityRef.current;

      // Session expired
      if (remaining <= 0) {
        clearInterval(heartbeat);
        logout();
        return;
      }

      // Idle timeout
      if (idleTime >= IDLE_TIMEOUT_MS && !warningDismissedRef.current) {
        setShowWarning(true);
        setTimeRemaining(remaining);
        return;
      }

      // Session about to expire
      if (remaining <= SESSION_WARNING_MS && !warningDismissedRef.current) {
        setShowWarning(true);
        setTimeRemaining(remaining);
      }
    }, HEARTBEAT_INTERVAL_MS);

    return () => {
      clearInterval(heartbeat);
      clearTimeout(debounceTimer);
      events.forEach(e => window.removeEventListener(e, handler));
    };
  }, [user, logout, recordActivity]);

  const extendSession = useCallback(() => {
    setShowWarning(false);
    warningDismissedRef.current = true;
    lastActivityRef.current = Date.now();
    // In a real app, this would call the backend to refresh the JWT
    setTimeout(() => { warningDismissedRef.current = false; }, SESSION_WARNING_MS);
  }, []);

  const dismissWarning = useCallback(() => {
    setShowWarning(false);
    warningDismissedRef.current = true;
    setTimeout(() => { warningDismissedRef.current = false; }, 5 * 60 * 1000);
  }, []);

  return { showWarning, timeRemaining, extendSession, dismissWarning };
}

interface Props {
  show: boolean;
  timeRemaining: number;
  onExtend: () => void;
  onLogout: () => void;
}

export function SessionWarningModal({ show, timeRemaining, onExtend, onLogout }: Props) {
  const [countdown, setCountdown] = useState(timeRemaining);

  useEffect(() => {
    if (!show) return;
    setCountdown(timeRemaining);
    const timer = setInterval(() => {
      setCountdown(c => {
        if (c <= 1000) {
          clearInterval(timer);
          onLogout();
          return 0;
        }
        return c - 1000;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [show, timeRemaining, onLogout]);

  if (!show) return null;

  const minutes = Math.floor(countdown / 60000);
  const seconds = Math.floor((countdown % 60000) / 1000);

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 10000,
      background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }}>
      <div style={{
        background: 'linear-gradient(180deg, #0E1117 0%, #080c10 100%)',
        border: '1px solid rgba(255,204,0,0.15)',
        borderRadius: 12,
        padding: '32px 40px',
        maxWidth: 380,
        textAlign: 'center',
        boxShadow: '0 16px 64px rgba(0,0,0,0.5), 0 0 32px rgba(255,204,0,0.05)',
        animation: 'cam-fullscreen-enter 0.2s ease-out',
      }}>
        <div style={{ fontSize: 40, marginBottom: 16 }}>⏰</div>
        <div style={{
          fontSize: 16, fontWeight: 700, color: '#E89B2A',
          marginBottom: 8,
        }}>
          Session Expiring Soon
        </div>
        <div style={{
          fontSize: 11, color: '#8891A5', lineHeight: 1.6,
          marginBottom: 20,
        }}>
          Your session will expire in
        </div>
        <div style={{
          fontSize: 36, fontWeight: 700, color: countdown < 60000 ? '#EF4444' : '#E89B2A',
          fontFamily: "'JetBrains Mono', monospace",
          marginBottom: 24,
          transition: 'color 0.3s',
        }}>
          {minutes}:{seconds.toString().padStart(2, '0')}
        </div>
        <div style={{
          fontSize: 10, color: '#4A5268', marginBottom: 20,
        }}>
          Click "Extend Session" to continue working, or you will be logged out automatically.
        </div>
        <div style={{ display: 'flex', gap: 10, justifyContent: 'center' }}>
          <button
            onClick={onLogout}
            style={{
              padding: '8px 20px', borderRadius: 6,
              background: 'rgba(255,255,255,0.03)',
              border: '1px solid rgba(255,255,255,0.08)',
              color: '#8891A5', fontSize: 12, fontWeight: 600,
              cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            Log Out
          </button>
          <button
            onClick={onExtend}
            style={{
              padding: '8px 20px', borderRadius: 6,
              background: 'rgba(0,212,255,0.1)',
              border: '1px solid rgba(0,212,255,0.25)',
              color: '#E8732A', fontSize: 12, fontWeight: 700,
              cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            Extend Session
          </button>
        </div>
      </div>
    </div>
  );
}
