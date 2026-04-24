// ── Feature Flags ──
// Controls which premium features are available per customer/site.
// In production, these come from the backend with the auth token.
// For development, defaults are set here.

export interface FeatureFlags {
  vlm_safety: boolean;         // vLM-powered safety auditing (PPE, hazards)
  semantic_search: boolean;    // Natural language video search
  evidence_sharing: boolean;   // Shareable evidence links with expiry
  global_ai_training: boolean; // Opt-in to global model training from corrections
}

const DEFAULT_FLAGS: FeatureFlags = {
  vlm_safety: true,
  semantic_search: true,
  evidence_sharing: true,
  global_ai_training: true,
};

// In production: fetched from /api/v1/features?site_id=...
// For now, read from localStorage or return defaults
export function getFeatureFlags(siteId?: string): FeatureFlags {
  if (typeof window === 'undefined') return DEFAULT_FLAGS;
  try {
    const stored = localStorage.getItem('ironsight-features');
    if (stored) return { ...DEFAULT_FLAGS, ...JSON.parse(stored) };
  } catch { /* use defaults */ }
  return DEFAULT_FLAGS;
}

export function setFeatureFlag(flag: keyof FeatureFlags, value: boolean) {
  const current = getFeatureFlags();
  current[flag] = value;
  localStorage.setItem('ironsight-features', JSON.stringify(current));
}

// Hook for components
import { useState, useEffect } from 'react';

export function useFeatureFlags(siteId?: string) {
  const [flags, setFlags] = useState<FeatureFlags>(DEFAULT_FLAGS);

  useEffect(() => {
    setFlags(getFeatureFlags(siteId));
  }, [siteId]);

  const toggle = (flag: keyof FeatureFlags) => {
    setFeatureFlag(flag, !flags[flag]);
    setFlags(prev => ({ ...prev, [flag]: !prev[flag] }));
  };

  return { flags, toggle };
}
