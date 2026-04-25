'use client';

import type { CSSProperties } from 'react';

// Reusable skeleton placeholder. Two compositions:
//
//   <Skeleton w={120} h={14} /> — single block
//   <SkeletonRows count={6} /> — list of rows for table/card placeholders
//
// The shimmer animation is defined alongside the component (no CSS
// file needed) so any page can drop these in without an import-side-
// effect. Background colors come from the same CSS custom properties
// the rest of the UI uses, so the skeleton matches whichever theme
// is active.

const shimmerKeyframes = `
@keyframes sg-skeleton-shimmer {
  0%   { background-position: -200% 0; }
  100% { background-position: 200% 0; }
}
`;

if (typeof document !== 'undefined' && !document.getElementById('sg-skeleton-keyframes')) {
  const style = document.createElement('style');
  style.id = 'sg-skeleton-keyframes';
  style.textContent = shimmerKeyframes;
  document.head.appendChild(style);
}

const baseStyle: CSSProperties = {
  display: 'inline-block',
  borderRadius: 4,
  background:
    'linear-gradient(90deg, var(--sg-surface-1, rgba(255,255,255,0.04)) 0%, var(--sg-surface-2, rgba(255,255,255,0.08)) 50%, var(--sg-surface-1, rgba(255,255,255,0.04)) 100%)',
  backgroundSize: '200% 100%',
  animation: 'sg-skeleton-shimmer 1.4s ease-in-out infinite',
};

interface SkeletonProps {
  w?: number | string;
  h?: number | string;
  style?: CSSProperties;
  rounded?: number;
}

export default function Skeleton({ w = '100%', h = 14, style, rounded }: SkeletonProps) {
  return (
    <span
      style={{
        ...baseStyle,
        width: typeof w === 'number' ? `${w}px` : w,
        height: typeof h === 'number' ? `${h}px` : h,
        borderRadius: rounded ?? 4,
        ...style,
      }}
      aria-hidden="true"
    />
  );
}

interface SkeletonRowsProps {
  count?: number;
  height?: number;
  gap?: number;
}

// SkeletonRows renders N stacked placeholder lines, useful for table
// or card-list loading states. The widths vary subtly (90% / 75% /
// 100%) so the result reads as "list of items" rather than a solid
// grey block.
export function SkeletonRows({ count = 5, height = 18, gap = 10 }: SkeletonRowsProps) {
  const widths = ['100%', '88%', '94%', '82%', '96%', '85%', '92%', '90%'];
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap }} aria-busy="true" aria-live="polite">
      {Array.from({ length: count }).map((_, i) => (
        <Skeleton key={i} h={height} w={widths[i % widths.length]} />
      ))}
    </div>
  );
}
