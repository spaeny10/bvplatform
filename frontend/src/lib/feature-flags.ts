// ── Feature flags ──
//
// Deploy-wide flags for the 2026-06 MVP descope: parked surfaces
// (analytics, SOC console, compliance, speakers, semantic search, AI
// panels, labeling, integrations, support tickets, evidence sharing)
// are hidden behind these flags so customers never see mock-data or
// half-wired pages. The MVP core (recording, playback, live view,
// alerts, login, admin basics) has no flags — it is always on.
//
// Source of truth is the backend: GET /api/v1/features returns
// api.DefaultFeatureFlags merged with the FEATURES_OVERRIDE env var
// (see internal/api/platform.go). This module hydrates once per page
// load and caches; until the fetch resolves, the static defaults
// below apply (mirror the backend defaults = everything parked OFF,
// so a slow fetch can never flash a parked page open).
//
// Canonical flag list + what each gates: docs/feature-registry/README.md.

'use client';

import { useEffect, useState } from 'react';
import { authFetch } from '@/lib/api';

export type FeatureFlagName =
  | 'analytics'
  | 'operator_console'
  | 'compliance'
  | 'person_tracking'
  | 'speakers'
  | 'semantic_search'
  | 'vlm_safety'
  | 'evidence_sharing'
  | 'labeling'
  | 'support_tickets'
  | 'integrations'
  | 'ai_insights'
  | 'weekly_digest'
  | 'global_ai_training';

export type FeatureFlags = Record<FeatureFlagName, boolean>;

// Must mirror api.DefaultFeatureFlags — parked features default OFF.
const DEFAULT_FLAGS: FeatureFlags = {
  analytics: false,
  operator_console: false,
  compliance: false,
  person_tracking: false,
  speakers: false,
  semantic_search: false,
  vlm_safety: false,
  evidence_sharing: false,
  labeling: false,
  support_tickets: false,
  integrations: false,
  ai_insights: false,
  weekly_digest: false,
  global_ai_training: false,
};

let cached: FeatureFlags | null = null;
let inflight: Promise<FeatureFlags> | null = null;

async function fetchFlags(): Promise<FeatureFlags> {
  try {
    const res = await authFetch('/api/v1/features');
    if (!res.ok) return DEFAULT_FLAGS;
    const data = (await res.json()) as Record<string, boolean>;
    // Merge over defaults so a missing key in the response (older
    // backend) falls back to the parked-off default rather than
    // undefined.
    return { ...DEFAULT_FLAGS, ...data };
  } catch {
    return DEFAULT_FLAGS;
  }
}

/** Resolve the effective flag map, fetching once per page load. */
export function getFeatureFlags(): Promise<FeatureFlags> {
  if (cached) return Promise.resolve(cached);
  if (!inflight) {
    inflight = fetchFlags().then((f) => {
      cached = f;
      return f;
    });
  }
  return inflight;
}

/**
 * Hook: the effective flag map plus a `loaded` marker. Until loaded,
 * callers see the parked-off defaults — gate UIs should render nothing
 * (not a 404) while `loaded` is false to avoid flashing.
 */
export function useFeatureFlags(): { flags: FeatureFlags; loaded: boolean } {
  const [state, setState] = useState<{ flags: FeatureFlags; loaded: boolean }>(
    () => ({ flags: cached ?? DEFAULT_FLAGS, loaded: cached !== null }),
  );

  useEffect(() => {
    if (cached) return;
    let alive = true;
    getFeatureFlags().then((f) => {
      if (alive) setState({ flags: f, loaded: true });
    });
    return () => {
      alive = false;
    };
  }, []);

  return state;
}

/** Hook: one flag. `enabled` is false until loaded resolves it true. */
export function useFeatureFlag(name: FeatureFlagName): { enabled: boolean; loaded: boolean } {
  const { flags, loaded } = useFeatureFlags();
  return { enabled: flags[name], loaded };
}
