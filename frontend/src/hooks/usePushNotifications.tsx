'use client';

import { useState, useCallback, useEffect } from 'react';
import { BRAND } from '@/lib/branding';

type PermissionState = 'default' | 'granted' | 'denied' | 'unsupported';

export function usePushNotifications() {
  const [permission, setPermission] = useState<PermissionState>('default');
  const [isSubscribed, setIsSubscribed] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined' || !('Notification' in window)) {
      setPermission('unsupported');
      return;
    }
    setPermission(Notification.permission as PermissionState);
    // Check if already subscribed
    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.ready.then(reg => {
        reg.pushManager.getSubscription().then(sub => {
          setIsSubscribed(!!sub);
        }).catch(() => {});
      }).catch(() => {});
    }
  }, []);

  const requestPermission = useCallback(async () => {
    if (!('Notification' in window)) {
      setPermission('unsupported');
      return false;
    }
    const result = await Notification.requestPermission();
    setPermission(result as PermissionState);
    return result === 'granted';
  }, []);

  const subscribe = useCallback(async () => {
    if (!('serviceWorker' in navigator)) return false;
    try {
      const granted = await requestPermission();
      if (!granted) return false;

      const reg = await navigator.serviceWorker.ready;
      const subscription = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: 'BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkOs-qSg6r20ePa3CzEYPTMrj_S3CmAaVCMjIJpbNU',
      });
      setIsSubscribed(true);
      // In production, send subscription to backend:
      // await fetch('/api/push/subscribe', { method: 'POST', body: JSON.stringify(subscription) });
      console.log('[Push] Subscribed:', subscription.endpoint);
      return true;
    } catch (err) {
      console.warn('[Push] Subscribe failed:', err);
      return false;
    }
  }, [requestPermission]);

  const unsubscribe = useCallback(async () => {
    if (!('serviceWorker' in navigator)) return;
    try {
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.getSubscription();
      if (sub) {
        await sub.unsubscribe();
        setIsSubscribed(false);
      }
    } catch (err) {
      console.warn('[Push] Unsubscribe failed:', err);
    }
  }, []);

  const sendTestNotification = useCallback(() => {
    if (permission !== 'granted') return;
    new Notification(`${BRAND.name} — Test Alert`, {
      body: '🚨 Critical: Worker without harness detected at Southgate Power Station, Zone B',
      icon: '/icon-192.png',
      badge: '/icon-192.png',
      tag: 'test-alert',
      requireInteraction: true,
    });
  }, [permission]);

  return {
    permission,
    isSubscribed,
    requestPermission,
    subscribe,
    unsubscribe,
    sendTestNotification,
  };
}

// ── Push Settings Panel ──
export function PushNotificationSettings() {
  const { permission, isSubscribed, subscribe, unsubscribe, sendTestNotification } = usePushNotifications();

  const statusColor = {
    granted: '#22C55E',
    denied: '#EF4444',
    default: '#E89B2A',
    unsupported: '#4A5268',
  }[permission];

  const statusLabel = {
    granted: 'Enabled',
    denied: 'Blocked',
    default: 'Not Set',
    unsupported: 'Unsupported',
  }[permission];

  return (
    <div style={{
      background: '#0E1117',
      border: '1px solid rgba(255,255,255,0.04)',
      borderRadius: 6,
      padding: 16,
    }}>
      <div style={{
        display: 'flex', alignItems: 'center', gap: 10,
        marginBottom: 14,
      }}>
        <span style={{ fontSize: 18 }}>🔔</span>
        <div style={{ flex: 1 }}>
          <div style={{
            fontSize: 12, fontWeight: 700, color: '#E4E8F0',
            letterSpacing: 0.5,
          }}>
            Push Notifications
          </div>
          <div style={{ fontSize: 9, color: '#4A5268', marginTop: 1 }}>
            Get critical alerts even when the app is in the background
          </div>
        </div>
        <div style={{
          display: 'flex', alignItems: 'center', gap: 5,
          padding: '3px 8px', borderRadius: 10,
          background: `${statusColor}10`,
          border: `1px solid ${statusColor}25`,
        }}>
          <div style={{
            width: 6, height: 6, borderRadius: '50%',
            background: statusColor,
            boxShadow: permission === 'granted' ? `0 0 6px ${statusColor}` : 'none',
          }} />
          <span style={{
            fontSize: 9, fontWeight: 600, color: statusColor,
            fontFamily: "'JetBrains Mono', monospace",
          }}>
            {statusLabel}
          </span>
        </div>
      </div>

      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {permission === 'unsupported' ? (
          <div style={{
            padding: '8px 12px', borderRadius: 4,
            background: 'rgba(255,255,255,0.02)',
            color: '#4A5268', fontSize: 10,
          }}>
            Push notifications are not supported in this browser
          </div>
        ) : permission === 'denied' ? (
          <div style={{
            padding: '8px 12px', borderRadius: 4,
            background: 'rgba(255,51,85,0.04)',
            color: '#EF4444', fontSize: 10,
          }}>
            Notifications are blocked. Please enable them in your browser settings.
          </div>
        ) : (
          <>
            {!isSubscribed ? (
              <button
                onClick={subscribe}
                style={{
                  padding: '7px 14px', borderRadius: 4,
                  background: 'rgba(0,229,160,0.08)',
                  border: '1px solid rgba(0,229,160,0.2)',
                  color: '#22C55E', fontSize: 11, fontWeight: 600,
                  cursor: 'pointer', fontFamily: 'inherit',
                }}
              >
                🔔 Enable Notifications
              </button>
            ) : (
              <button
                onClick={unsubscribe}
                style={{
                  padding: '7px 14px', borderRadius: 4,
                  background: 'rgba(255,255,255,0.03)',
                  border: '1px solid rgba(255,255,255,0.06)',
                  color: '#8891A5', fontSize: 11, fontWeight: 600,
                  cursor: 'pointer', fontFamily: 'inherit',
                }}
              >
                Disable Notifications
              </button>
            )}
            {permission === 'granted' && (
              <button
                onClick={sendTestNotification}
                style={{
                  padding: '7px 14px', borderRadius: 4,
                  background: 'rgba(0,212,255,0.06)',
                  border: '1px solid rgba(0,212,255,0.15)',
                  color: '#E8732A', fontSize: 11, fontWeight: 600,
                  cursor: 'pointer', fontFamily: 'inherit',
                }}
              >
                🧪 Test Alert
              </button>
            )}
          </>
        )}
      </div>

      {/* Notification preferences */}
      {permission === 'granted' && (
        <div style={{
          marginTop: 14, paddingTop: 14,
          borderTop: '1px solid rgba(255,255,255,0.04)',
        }}>
          <div style={{
            fontSize: 9, fontWeight: 600, letterSpacing: 1,
            textTransform: 'uppercase', color: '#4A5268', marginBottom: 8,
          }}>
            Alert Types
          </div>
          {[
            { label: 'Critical Alerts', desc: 'SLA breaches, safety violations', enabled: true },
            { label: 'High Priority', desc: 'Escalations, active incidents', enabled: true },
            { label: 'Shift Changes', desc: 'Handoff notifications', enabled: false },
            { label: 'System Status', desc: 'Camera offline, connectivity', enabled: false },
          ].map((pref, i) => (
            <div key={i} style={{
              display: 'flex', alignItems: 'center', gap: 10,
              padding: '6px 0',
            }}>
              <div style={{
                width: 32, height: 18, borderRadius: 9,
                background: pref.enabled ? 'rgba(0,229,160,0.2)' : 'rgba(255,255,255,0.06)',
                position: 'relative', cursor: 'pointer',
                transition: 'background 0.2s',
              }}>
                <div style={{
                  width: 14, height: 14, borderRadius: '50%',
                  background: pref.enabled ? '#22C55E' : '#4A5268',
                  position: 'absolute', top: 2,
                  left: pref.enabled ? 16 : 2,
                  transition: 'left 0.2s, background 0.2s',
                  boxShadow: pref.enabled ? '0 0 6px rgba(0,229,160,0.4)' : 'none',
                }} />
              </div>
              <div style={{ flex: 1 }}>
                <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>
                  {pref.label}
                </div>
                <div style={{ fontSize: 9, color: '#4A5268' }}>{pref.desc}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
