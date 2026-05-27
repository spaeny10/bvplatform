// Visual + lookup constants extracted from ActiveAlarmView.tsx as part of
// P1-B-11 (Phase-1 UI decomposition). These were inline at the top of
// ActiveAlarmView; moving them out reduces that file's line count and
// makes them reusable from sibling components (e.g. the embedded
// AlarmVideoFeed and AIFeedbackButtons sub-components we also extracted).

import type { DispositionCode } from '@/stores/operator-store';

// Background gradients used by AlarmVideoFeed when no snapshot/clip URL is
// available — gives the video tile a non-uniform "looks like a real scene"
// fill instead of solid black.
export const BG_SCENES = [
  'linear-gradient(180deg, #0a0f08 0%, #141a0c 40%, #1a200e 100%)',
  'linear-gradient(160deg, #0a1520, #151f10 40%, #0d1818)',
  'linear-gradient(200deg, #0f1510, #1a1208 60%, #0c1015)',
  'linear-gradient(140deg, #08121a, #12100a 50%, #0a1510)',
];

export const DISPOSITION_ICONS: Record<DispositionCode, string> = {
  false_positive_animal: '🐾',
  false_positive_weather: '🌩️',
  false_positive_shadow: '💡',
  false_positive_equipment: '⚙️',
  false_positive_other: '❓',
  verified_customer_notified: '📞',
  verified_police_dispatched: '🚔',
  verified_guard_responded: '🛡️',
  verified_no_threat: '✅',
  verified_other: '⚠️',
};

export const DISPOSITION_SHORT: Record<DispositionCode, string> = {
  false_positive_animal: 'Animal',
  false_positive_weather: 'Weather',
  false_positive_shadow: 'Shadow / Light',
  false_positive_equipment: 'Equipment',
  false_positive_other: 'Other',
  verified_customer_notified: 'Customer Notified',
  verified_police_dispatched: 'Police Dispatched',
  verified_guard_responded: 'Guard Responded',
  verified_no_threat: 'No Active Threat',
  verified_other: 'Other',
};
