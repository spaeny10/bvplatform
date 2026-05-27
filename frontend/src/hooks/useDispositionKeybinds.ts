'use client';

// Extracted from ActiveAlarmView.tsx (P1-B-11). Encapsulates the F/V +
// 1–5 keyboard chord that maps to a disposition code. Pressing F or V
// arms a 1.5s window during which a digit 1–5 selects one of the five
// false-positive or verified codes respectively.
//
// Returns the live keyPrefix so callers can render an "armed" indicator.

import { useEffect, useRef, useState } from 'react';
import { DISPOSITION_OPTIONS, type DispositionCode } from '@/stores/operator-store';

const ARM_WINDOW_MS = 1500;

export function useDispositionKeybinds(
  onSelect: (code: DispositionCode) => void,
): 'f' | 'v' | null {
  const [keyPrefix, setKeyPrefix] = useState<'f' | 'v' | null>(null);
  const keyPrefixTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const falseCodes = DISPOSITION_OPTIONS.filter(d => d.category === 'false');
    const verifiedCodes = DISPOSITION_OPTIONS.filter(d => d.category === 'verified');

    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as Element).tagName;
      if (tag === 'TEXTAREA' || tag === 'INPUT' || tag === 'SELECT') return;

      const key = e.key.toLowerCase();

      if (key === 'f' || key === 'v') {
        if (keyPrefixTimer.current) clearTimeout(keyPrefixTimer.current);
        setKeyPrefix(key as 'f' | 'v');
        keyPrefixTimer.current = setTimeout(() => setKeyPrefix(null), ARM_WINDOW_MS);
        return;
      }

      const digit = parseInt(e.key);
      if (keyPrefix && digit >= 1 && digit <= 5) {
        e.preventDefault();
        const opts = keyPrefix === 'f' ? falseCodes : verifiedCodes;
        if (opts[digit - 1]) onSelect(opts[digit - 1].code);
        setKeyPrefix(null);
        if (keyPrefixTimer.current) clearTimeout(keyPrefixTimer.current);
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [keyPrefix, onSelect]);

  return keyPrefix;
}
