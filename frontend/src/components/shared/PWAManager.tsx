'use client';

import { useEffect, useState } from 'react';
import { BRAND } from '@/lib/branding';

// PWAManager handles two pieces of PWA plumbing for the app shell:
//
//   1. Service-worker registration. We register /sw.js once on mount.
//      Failures are logged but never propagated — a failed SW means
//      no installability, but the app still works.
//
//   2. Install-prompt UI. Browsers fire a `beforeinstallprompt`
//      event when the app meets installability criteria; we capture
//      it, defer the default banner, and surface our own prompt
//      that fits the brand. Customer dismissal is remembered for
//      30 days so we don't nag.
//
// Mount this once at the root layout (it renders nothing on
// desktop / when the app is already installed / when the user has
// dismissed). All side effects are wrapped in browser-only checks
// so SSR doesn't trip on `window`.

const DISMISS_KEY = 'ironsight_pwa_dismissed_until';
const DISMISS_DAYS = 30;

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

export default function PWAManager() {
  const [installEvent, setInstallEvent] = useState<BeforeInstallPromptEvent | null>(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined') return;

    // Register the service worker. We don't await this; the page
    // shouldn't block on it.
    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js').catch((err) => {
        console.warn('[PWA] sw register failed:', err);
      });
    }

    // Listen for the install-eligible event. Stash it so a click on
    // our custom prompt button can replay it.
    const onPrompt = (e: Event) => {
      e.preventDefault();
      setInstallEvent(e as BeforeInstallPromptEvent);

      // Respect dismissal. Only show our prompt if the user hasn't
      // closed it within the last 30 days.
      const until = parseInt(localStorage.getItem(DISMISS_KEY) ?? '0', 10);
      if (Date.now() < until) return;

      // And only on viewports where install actually adds value —
      // mobile and tablet. Desktop already has the URL bar's
      // browser-native install button, no need to nag.
      if (window.innerWidth <= 900) {
        setVisible(true);
      }
    };

    window.addEventListener('beforeinstallprompt', onPrompt as EventListener);
    return () => window.removeEventListener('beforeinstallprompt', onPrompt as EventListener);
  }, []);

  const handleInstall = async () => {
    if (!installEvent) return;
    setVisible(false);
    try {
      await installEvent.prompt();
      const result = await installEvent.userChoice;
      if (result.outcome === 'dismissed') {
        // Don't ask again for 30 days
        const until = Date.now() + DISMISS_DAYS * 24 * 60 * 60 * 1000;
        localStorage.setItem(DISMISS_KEY, String(until));
      }
    } catch {
      // user agent declined or threw; silently swallow
    }
    setInstallEvent(null);
  };

  const handleDismiss = () => {
    const until = Date.now() + DISMISS_DAYS * 24 * 60 * 60 * 1000;
    localStorage.setItem(DISMISS_KEY, String(until));
    setVisible(false);
  };

  if (!visible || !installEvent) return null;

  return (
    <div
      role="dialog"
      aria-label={`Install ${BRAND.name} app`}
      style={{
        position: 'fixed',
        bottom: 16, left: 16, right: 16,
        padding: 14,
        background: 'var(--sg-surface-1, #181c22)',
        border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.10))',
        borderRadius: 10,
        boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        color: 'var(--sg-text-primary, #E4E8F0)',
        zIndex: 9999,
        fontFamily: "var(--font-family, 'Inter', sans-serif)",
        display: 'flex', alignItems: 'center', gap: 12,
      }}
    >
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 2 }}>
          Install {BRAND.name} on your home screen
        </div>
        <div style={{ fontSize: 11, color: 'var(--sg-text-dim, #9CA3AF)', lineHeight: 1.4 }}>
          Faster access from your phone — and notifications work even
          when the browser is closed.
        </div>
      </div>
      <button
        onClick={handleDismiss}
        style={{
          padding: '6px 10px', fontSize: 11, fontWeight: 600,
          background: 'transparent',
          border: '1px solid rgba(255,255,255,0.15)',
          borderRadius: 4,
          color: 'var(--sg-text-dim, #B0B8C8)',
          cursor: 'pointer', fontFamily: 'inherit',
        }}
      >
        Not now
      </button>
      <button
        onClick={handleInstall}
        style={{
          padding: '6px 14px', fontSize: 12, fontWeight: 700,
          background: 'var(--brand-primary, #E8732A)', color: '#fff',
          border: 'none', borderRadius: 4,
          cursor: 'pointer', fontFamily: 'inherit',
          whiteSpace: 'nowrap',
        }}
      >
        Install
      </button>
    </div>
  );
}
