'use client';

// Gates for the 2026-06 MVP descope. Two shapes:
//
//   <FeatureGate flag="analytics">…</FeatureGate>
//     Renders children only when the flag is on. Renders nothing while
//     flags load and nothing when off — for nav links, tabs, cards.
//
//   <FeaturePageGate flag="analytics">…</FeaturePageGate>
//     Whole-page variant: 404s (next/navigation notFound) when the
//     flag is off, so parked routes like /analytics are unreachable,
//     not just unlinked. Renders nothing while flags load to avoid
//     flashing parked content.
//
// Flag semantics + the canonical list: docs/feature-registry/README.md
// and lib/feature-flags.ts.

import { notFound } from 'next/navigation';
import type { ReactNode } from 'react';
import { type FeatureFlagName, useFeatureFlag } from '@/lib/feature-flags';

export function FeatureGate({ flag, children }: { flag: FeatureFlagName; children: ReactNode }) {
  const { enabled, loaded } = useFeatureFlag(flag);
  if (!loaded || !enabled) return null;
  return <>{children}</>;
}

export function FeaturePageGate({ flag, children }: { flag: FeatureFlagName; children: ReactNode }) {
  const { enabled, loaded } = useFeatureFlag(flag);
  if (!loaded) return null;
  if (!enabled) notFound();
  return <>{children}</>;
}
