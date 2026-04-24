'use client';

import type { Severity } from '@/types/ironsight';

interface SeverityPillProps {
  severity: Severity;
  size?: 'sm' | 'md';
  className?: string;
}

const DARK_COLORS: Record<Severity, { bg: string; border: string; text: string }> = {
  critical: { bg: 'rgba(255,51,85,0.12)', border: 'rgba(255,51,85,0.4)', text: '#EF4444' },
  high:     { bg: 'rgba(255,107,53,0.12)', border: 'rgba(255,107,53,0.4)', text: '#EF4444' },
  medium:   { bg: 'rgba(255,204,0,0.12)', border: 'rgba(255,204,0,0.4)', text: '#E89B2A' },
  low:      { bg: 'rgba(0,212,255,0.12)', border: 'rgba(0,212,255,0.4)', text: '#E8732A' },
};

const LIGHT_COLORS: Record<Severity, { bg: string; border: string; text: string }> = {
  critical: { bg: 'rgba(192,49,26,0.08)', border: 'rgba(192,49,26,0.2)', text: '#c0311a' },
  high:     { bg: 'rgba(160,88,0,0.08)', border: 'rgba(160,88,0,0.2)', text: '#a05800' },
  medium:   { bg: 'rgba(154,111,0,0.08)', border: 'rgba(154,111,0,0.2)', text: '#9a6f00' },
  low:      { bg: 'rgba(26,79,138,0.08)', border: 'rgba(26,79,138,0.15)', text: '#1a4f8a' },
};

export default function SeverityPill({ severity, size = 'md', className = '' }: SeverityPillProps) {
  // Detect theme from closest parent — uses CSS custom property
  const colors = DARK_COLORS[severity]; // default dark; the component adapts via CSS if needed

  const fontSize = size === 'sm' ? 9 : 10;
  const padding = size === 'sm' ? '1px 6px' : '2px 8px';

  return (
    <span
      className={`sg-severity-pill ${className}`}
      data-severity={severity}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding,
        borderRadius: 3,
        fontSize,
        fontWeight: 700,
        letterSpacing: '0.5px',
        textTransform: 'uppercase' as const,
        background: colors.bg,
        border: `1px solid ${colors.border}`,
        color: colors.text,
        fontFamily: 'var(--font-mono, monospace)',
        whiteSpace: 'nowrap' as const,
      }}
    >
      <span style={{
        width: 5, height: 5, borderRadius: '50%',
        background: colors.text, flexShrink: 0,
      }} />
      {severity}
    </span>
  );
}

/** Light-theme variant for portal pages */
export function SeverityPillLight({ severity, size = 'md', className = '' }: SeverityPillProps) {
  const colors = LIGHT_COLORS[severity];
  const fontSize = size === 'sm' ? 9 : 10;
  const padding = size === 'sm' ? '1px 6px' : '2px 8px';

  return (
    <span
      className={`sg-severity-pill-light ${className}`}
      data-severity={severity}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding,
        borderRadius: 3,
        fontSize,
        fontWeight: 700,
        letterSpacing: '0.5px',
        textTransform: 'uppercase' as const,
        background: colors.bg,
        border: `1px solid ${colors.border}`,
        color: colors.text,
        fontFamily: 'var(--font-mono, monospace)',
        whiteSpace: 'nowrap' as const,
      }}
    >
      <span style={{
        width: 5, height: 5, borderRadius: '50%',
        background: colors.text, flexShrink: 0,
      }} />
      {severity}
    </span>
  );
}
