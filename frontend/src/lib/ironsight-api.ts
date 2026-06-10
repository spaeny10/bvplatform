// ── Ironsight Platform API Client ──
//
// Companion to `lib/api.ts`. The split is by domain, not by helper:
//
//   - lib/api.ts          → /api/* — cameras, recording health, VCA rules,
//                           Milesight CGI, semantic search, evidence
//                           export, AI metrics, SD-card status.
//   - lib/ironsight-api.ts → /api/v1/* — sites, incidents, SOC operators,
//                           shift handoffs, SLA reports, evidence sharing,
//                           security events, audit log, integrations.
//
// Both clients ride on the same `authFetch` (JWT injection, 401 → /login).
// Both throw on non-2xx via `fetchJSON`; the wrapping component or React
// Query hook handles the error. Earlier revisions silently fell back to
// inline mock data on any error, which made backend outages invisible to
// the operator — that fallback was removed in P1-B-10. Two surfaces
// (the /analytics page and operator/IncidentTimeline) still import the
// mock module directly; wiring them to the real API is a separate
// follow-up so the mock module isn't deleted yet.

import type {
  SiteSummary, SiteDetail, IncidentSummary, IncidentDetail,
  AlertEvent, SOCIncident, SearchResult, SearchFilters, ComplianceHistory,
  Company, CompanyUser, SiteCreate, CameraAssignment,
  SOCOperator, SiteLock,
  SiteSOP, SiteMapData,
  ShiftHandoff, OperatorPresence,
  OperatorMetrics, ScheduledReport, NotificationRule,
  ExclusionZone, SavedSearch, Integration,
  PortalSummary,
} from '@/types/ironsight';
export type { PortalSummary } from '@/types/ironsight';

import { authFetch } from './api';

const BASE = '/api/v1';

// fetchJSON is the authenticated JSON helper used by every function
// in this file. Sits on top of `authFetch` (the single auth-header
// layer in /lib/api.ts) so JWT injection and 401 → /login redirect
// happen in one place. Exported so portal pages that need a one-off
// endpoint can use it directly without duplicating header plumbing.
//
// Non-2xx responses become thrown errors; the wrapping React Query
// hook / component error boundary turns those into a visible failure
// state. Pre-P1-B-10 every consumer wrapped the call in `try/catch`
// and silently swapped in mock data — meaning a backend outage was
// invisible to the operator. That pattern is gone.
export async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers ?? {});
  if (!headers.has('Content-Type') && init?.body) {
    headers.set('Content-Type', 'application/json');
  }
  const res = await authFetch(url, { ...init, headers });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(body || `API ${res.status}: ${res.statusText}`);
  }
  // 204 No Content (PUT/DELETE) — the body is empty, return undefined cast.
  if (res.status === 204) return undefined as unknown as T;
  return res.json();
}

// ── Sites ──

export async function getSites(): Promise<SiteSummary[]> {
  return fetchJSON<SiteSummary[]>(`${BASE}/sites`);
}

export async function getSite(id: string): Promise<SiteDetail> {
  return fetchJSON<SiteDetail>(`${BASE}/sites/${id}`);
}

export async function getSiteCameras(id: string) {
  return fetchJSON(`${BASE}/sites/${id}/cameras`);
}

export async function getSiteCompliance(id: string): Promise<ComplianceHistory> {
  return fetchJSON<ComplianceHistory>(`${BASE}/sites/${id}/compliance`);
}

// ── Incidents ──

export async function getIncidents(filters?: {
  site_id?: string; status?: string; severity?: string; limit?: number;
}): Promise<IncidentSummary[]> {
  const params = new URLSearchParams();
  if (filters?.site_id) params.set('site_id', filters.site_id);
  if (filters?.status) params.set('status', filters.status);
  if (filters?.severity) params.set('severity', filters.severity);
  if (filters?.limit) params.set('limit', String(filters.limit));
  return fetchJSON<IncidentSummary[]>(`${BASE}/incidents?${params}`);
}

export async function getIncident(id: string): Promise<IncidentDetail> {
  return fetchJSON<IncidentDetail>(`${BASE}/incidents/${id}`);
}

// F-09: updateIncidentStatus / addIncidentComment removed — they targeted
// PUT /api/v1/incidents/{id}/status and POST /api/v1/incidents/{id}/comments,
// routes that never existed server-side. The portal incident page no longer
// offers status/comment writes; SOC-side dispositions happen through the
// operator console. Re-add together with real backend routes if customer
// incident write-back ever becomes a feature.

// ── Portal proof-of-work rollup ──

export async function getPortalSummary(days: number): Promise<PortalSummary> {
  return fetchJSON<PortalSummary>(`${BASE}/portal/summary?days=${days}`);
}

// ── Alerts ──

export async function getAlerts(filters?: {
  site_id?: string; severity?: string; limit?: number;
}): Promise<AlertEvent[]> {
  const params = new URLSearchParams();
  if (filters?.site_id) params.set('site_id', filters.site_id);
  if (filters?.severity) params.set('severity', filters.severity);
  if (filters?.limit) params.set('limit', String(filters.limit));
  return fetchJSON<AlertEvent[]>(`${BASE}/alerts?${params}`);
}

export async function getActiveIncidents(): Promise<SOCIncident[]> {
  return fetchJSON<SOCIncident[]>(`${BASE}/incidents/active`);
}

export async function getIncidentDetail(incidentId: string): Promise<{ incident: SOCIncident; alarms: AlertEvent[] }> {
  return fetchJSON<{ incident: SOCIncident; alarms: AlertEvent[] }>(`${BASE}/incidents/${incidentId}`);
}

// ── Search ──

export async function searchFrames(filters: SearchFilters): Promise<SearchResult[]> {
  return fetchJSON<SearchResult[]>(`/api/search/frames`, {
    method: 'POST',
    body: JSON.stringify(filters),
  });
}

// Hardcoded suggestion list is a sensible UX default, not a backend
// mock — keep it as a fallback when the suggest endpoint is offline so
// the typeahead UI still has something to show.
const DEFAULT_SEARCH_SUGGESTIONS = [
  'worker without hard hat near crane',
  'person in exclusion zone',
  'missing harness at height',
  'vehicle near workers',
  'unsecured scaffolding',
];

export async function getSearchSuggestions(q: string): Promise<string[]> {
  try {
    return await fetchJSON<string[]>(`${BASE}/search/suggest?q=${encodeURIComponent(q)}`);
  } catch {
    return DEFAULT_SEARCH_SUGGESTIONS.filter(s => s.toLowerCase().includes(q.toLowerCase()));
  }
}

// ── Companies ──

export async function getCompanies(): Promise<Company[]> {
  return fetchJSON<Company[]>(`${BASE}/companies`);
}

export async function getCompany(id: string): Promise<Company> {
  return fetchJSON<Company>(`${BASE}/companies/${id}`);
}

export async function createCompany(data: Omit<Company, 'id' | 'created_at'>): Promise<Company> {
  return fetchJSON<Company>(`${BASE}/companies`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

// ── Company Users ──

export async function getCompanyUsers(companyId: string): Promise<CompanyUser[]> {
  return fetchJSON<CompanyUser[]>(`${BASE}/companies/${companyId}/users`);
}

// ── Site Management ──

export async function createSite(data: SiteCreate): Promise<SiteSummary> {
  return fetchJSON<SiteSummary>(`${BASE}/sites`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateSite(id: string, data: SiteCreate): Promise<SiteSummary> {
  return fetchJSON<SiteSummary>(`${BASE}/sites/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteSite(id: string): Promise<void> {
  await fetchJSON(`${BASE}/sites/${id}`, { method: 'DELETE' });
}

// ── Camera Assignments ──

export async function getCameraAssignments(siteId: string): Promise<CameraAssignment[]> {
  return fetchJSON<CameraAssignment[]>(`${BASE}/sites/${siteId}/camera-assignments`);
}

export async function assignCameraToSite(siteId: string, cameraId: string, locationLabel: string): Promise<CameraAssignment> {
  return fetchJSON<CameraAssignment>(`${BASE}/sites/${siteId}/camera-assignments`, {
    method: 'POST',
    body: JSON.stringify({ camera_id: cameraId, location_label: locationLabel }),
  });
}

export async function unassignCamera(siteId: string, cameraId: string): Promise<void> {
  await fetchJSON(`${BASE}/sites/${siteId}/camera-assignments/${cameraId}`, { method: 'DELETE' });
}

// ── Speaker Assignments ──

export async function getAllSpeakers(): Promise<import('@/types/ironsight').PlatformSpeaker[]> {
  return fetchJSON(`${BASE}/speakers`);
}

export async function assignSpeakerToSite(siteId: string, speakerId: string, locationLabel: string): Promise<void> {
  await fetchJSON(`${BASE}/sites/${siteId}/speaker-assignments`, {
    method: 'POST',
    body: JSON.stringify({ speaker_id: speakerId, location_label: locationLabel }),
  });
}

export async function unassignSpeaker(siteId: string, speakerId: string): Promise<void> {
  await fetchJSON(`${BASE}/sites/${siteId}/speaker-assignments/${speakerId}`, { method: 'DELETE' });
}

// ── Site User Assignments ──
//
// F-04: getSiteUsers / assignUserToSite / unassignUserFromSite removed —
// they targeted GET/POST/DELETE /api/v1/sites/{id}/users, routes that
// never existed server-side (no handler, table, or migration). The real
// access-scoping mechanism is users.assigned_site_ids, read via
// GET /api/users and edited via PATCH /api/users/{id} (lib/api.ts
// listUsers / updateUserProfile) — CustomerAccessModal now uses that.

// ── SOC Operators ──

export async function getCurrentOperator(): Promise<SOCOperator> {
  // The logged-in user's identity is authoritative — always merge it
  // over what the operator endpoint returns so the display name in the
  // UI matches the actual session, not whatever happens to be in the
  // operators table.
  const stored = typeof window !== 'undefined' ? localStorage.getItem('ironsight_user') : null;
  let localUser: { id: string; username: string; display_name?: string } | null = null;
  if (stored) {
    try { localUser = JSON.parse(stored); } catch { /* ignore */ }
  }

  const op = await fetchJSON<SOCOperator>(`${BASE}/operators/current`);
  if (localUser) {
    return { ...op, name: localUser.display_name || localUser.username || op.name };
  }
  return op;
}

// ── Site Locks ──

export async function getSiteLocks(): Promise<SiteLock[]> {
  return fetchJSON<SiteLock[]>(`${BASE}/sites/locks`);
}

export async function lockSite(siteId: string, _operatorId: string, _callsign: string): Promise<SiteLock> {
  return fetchJSON<SiteLock>(`${BASE}/sites/${siteId}/lock`, { method: 'POST' });
}

export async function unlockSite(siteId: string): Promise<void> {
  await fetchJSON(`${BASE}/sites/${siteId}/lock`, { method: 'DELETE' });
}

// ── Alert Ownership ──

export async function claimAlert(alertId: string, operatorId: string, _callsign: string): Promise<void> {
  await fetchJSON(`${BASE}/alerts/${alertId}/claim`, {
    method: 'PUT',
    body: JSON.stringify({ operator_id: operatorId }),
  });
}

export async function releaseAlert(alertId: string): Promise<void> {
  await fetchJSON(`${BASE}/alerts/${alertId}/claim`, { method: 'DELETE' });
}

// ── Site SOPs ──

export async function getSiteSOPs(siteId: string): Promise<SiteSOP[]> {
  return fetchJSON<SiteSOP[]>(`${BASE}/sites/${siteId}/sops`);
}

export async function createSiteSOP(data: Omit<SiteSOP, 'id' | 'updated_at'>): Promise<SiteSOP> {
  return fetchJSON<SiteSOP>(`${BASE}/sites/${data.site_id}/sops`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateSiteSOP(sopId: string, data: Partial<SiteSOP>): Promise<SiteSOP> {
  return fetchJSON<SiteSOP>(`${BASE}/sops/${sopId}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteSiteSOP(sopId: string): Promise<void> {
  await fetchJSON(`${BASE}/sops/${sopId}`, { method: 'DELETE' });
}

// ── Site Maps ──

export async function getSiteMap(siteId: string): Promise<SiteMapData | null> {
  return fetchJSON<SiteMapData>(`${BASE}/sites/${siteId}/map`);
}

export async function updateSiteMap(siteId: string, data: Partial<SiteMapData>): Promise<SiteMapData> {
  return fetchJSON<SiteMapData>(`${BASE}/sites/${siteId}/map`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

// ════════════════════════════════════════════════════════════════
// Complete Platform API Functions
// ════════════════════════════════════════════════════════════════

// ── Shift Handoffs ──

export async function getPendingHandoffs(operatorId: string): Promise<ShiftHandoff[]> {
  return fetchJSON<ShiftHandoff[]>(`${BASE}/handoffs?to=${operatorId}&status=pending`);
}

export async function createHandoff(data: Omit<ShiftHandoff, 'id' | 'created_at' | 'status'>): Promise<ShiftHandoff> {
  return fetchJSON<ShiftHandoff>(`${BASE}/handoffs`, { method: 'POST', body: JSON.stringify(data) });
}

export async function acceptHandoff(handoffId: string): Promise<void> {
  await fetchJSON(`${BASE}/handoffs/${handoffId}/accept`, { method: 'PUT' });
}

// ── Audit Trail ──

// F-03: getAuditLog/logAuditAction removed — they targeted /api/v1/audit,
// a route that never existed (the real audit API is GET /api/audit via
// lib/api.ts queryAuditLog, which AuditLogPanel/AuditLogExport now use;
// writes happen server-side in AuditMiddleware, never from the client).

// ── Operator Presence ──

export async function getOperatorPresence(): Promise<OperatorPresence[]> {
  return fetchJSON<OperatorPresence[]>(`${BASE}/operators/presence`);
}

export async function updatePresence(data: Partial<OperatorPresence>): Promise<void> {
  await fetchJSON(`${BASE}/operators/presence`, { method: 'PUT', body: JSON.stringify(data) });
}

// ── Operator Metrics ──

export async function getOperatorMetrics(): Promise<OperatorMetrics[]> {
  return fetchJSON<OperatorMetrics[]>(`${BASE}/operators/metrics`);
}

// ── Scheduled Reports ──

export async function getScheduledReports(): Promise<ScheduledReport[]> {
  return fetchJSON<ScheduledReport[]>(`${BASE}/reports/scheduled`);
}

export async function toggleScheduledReport(reportId: string, enabled: boolean): Promise<void> {
  await fetchJSON(`${BASE}/reports/scheduled/${reportId}`, { method: 'PATCH', body: JSON.stringify({ enabled }) });
}

// ── Evidence Packages ──
//
// F-07: generateEvidencePackage removed — it POSTed
// /api/v1/incidents/{id}/evidence, a route that never existed, so the
// portal's "Export MP4" button hung forever. EvidenceExportButton now
// downloads the incident's real clip via the signed media-mint pipeline
// (lib/media resolveMediaURL + downloadAuthenticated below); evidence
// share links go through createEvidenceShareLink (real backend).

// ── Notification Rules ──

export async function getNotificationRules(siteId: string): Promise<NotificationRule[]> {
  return fetchJSON<NotificationRule[]>(`${BASE}/sites/${siteId}/notifications`);
}

export async function createNotificationRule(data: Omit<NotificationRule, 'id'>): Promise<NotificationRule> {
  return fetchJSON<NotificationRule>(`${BASE}/sites/${data.site_id}/notifications`, { method: 'POST', body: JSON.stringify(data) });
}

export async function deleteNotificationRule(ruleId: string): Promise<void> {
  await fetchJSON(`${BASE}/notifications/${ruleId}`, { method: 'DELETE' });
}

// ── Exclusion Zones ──

export async function getExclusionZones(siteId: string): Promise<ExclusionZone[]> {
  return fetchJSON<ExclusionZone[]>(`${BASE}/sites/${siteId}/zones`);
}

// ── Saved Searches ──
//
// F-24: /api/v1/search/saved was never implemented server-side (every
// call 404'd and "save" silently no-opped). Saved searches are now
// persisted per-browser in localStorage and labeled as local in the
// UI. If cross-device saved searches ever become a real need, add the
// backend table + routes and swap these implementations back to
// fetchJSON — the call sites won't change.

const SAVED_SEARCHES_KEY = 'ironsight_saved_searches';

function readSavedSearches(): SavedSearch[] {
  if (typeof window === 'undefined') return [];
  try {
    return JSON.parse(localStorage.getItem(SAVED_SEARCHES_KEY) ?? '[]') as SavedSearch[];
  } catch {
    return [];
  }
}

export async function getSavedSearches(): Promise<SavedSearch[]> {
  return readSavedSearches();
}

export async function createSavedSearch(data: Omit<SavedSearch, 'id' | 'created_at' | 'run_count'>): Promise<SavedSearch> {
  const entry: SavedSearch = {
    ...data,
    id: `local-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
    created_at: new Date().toISOString(),
    run_count: 0,
  } as SavedSearch;
  const all = [entry, ...readSavedSearches()];
  localStorage.setItem(SAVED_SEARCHES_KEY, JSON.stringify(all));
  return entry;
}

export async function deleteSavedSearch(searchId: string): Promise<void> {
  const all = readSavedSearches().filter(s => s.id !== searchId);
  localStorage.setItem(SAVED_SEARCHES_KEY, JSON.stringify(all));
}

// ── Integrations ──

export async function getIntegrations(): Promise<Integration[]> {
  return fetchJSON<Integration[]>(`${BASE}/integrations`);
}

export async function toggleIntegration(integrationId: string, active: boolean): Promise<void> {
  await fetchJSON(`${BASE}/integrations/${integrationId}`, { method: 'PATCH', body: JSON.stringify({ active }) });
}

export async function deleteIntegration(integrationId: string): Promise<void> {
  await fetchJSON(`${BASE}/integrations/${integrationId}`, { method: 'DELETE' });
}

// ── Security Events ──

/**
 * TMA-AVS-01 validation factors captured by the operator at
 * disposition. The backend computes the 0–4 score from these flags
 * deterministically — clients never supply a score directly. See the
 * Go side at internal/avs/scoring.go for the canonical rubric. Every
 * field is intentionally a discrete yes/no observation grounded in
 * specific evidence ("I saw / heard / verified X"). Auditors can
 * reconstruct the operator's mental state from the row alone.
 */
export interface AVSFactors {
  video_verified: boolean;
  person_detected: boolean;
  suspicious_behavior: boolean;
  weapon_observed: boolean;
  active_crime: boolean;
  multi_camera_evidence: boolean;
  multi_sensor_evidence: boolean;
  audio_verified: boolean;
  talkdown_ignored: boolean;
  auth_failure: boolean;
  ai_corroborated: boolean;
}

export interface SecurityEventPayload {
  alarm_id: string;
  site_id: string;
  camera_id: string;
  disposition_code: string;
  disposition_label?: string;
  operator_notes: string;
  action_log: Array<{ ts: number; text: string; auto?: boolean }>;
  escalation_depth: number;      // how many call tree contacts were reached
  clip_bookmark_id?: string;     // NVR bookmark reference
  severity?: string;
  type?: string;
  description?: string;
  operator_callsign?: string;
  avs_factors?: AVSFactors;      // TMA-AVS-01 validation attestations
}

/**
 * Mirror of internal/avs/scoring.go ComputeScore. Kept on the
 * frontend so the disposition UI can show a live preview of the score
 * the backend will record. The backend value is still authoritative
 * — never trust this for any persisted decision.
 */
export function previewAVSScore(f: Partial<AVSFactors>): { score: 0|1|2|3|4; label: string; dispatch: boolean } {
  if (!f.video_verified) return { score: 0, label: 'UNVERIFIED', dispatch: false };
  if (f.weapon_observed || f.active_crime) return { score: 4, label: 'CRITICAL', dispatch: true };
  const corroborated = !!(f.suspicious_behavior || f.multi_camera_evidence ||
    f.multi_sensor_evidence || f.audio_verified || f.talkdown_ignored || f.auth_failure);
  if (corroborated) return { score: 3, label: 'ELEVATED', dispatch: true };
  if (f.person_detected) return { score: 2, label: 'VERIFIED', dispatch: true };
  return { score: 1, label: 'MINIMAL', dispatch: false };
}

export async function createSecurityEvent(payload: SecurityEventPayload): Promise<{ event_id: string }> {
  return fetchJSON(`${BASE}/events`, {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export interface SecurityEventRecord {
  id: string;
  alarm_id: string;
  site_id: string;
  camera_id: string;
  severity: string;
  type: string;
  description: string;
  disposition_code: string;
  disposition_label: string;
  operator_callsign: string;
  operator_notes: string;
  action_log: { ts: number; text: string; auto?: boolean }[];
  escalation_depth: number;
  clip_url: string;
  ts: number;
  resolved_at: number;
}

export async function listSecurityEvents(siteId: string): Promise<SecurityEventRecord[]> {
  return fetchJSON<SecurityEventRecord[]>(`${BASE}/events?site_id=${encodeURIComponent(siteId)}`);
}

export async function escalateAlarm(alarmId: string, level: number): Promise<void> {
  await fetchJSON(`${BASE}/alarms/${alarmId}/escalate`, {
    method: 'POST',
    body: JSON.stringify({ level }),
  });
}

export async function submitAIFeedback(alarmId: string, agreed: boolean): Promise<void> {
  await fetchJSON(`${BASE}/alarms/${alarmId}/ai-feedback`, {
    method: 'POST',
    body: JSON.stringify({ agreed }),
  });
}

// ── Reports & supervisor dashboards ──
//
// These endpoints feed the /reports page (admin + soc_supervisor
// access). All require auth; the backend enforces RBAC on the data
// surfaces (SLA report aggregates ALL alarms — visible to SOC roles
// only; verification queue is global; evidence shares are scoped to
// the calling user's organization unless they're a SOC role).

export interface SLAReportRow {
  bucket: string;
  total_alarms: number;
  acked_alarms: number;
  within_sla: number;
  over_sla: number;
  avg_ack_sec: number;
  p50_ack_sec: number;
  p95_ack_sec: number;
}

export interface SLAReportResponse {
  from: string;
  to: string;
  group: 'operator' | 'day';
  rows: SLAReportRow[];
}

export async function getSLAReport(params: {
  from?: string;        // RFC3339; default = -30 days
  to?: string;          // RFC3339; default = now
  group?: 'operator' | 'day';
}): Promise<SLAReportResponse> {
  const qs = new URLSearchParams();
  if (params.from)  qs.set('from', params.from);
  if (params.to)    qs.set('to', params.to);
  if (params.group) qs.set('group', params.group);
  // F-01: HandleSLAReport is registered under /api (router.go), not /api/v1.
  return fetchJSON(`/api/reports/sla?${qs.toString()}`);
}

export function slaReportCsvUrl(params: {
  from?: string;
  to?: string;
  group?: 'operator' | 'day';
}): string {
  const qs = new URLSearchParams({ format: 'csv' });
  if (params.from)  qs.set('from', params.from);
  if (params.to)    qs.set('to', params.to);
  if (params.group) qs.set('group', params.group);
  // F-01: same /api (not /api/v1) registration as getSLAReport above.
  return `/api/reports/sla?${qs.toString()}`;
}

/**
 * Authenticated download — for endpoints that emit text/csv with
 * Content-Disposition: attachment. The browser's anchor-tag trick
 * won't work because the API requires a Bearer token; we fetch the
 * blob, build a temporary object URL, and trigger the download in
 * one shot. Works for all our CSV-export endpoints.
 */
export async function downloadAuthenticated(url: string, filename: string): Promise<void> {
  const res = await authFetch(url);
  if (!res.ok) throw new Error(`download failed: ${res.status}`);
  const blob = await res.blob();
  const objectUrl = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = objectUrl;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(objectUrl);
}

// Verification queue — high-severity dispositioned events that haven't
// been signed off by a second supervisor yet. Drives the four-eyes
// follow-through dashboard. Backend filters via the partial index
// idx_security_events_unverified_high.
export interface UnverifiedEvent {
  id: string;
  alarm_id: string;
  site_id: string;
  camera_id: string;
  severity: string;
  type: string;
  description: string;
  disposition_code: string;
  disposition_label: string;
  operator_callsign: string;
  ts: number;
  resolved_at: number;
  avs_score?: number;
  avs_rubric_version?: string;
  disposed_by_user_id?: string | null;
}

export async function listUnverifiedSecurityEvents(): Promise<UnverifiedEvent[]> {
  // Backend doesn't have a dedicated endpoint yet — we fetch the full
  // list and filter client-side. Acceptable while the dataset is
  // small; revisit when active-event volume forces pagination.
  // Empty site_id intentionally — the backend interprets that as
  // "across all sites the caller is authorized to see," which is
  // exactly the supervisor scope.
  const all = await listSecurityEvents('');
  return (all as any[]).filter(e =>
    (e.severity === 'critical' || e.severity === 'high') && !e.verified_at,
  ) as UnverifiedEvent[];
}

export async function verifySecurityEvent(eventId: string): Promise<void> {
  await fetchJSON(`${BASE}/events/${encodeURIComponent(eventId)}/verify`, { method: 'POST' });
}

// Evidence-share management — list / revoke. Used by the supervisor's
// "Manage shares" view to see active tokens with their open counts
// and revoke ones that are no longer needed.
export interface EvidenceShareWithStats {
  token: string;
  incident_id: string;
  created_by: string;
  expires_at: string | null;
  revoked: boolean;
  created_at: string;
  open_count: number;
  active: boolean;
}

export async function listIncidentShares(incidentId: string): Promise<EvidenceShareWithStats[]> {
  return fetchJSON(`${BASE}/incidents/${encodeURIComponent(incidentId)}/shares`);
}

// ── Evidence Sharing ──
//
// Wired against the real backend endpoints registered in
// internal/api/router.go:
//   POST   /api/v1/incidents/{id}/share   create
//   DELETE /api/v1/shares/{token}         revoke
//   GET    /share/{token}                 public read (no auth)
//
// The backend caps expiration at 90 days and defaults to 7 — anything
// longer is rejected at the handler. The "never" UI option is now
// silently mapped to the maximum allowed (90 days) so an operator
// who picks it gets the longest legal window without a confusing
// 400 from the server.

export interface EvidenceShareRequest {
  incident_id: string;
  expires_in: '1h' | '1d' | '1w' | '1m' | 'never';
}

export interface EvidenceShareResponse {
  token: string;
  url: string;
  expires_at: string | null;
}

const SHARE_TTL_HOURS: Record<EvidenceShareRequest['expires_in'], number> = {
  '1h': 1,
  '1d': 24,
  '1w': 24 * 7,
  '1m': 24 * 30,
  'never': 24 * 90, // backend ceiling
};

export async function createEvidenceShareLink(req: EvidenceShareRequest): Promise<EvidenceShareResponse> {
  const hours = SHARE_TTL_HOURS[req.expires_in] ?? 24 * 7;
  const created = await fetchJSON<{
    token: string;
    incident_id: string;
    expires_at: string | null;
  }>(`${BASE}/incidents/${encodeURIComponent(req.incident_id)}/share`, {
    method: 'POST',
    body: JSON.stringify({
      incident_id: req.incident_id,
      expires_in_hours: hours,
    }),
  });
  return {
    token: created.token,
    url: `/share/${created.token}`,
    expires_at: created.expires_at,
  };
}

export async function revokeEvidenceShareLink(token: string): Promise<void> {
  await fetchJSON(`${BASE}/shares/${encodeURIComponent(token)}`, { method: 'DELETE' });
}

// ── Feature Flags ──

// Default feature flags returned when the backend is unreachable.
// These are static and conservative — they grant the standard
// capability set so a transient backend blip doesn't toggle the UI
// into a degraded mode mid-shift.
const DEFAULT_FEATURE_FLAGS: Record<string, boolean> = {
  vlm_safety: true,
  semantic_search: true,
  evidence_sharing: true,
  global_ai_training: true,
};

export async function getFeatureFlags(siteId?: string): Promise<Record<string, boolean>> {
  try {
    const params = siteId ? `?site_id=${siteId}` : '';
    return await fetchJSON(`${BASE}/features${params}`);
  } catch {
    return DEFAULT_FEATURE_FLAGS;
  }
}
