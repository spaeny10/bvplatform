'use client';

import { useEffect, useState } from 'react';
import { BRAND } from '@/lib/branding';

// PWAManager handles three pieces of PWA plumbing for the app shell:
//
//   1. Service-worker registration. We register /sw.js once on mount.
//      Failures are logged but never propagated — a failed SW means
//      no installability, but the app still works.
//
//   2. Android/Chrome install-prompt UI. Browsers fire a
//      `beforeinstallprompt` event when the app meets installability
//      criteria; we capture it, defer the default banner, and surface
//      our own prompt that fits the brand. Customer dismissal is
//      remembered for 30 days so we don't nag.
//
//   3. iOS Safari "Add to Home Screen" hint. iOS never fires
//      `beforeinstallprompt`; instead, users must manually tap Share
//      then "Add to Home Screen." We detect iOS Safari (user-agent
//      pattern + navigator.standalone check) and show a dismissible
//      instruction banner with the iOS Share glyph. Dismissal
//      persists to localStorage for 30 days.
//
// Mount this once at the root layout (it renders nothing on
// desktop / when the app is already installed / when the user has
// dismissed). All side effects are wrapped in browser-only checks
// so SSR doesn't trip on `window`.

const DISMISS_KEY = 'ironsight_pwa_dismissed_until';
const IOS_DISMISS_KEY = 'ironsight_ios_pwa_dismissed_until';
const DISMISS_DAYS = 30;

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

function detectIosSafari(): boolean {
  if (typeof window === 'undefined' || typeof navigator === 'undefined') return false;
  const ua = navigator.userAgent;
  // Matches iPhone/iPad/iPod running Safari (not Chrome/Firefox on iOS,
  // which can't install anyway). Also guards against already-installed PWA
  // (navigator.standalone === true) where we don't need the hint.
  const isIos = /iphone|ipad|ipod/i.test(ua);
  const isSafari = /safari/i.test(ua) && !/crios|fxios|opios|edgios/i.test(ua);
  const isStandalone =
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (navigator as any).standalone === true ||
    window.matchMedia('(display-mode: standalone)').matches;
  return isIos && isSafari && !isStandalone;
}

export default function PWAManager() {
  const [installEvent, setInstallEvent] = useState<BeforeInstallPromptEvent | null>(null);
  const [visible, setVisible] = useState(false);
  const [iosVisible, setIosVisible] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined') return;

    // Register the service worker. We don't await this; the page
    // shouldn't block on it.
    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js').catch((err) => {
        console.warn('[PWA] sw register failed:', err);
      });
    }

    // ── Android/Chrome: beforeinstallprompt ──────────────────────
    const onPrompt = (e: Event) => {
      e.preventDefault();
      setInstallEvent(e as BeforeInstallPromptEvent);

      const until = parseInt(localStorage.getItem(DISMISS_KEY) ?? '0', 10);
      if (Date.now() < until) return;

      if (window.innerWidth <= 900) {
        setVisible(true);
      }
    };

    window.addEventListener('beforeinstallprompt', onPrompt as EventListener);

    // ── iOS Safari: manual Add to Home Screen hint ───────────────
    if (detectIosSafari()) {
      const until = parseInt(localStorage.getItem(IOS_DISMISS_KEY) ?? '0', 10);
      if (Date.now() >= until) {
        setIosVisible(true);
      }
    }

    return () => window.removeEventListener('beforeinstallprompt', onPrompt as EventListener);
  }, []);

  // ── Android/Chrome install handler ──────────────────────────────
  const handleInstall = async () => {
    if (!installEvent) return;
    setVisible(false);
    try {
      await installEvent.prompt();
      const result = await installEvent.userChoice;
      if (result.outcome === 'dismissed') {
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

  const handleIosDismiss = () => {
    const until = Date.now() + DISMISS_DAYS * 24 * 60 * 60 * 1000;
    localStorage.setItem(IOS_DISMISS_KEY, String(until));
    setIosVisible(false);
  };

  // Shared banner positioning style
  const bannerStyle: React.CSSProperties = {
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
  };

  // ── iOS hint banner ─────────────────────────────────────────────
  if (iosVisible) {
    return (
      <div role="dialog" aria-label={`Install ${BRAND.name} on your home screen`} style={bannerStyle}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 4 }}>
            Install {BRAND.name} on your home screen
          </div>
          <div style={{ fontSize: 11, color: 'var(--sg-text-dim, #9CA3AF)', lineHeight: 1.5 }}>
            {/* The iOS share glyph (U+2B06 + box) is the canonical representation
                of the iOS Share button that Safari shows in the toolbar. */}
            Tap&nbsp;
            <span aria-label="Share" style={{ display: 'inline-block', verticalAlign: 'middle' }}>
              {/* iOS Share icon approximation using unicode */}
              &#x2BA1;
            </span>
            &nbsp;then &ldquo;Add to Home Screen&rdquo; for faster access and offline support.
          </div>
        </div>
        <button
          onClick={handleIosDismiss}
          // 44×44px minimum touch target
          style={{
            minWidth: 44, minHeight: 44,
            padding: '6px 10px', fontSize: 11, fontWeight: 600,
            background: 'transparent',
            border: '1px solid rgba(255,255,255,0.15)',
            borderRadius: 4,
            color: 'var(--sg-text-dim, #B0B8C8)',
            cursor: 'pointer', fontFamily: 'inherit',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}
          aria-label="Dismiss install hint"
        >
          Not now
        </button>
      </div>
    );
  }

  // ── Android/Chrome install banner ───────────────────────────────
  if (!visible || !installEvent) return null;

  return (
    <div
      role="dialog"
      aria-label={`Install ${BRAND.name} app`}
      style={bannerStyle}
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
          minWidth: 44, minHeight: 44,
          padding: '6px 10px', fontSize: 11, fontWeight: 600,
          background: 'transparent',
          border: '1px solid rgba(255,255,255,0.15)',
          borderRadius: 4,
          color: 'var(--sg-text-dim, #B0B8C8)',
          cursor: 'pointer', fontFamily: 'inherit',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}
      >
        Not now
      </button>
      <button
        onClick={handleInstall}
        style={{
          minWidth: 44, minHeight: 44,
          padding: '6px 14px', fontSize: 12, fontWeight: 700,
          background: 'var(--brand-primary, #E8732A)', color: '#fff',
          border: 'none', borderRadius: 4,
          cursor: 'pointer', fontFamily: 'inherit',
          whiteSpace: 'nowrap',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}
      >
        Install
      </button>
    </div>
  );
}
