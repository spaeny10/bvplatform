'use client';

import { useEffect, useRef, useCallback, useState, ReactNode } from 'react';

// ── Focus Trap for Modals ──
// Traps keyboard focus within a container (WCAG 2.4.3)
export function useFocusTrap(isOpen: boolean) {
  const containerRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!isOpen || !containerRef.current) return;

    // Save current focus to restore later
    previousFocusRef.current = document.activeElement as HTMLElement;

    const container = containerRef.current;
    const focusableSelector = [
      'a[href]', 'button:not([disabled])', 'input:not([disabled])',
      'select:not([disabled])', 'textarea:not([disabled])',
      '[tabindex]:not([tabindex="-1"])',
    ].join(', ');

    const focusables = container.querySelectorAll<HTMLElement>(focusableSelector);
    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    // Auto-focus first focusable
    if (first) first.focus();

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return;

      if (e.shiftKey) {
        if (document.activeElement === first) {
          e.preventDefault();
          last?.focus();
        }
      } else {
        if (document.activeElement === last) {
          e.preventDefault();
          first?.focus();
        }
      }
    };

    container.addEventListener('keydown', handleKeyDown);

    return () => {
      container.removeEventListener('keydown', handleKeyDown);
      // Restore focus on close
      previousFocusRef.current?.focus();
    };
  }, [isOpen]);

  return containerRef;
}

// ── Live Region Announcer ──
// For screen reader announcements of dynamic content (WCAG 4.1.3)
let announceTimeout: NodeJS.Timeout | null = null;

export function announceToScreenReader(message: string, priority: 'polite' | 'assertive' = 'polite') {
  let region = document.getElementById(`sr-announce-${priority}`);
  if (!region) {
    region = document.createElement('div');
    region.id = `sr-announce-${priority}`;
    region.setAttribute('role', 'status');
    region.setAttribute('aria-live', priority);
    region.setAttribute('aria-atomic', 'true');
    Object.assign(region.style, {
      position: 'absolute', width: '1px', height: '1px',
      overflow: 'hidden', clip: 'rect(0,0,0,0)',
      whiteSpace: 'nowrap', border: 0,
    });
    document.body.appendChild(region);
  }

  // Clear and re-announce to trigger screen reader
  if (announceTimeout) clearTimeout(announceTimeout);
  region.textContent = '';
  announceTimeout = setTimeout(() => {
    region!.textContent = message;
  }, 100);
}

// ── Skip to Content Link ──
// WCAG 2.4.1 — Bypass blocks
export function SkipToContent({ targetId = 'main-content' }: { targetId?: string }) {
  return (
    <a
      href={`#${targetId}`}
      style={{
        position: 'absolute', left: -9999, top: 4,
        background: '#E8732A', color: '#080c10',
        padding: '8px 16px', borderRadius: 4,
        fontSize: 12, fontWeight: 700, zIndex: 10000,
        textDecoration: 'none',
      }}
      onFocus={(e) => { e.currentTarget.style.left = '4px'; }}
      onBlur={(e) => { e.currentTarget.style.left = '-9999px'; }}
    >
      Skip to main content
    </a>
  );
}

// ── Keyboard Navigation Hook ──
// Arrow key navigation for lists (WCAG 2.1.1)
export function useArrowKeyNavigation(itemCount: number) {
  const [activeIndex, setActiveIndex] = useState(0);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowRight':
        e.preventDefault();
        setActiveIndex(i => Math.min(i + 1, itemCount - 1));
        break;
      case 'ArrowUp':
      case 'ArrowLeft':
        e.preventDefault();
        setActiveIndex(i => Math.max(i - 1, 0));
        break;
      case 'Home':
        e.preventDefault();
        setActiveIndex(0);
        break;
      case 'End':
        e.preventDefault();
        setActiveIndex(itemCount - 1);
        break;
    }
  }, [itemCount]);

  return { activeIndex, setActiveIndex, handleKeyDown };
}

// ── Visually Hidden (but accessible) ──
export function VisuallyHidden({ children }: { children: ReactNode }) {
  return (
    <span style={{
      position: 'absolute', width: 1, height: 1,
      overflow: 'hidden', clip: 'rect(0,0,0,0)',
      whiteSpace: 'nowrap', border: 0, padding: 0, margin: -1,
    }}>
      {children}
    </span>
  );
}

// ── High Contrast Mode Detection ──
export function useHighContrastMode() {
  const [isHighContrast, setIsHighContrast] = useState(false);

  useEffect(() => {
    const mq = window.matchMedia('(forced-colors: active)');
    setIsHighContrast(mq.matches);
    const handler = (e: MediaQueryListEvent) => setIsHighContrast(e.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  return isHighContrast;
}

// ── Reduced Motion Detection ──
export function useReducedMotion() {
  const [prefersReduced, setPrefersReduced] = useState(false);

  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    setPrefersReduced(mq.matches);
    const handler = (e: MediaQueryListEvent) => setPrefersReduced(e.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  return prefersReduced;
}
