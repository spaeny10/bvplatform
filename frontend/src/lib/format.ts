// ── Ironsight Formatters ──
// All timestamps in Ironsight are Unix epoch milliseconds.

/**
 * Format a Unix ms timestamp to a localized time string (HH:MM:SS)
 */
export function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString('en-US', { hour12: false });
}

/**
 * Format a Unix ms timestamp to a short time (HH:MM)
 */
export function formatTimeShort(ts: number): string {
  const d = new Date(ts);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/**
 * Format a Unix ms timestamp to date + time
 */
export function formatDateTime(ts: number): string {
  const d = new Date(ts);
  return d.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }) + ' ' + formatTime(ts);
}

/**
 * Format a Unix ms timestamp to a relative string ("2m ago", "3h ago", "1d ago")
 */
export function formatRelativeTime(ts: number): string {
  const now = Date.now();
  const diff = now - ts;
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

/**
 * Format a duration in milliseconds to "M:SS" or "H:MM:SS"
 */
export function formatDuration(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
  }
  return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

/**
 * Format a confidence score (0-1) to a percentage string
 */
export function formatConfidence(score: number): string {
  return `${Math.round(score * 100)}%`;
}

/**
 * Format a compliance score (0-100) with color class
 */
export function getComplianceLevel(score: number): 'green' | 'amber' | 'red' {
  if (score >= 90) return 'green';
  if (score >= 75) return 'amber';
  return 'red';
}

/**
 * Format an ISO date to "Mon DD" (e.g. "Apr 15")
 */
export function formatShortDate(isoDate: string): string {
  const d = new Date(isoDate);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}
