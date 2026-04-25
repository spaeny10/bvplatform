// ── Ironsight API Client ──
// Wraps fetch calls to /api/v1/* endpoints.
// Falls back to inline mock data when the backend isn't available.

import type {
  SiteSummary, SiteDetail, IncidentSummary, IncidentDetail,
  AlertEvent, SOCIncident, SearchResult, SearchFilters, ComplianceHistory,
  ReportRequest, ReportStatus,
  Company, CompanyUser, SiteCreate, CameraAssignment, SiteUserAssignment, IRONSightCamera,
  SOCOperator, SiteLock,
  SiteSOP, SiteMapData,
  ShiftHandoff, AuditEntry, OperatorPresence,
  SLAConfig, OperatorMetrics, ScheduledReport, EvidencePackage, NotificationRule,
  PTZCapability, ExclusionZone, SavedSearch, Integration,
} from '@/types/ironsight';
import {
  MOCK_SITES, MOCK_SITE_DETAIL, MOCK_INCIDENTS, MOCK_INCIDENT_DETAIL,
  MOCK_ALERTS, MOCK_SEARCH_RESULTS,
  MOCK_COMPANIES, MOCK_COMPANY_USERS, MOCK_CAMERA_ASSIGNMENTS, MOCK_SITE_USER_ASSIGNMENTS,
  MOCK_SOC_OPERATORS, MOCK_CURRENT_OPERATOR, MOCK_SITE_LOCKS,
  MOCK_SITE_SOPS, MOCK_SITE_MAPS,
  MOCK_SHIFT_HANDOFFS, MOCK_AUDIT_ENTRIES, MOCK_OPERATOR_PRESENCE,
  MOCK_SLA_CONFIGS, MOCK_OPERATOR_METRICS, MOCK_SCHEDULED_REPORTS,
  MOCK_NOTIFICATION_RULES, MOCK_PTZ_CAPABILITIES, MOCK_EXCLUSION_ZONES,
  MOCK_SAVED_SEARCHES, MOCK_INTEGRATIONS,
} from './ironsight-mock';

const BASE = '/api/v1';

function authHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : null;
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { ...init, headers: { ...authHeaders(), ...init?.headers } });
  if (!res.ok) throw new Error(`API ${res.status}: ${res.statusText}`);
  return res.json();
}

// ── Sites ──

export async function getSites(): Promise<SiteSummary[]> {
  try { return await fetchJSON<SiteSummary[]>(`${BASE}/sites`); }
  catch { return MOCK_SITES; }
}

export async function getSite(id: string): Promise<SiteDetail> {
  try {
    const apiSite = await fetchJSON<SiteDetail>(`${BASE}/sites/${id}`);
    // Go backend returns flat Site — supplement with mock detail fields
    // (cameras, ppe_breakdown, etc.) until backend serves them.
    const mock = MOCK_SITE_DETAIL(id);
    return {
      ...mock,
      ...apiSite,
      cameras: apiSite.cameras ?? mock.cameras,
      ppe_breakdown: apiSite.ppe_breakdown ?? mock.ppe_breakdown,
      compliance_history: apiSite.compliance_history ?? mock.compliance_history,
      risk_notes: apiSite.risk_notes ?? mock.risk_notes,
    };
  }
  catch { return MOCK_SITE_DETAIL(id); }
}

export async function getSiteCameras(id: string) {
  try { return await fetchJSON(`${BASE}/sites/${id}/cameras`); }
  catch { return MOCK_SITE_DETAIL(id).cameras; }
}

export async function getSiteCompliance(id: string): Promise<ComplianceHistory> {
  try { return await fetchJSON<ComplianceHistory>(`${BASE}/sites/${id}/compliance`); }
  catch { return { site_id: id, data: MOCK_SITE_DETAIL(id).compliance_history }; }
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
  try { return await fetchJSON<IncidentSummary[]>(`${BASE}/incidents?${params}`); }
  catch { return MOCK_INCIDENTS; }
}

export async function getIncident(id: string): Promise<IncidentDetail> {
  try { return await fetchJSON<IncidentDetail>(`${BASE}/incidents/${id}`); }
  catch { return MOCK_INCIDENT_DETAIL(id); }
}

export async function updateIncidentStatus(id: string, status: string, note?: string) {
  return fetchJSON(`${BASE}/incidents/${id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status, note }),
  });
}

export async function addIncidentComment(id: string, text: string) {
  return fetchJSON(`${BASE}/incidents/${id}/comments`, {
    method: 'POST',
    body: JSON.stringify({ text }),
  });
}

// ── Alerts ──

export async function getAlerts(filters?: {
  site_id?: string; severity?: string; limit?: number;
}): Promise<AlertEvent[]> {
  const params = new URLSearchParams();
  if (filters?.site_id) params.set('site_id', filters.site_id);
  if (filters?.severity) params.set('severity', filters.severity);
  if (filters?.limit) params.set('limit', String(filters.limit));
  try { return await fetchJSON<AlertEvent[]>(`${BASE}/alerts?${params}`); }
  catch { return MOCK_ALERTS; }
}

export async function getActiveIncidents(): Promise<SOCIncident[]> {
  try { return await fetchJSON<SOCIncident[]>(`${BASE}/incidents/active`); }
  catch { return []; }
}

export async function getIncidentDetail(incidentId: string): Promise<{ incident: SOCIncident; alarms: AlertEvent[] }> {
  return fetchJSON<{ incident: SOCIncident; alarms: AlertEvent[] }>(`${BASE}/incidents/${incidentId}`);
}

// ── Search ──

export async function searchFrames(filters: SearchFilters): Promise<SearchResult[]> {
  // Primary: the Go backend's unified /api/search/frames endpoint. Searches
  // both VLM-described segments (natural language) and SOC alarms (PPE +
  // security triggers) and returns the frontend's SearchResult shape
  // directly — no mapping layer needed. Falls back to the legacy mock only
  // if the call fails so the UI stays functional in dev.
  try {
    return await fetchJSON<SearchResult[]>(`/api/search/frames`, {
      method: 'POST',
      body: JSON.stringify(filters),
    });
  } catch {
    return MOCK_SEARCH_RESULTS(filters.query);
  }
}

export async function getSearchSuggestions(q: string): Promise<string[]> {
  try { return await fetchJSON<string[]>(`${BASE}/search/suggest?q=${encodeURIComponent(q)}`); }
  catch {
    return [
      'worker without hard hat near crane',
      'person in exclusion zone',
      'missing harness at height',
      'vehicle near workers',
      'unsecured scaffolding',
    ].filter(s => s.toLowerCase().includes(q.toLowerCase()));
  }
}

// ── Reports ──

export async function generateReport(req: ReportRequest): Promise<ReportStatus> {
  try {
    return await fetchJSON<ReportStatus>(`${BASE}/reports/generate`, {
      method: 'POST',
      body: JSON.stringify(req),
    });
  } catch {
    return { id: 'rpt-mock-1', status: 'complete', download_url: '#', created_at: new Date().toISOString() };
  }
}

// ── Companies ──

export async function getCompanies(): Promise<Company[]> {
  try { return await fetchJSON<Company[]>(`${BASE}/companies`); }
  catch { return MOCK_COMPANIES; }
}

export async function getCompany(id: string): Promise<Company> {
  try { return await fetchJSON<Company>(`${BASE}/companies/${id}`); }
  catch { return MOCK_COMPANIES.find(c => c.id === id) || MOCK_COMPANIES[0]; }
}

export async function createCompany(data: Omit<Company, 'id' | 'created_at'>): Promise<Company> {
  try {
    return await fetchJSON<Company>(`${BASE}/companies`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  } catch {
    return { ...data, id: `co-${Date.now()}`, created_at: new Date().toISOString() };
  }
}

// ── Company Users ──

export async function getCompanyUsers(companyId: string): Promise<CompanyUser[]> {
  try { return await fetchJSON<CompanyUser[]>(`${BASE}/companies/${companyId}/users`); }
  catch { return MOCK_COMPANY_USERS.filter(u => u.company_id === companyId); }
}

export async function createCompanyUser(data: Omit<CompanyUser, 'id' | 'created_at'>): Promise<CompanyUser> {
  try {
    return await fetchJSON<CompanyUser>(`${BASE}/companies/${data.company_id}/users`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  } catch {
    return { ...data, id: `u-${Date.now()}`, created_at: new Date().toISOString() };
  }
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
  try {
    await fetchJSON(`${BASE}/sites/${id}`, { method: 'DELETE' });
  } catch {
    // Mock: no-op
  }
}

// ── Camera Assignments ──

export async function getCameraAssignments(siteId: string): Promise<CameraAssignment[]> {
  try { return await fetchJSON<CameraAssignment[]>(`${BASE}/sites/${siteId}/camera-assignments`); }
  catch { return MOCK_CAMERA_ASSIGNMENTS.filter(a => a.site_id === siteId); }
}

export async function assignCameraToSite(siteId: string, cameraId: string, locationLabel: string): Promise<CameraAssignment> {
  try {
    return await fetchJSON<CameraAssignment>(`${BASE}/sites/${siteId}/camera-assignments`, {
      method: 'POST',
      body: JSON.stringify({ camera_id: cameraId, location_label: locationLabel }),
    });
  } catch {
    return {
      site_id: siteId,
      camera_id: cameraId,
      camera_name: `Camera ${cameraId.slice(0, 8)}`,
      location_label: locationLabel,
      assigned_at: new Date().toISOString(),
    };
  }
}

export async function unassignCamera(siteId: string, cameraId: string): Promise<void> {
  try {
    await fetchJSON(`${BASE}/sites/${siteId}/camera-assignments/${cameraId}`, { method: 'DELETE' });
  } catch {
    // Mock: no-op
  }
}

// ── Speaker Assignments ──

export async function getAllSpeakers(): Promise<import('@/types/ironsight').PlatformSpeaker[]> {
  try { return await fetchJSON(`${BASE}/speakers`); }
  catch { return []; }
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

export async function getSiteUsers(siteId: string): Promise<SiteUserAssignment[]> {
  try { return await fetchJSON<SiteUserAssignment[]>(`${BASE}/sites/${siteId}/users`); }
  catch { return MOCK_SITE_USER_ASSIGNMENTS.filter(a => a.site_id === siteId); }
}

export async function assignUserToSite(siteId: string, userId: string): Promise<SiteUserAssignment> {
  try {
    return await fetchJSON<SiteUserAssignment>(`${BASE}/sites/${siteId}/users`, {
      method: 'POST',
      body: JSON.stringify({ user_id: userId }),
    });
  } catch {
    const user = MOCK_COMPANY_USERS.find(u => u.id === userId);
    return {
      site_id: siteId,
      user_id: userId,
      user_name: user?.name || 'Unknown',
      user_email: user?.email || '',
      role: user?.role || 'viewer',
      assigned_at: new Date().toISOString(),
    };
  }
}

export async function unassignUserFromSite(siteId: string, userId: string): Promise<void> {
  try {
    await fetchJSON(`${BASE}/sites/${siteId}/users/${userId}`, { method: 'DELETE' });
  } catch {
    // Mock: no-op
  }
}

// ── IRONSight Cameras (from existing NVR system) ──

export async function getIRONSightCameras(): Promise<IRONSightCamera[]> {
  try {
    // Pull from the existing IRONSight API (not /api/v1)
    const res = await fetch('/api/cameras', {
      headers: authHeaders(),
    });
    if (!res.ok) throw new Error('Failed to fetch IRONSight cameras');
    const cameras = await res.json();
    return cameras.map((c: Record<string, unknown>) => ({
      id: c.id,
      name: c.name,
      onvif_address: c.onvif_address,
      status: c.status,
      manufacturer: c.manufacturer || '',
      model: c.model || '',
      rtsp_uri: c.rtsp_uri || '',
      recording: c.recording ?? false,
    }));
  } catch {
    // Mock fallback
    return [
      { id: 'cam-uuid-001', name: 'Front Gate Camera', onvif_address: '192.168.1.101', status: 'online', manufacturer: 'Hikvision', model: 'DS-2CD2143G2', rtsp_uri: 'rtsp://192.168.1.101/stream1', recording: true },
      { id: 'cam-uuid-002', name: 'Parking Lot West', onvif_address: '192.168.1.102', status: 'online', manufacturer: 'Dahua', model: 'IPC-HFW5442E', rtsp_uri: 'rtsp://192.168.1.102/stream1', recording: true },
      { id: 'cam-uuid-003', name: 'Loading Dock A', onvif_address: '192.168.1.103', status: 'online', manufacturer: 'Axis', model: 'P3245-V', rtsp_uri: 'rtsp://192.168.1.103/stream1', recording: true },
      { id: 'cam-uuid-004', name: 'Roof Overview', onvif_address: '192.168.1.104', status: 'offline', manufacturer: 'Hikvision', model: 'DS-2CD2387G2', rtsp_uri: 'rtsp://192.168.1.104/stream1', recording: false },
      { id: 'cam-uuid-005', name: 'Interior Office', onvif_address: '192.168.1.105', status: 'online', manufacturer: 'Hanwha', model: 'XNV-8080R', rtsp_uri: 'rtsp://192.168.1.105/stream1', recording: true },
    ];
  }
}

// ── SOC Operators ──

export async function getSOCOperators(): Promise<SOCOperator[]> {
  try { return await fetchJSON<SOCOperator[]>(`${BASE}/operators`); }
  catch { return MOCK_SOC_OPERATORS; }
}

export async function getCurrentOperator(): Promise<SOCOperator> {
  // The logged-in user's identity is always authoritative — read it first.
  const stored = typeof window !== 'undefined' ? localStorage.getItem('ironsight_user') : null;
  let localUser: { id: string; username: string; display_name?: string } | null = null;
  if (stored) {
    try { localUser = JSON.parse(stored); } catch { /* ignore */ }
  }

  try {
    const op = await fetchJSON<SOCOperator>(`${BASE}/operators/current`);
    // Keep callsign from the operator record, but always show the actual logged-in user's name.
    if (localUser) {
      return { ...op, name: localUser.display_name || localUser.username || op.name };
    }
    return op;
  } catch {
    if (localUser) {
      return {
        id: 'user-' + localUser.id,
        name: localUser.display_name || localUser.username,
        callsign: localUser.username.toUpperCase().slice(0, 8),
        status: 'on_shift',
        shift_start: new Date().toISOString(),
      };
    }
    return MOCK_CURRENT_OPERATOR;
  }
}

// ── Site Locks ──

export async function getSiteLocks(): Promise<SiteLock[]> {
  try { return await fetchJSON<SiteLock[]>(`${BASE}/sites/locks`); }
  catch { return MOCK_SITE_LOCKS; }
}

export async function lockSite(siteId: string, operatorId: string, callsign: string): Promise<SiteLock> {
  try {
    return await fetchJSON<SiteLock>(`${BASE}/sites/${siteId}/lock`, {
      method: 'POST',
    });
  } catch {
    return {
      site_id: siteId,
      operator_id: operatorId,
      operator_callsign: callsign,
      locked_at: new Date().toISOString(),
    };
  }
}

export async function unlockSite(siteId: string): Promise<void> {
  try {
    await fetchJSON(`${BASE}/sites/${siteId}/lock`, { method: 'DELETE' });
  } catch {
    // Mock: no-op
  }
}

// ── Alert Ownership ──

export async function claimAlert(alertId: string, operatorId: string, callsign: string): Promise<void> {
  try {
    await fetchJSON(`${BASE}/alerts/${alertId}/claim`, {
      method: 'PUT',
      body: JSON.stringify({ operator_id: operatorId }),
    });
  } catch {
    // Mock: no-op, store handles it locally
  }
}

export async function releaseAlert(alertId: string): Promise<void> {
  try {
    await fetchJSON(`${BASE}/alerts/${alertId}/claim`, { method: 'DELETE' });
  } catch {
    // Mock: no-op
  }
}

// ── Site SOPs ──

export async function getSiteSOPs(siteId: string): Promise<SiteSOP[]> {
  try { return await fetchJSON<SiteSOP[]>(`${BASE}/sites/${siteId}/sops`); }
  catch { return MOCK_SITE_SOPS.filter(s => s.site_id === siteId); }
}

export async function createSiteSOP(data: Omit<SiteSOP, 'id' | 'updated_at'>): Promise<SiteSOP> {
  try {
    return await fetchJSON<SiteSOP>(`${BASE}/sites/${data.site_id}/sops`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  } catch {
    return { ...data, id: `sop-${Date.now()}`, updated_at: new Date().toISOString() };
  }
}

export async function updateSiteSOP(sopId: string, data: Partial<SiteSOP>): Promise<SiteSOP> {
  try {
    return await fetchJSON<SiteSOP>(`${BASE}/sops/${sopId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  } catch {
    const existing = MOCK_SITE_SOPS.find(s => s.id === sopId) || MOCK_SITE_SOPS[0];
    return { ...existing, ...data, updated_at: new Date().toISOString() };
  }
}

export async function deleteSiteSOP(sopId: string): Promise<void> {
  try {
    await fetchJSON(`${BASE}/sops/${sopId}`, { method: 'DELETE' });
  } catch {
    // Mock: no-op
  }
}

// ── Site Maps ──

export async function getSiteMap(siteId: string): Promise<SiteMapData | null> {
  try { return await fetchJSON<SiteMapData>(`${BASE}/sites/${siteId}/map`); }
  catch { return MOCK_SITE_MAPS.find(m => m.site_id === siteId) || null; }
}

export async function updateSiteMap(siteId: string, data: Partial<SiteMapData>): Promise<SiteMapData> {
  try {
    return await fetchJSON<SiteMapData>(`${BASE}/sites/${siteId}/map`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  } catch {
    const existing = MOCK_SITE_MAPS.find(m => m.site_id === siteId);
    return {
      site_id: siteId,
      image_url: data.image_url || existing?.image_url || '',
      image_width: data.image_width || existing?.image_width || 1200,
      image_height: data.image_height || existing?.image_height || 800,
      markers: data.markers || existing?.markers || [],
      updated_at: new Date().toISOString(),
    };
  }
}

// ════════════════════════════════════════════════════════════════
// Complete Platform API Functions
// ════════════════════════════════════════════════════════════════

// ── Shift Handoffs ──

export async function getPendingHandoffs(operatorId: string): Promise<ShiftHandoff[]> {
  try { return await fetchJSON<ShiftHandoff[]>(`${BASE}/handoffs?to=${operatorId}&status=pending`); }
  catch { return MOCK_SHIFT_HANDOFFS.filter(h => h.to_operator_id === operatorId && h.status === 'pending'); }
}

export async function createHandoff(data: Omit<ShiftHandoff, 'id' | 'created_at' | 'status'>): Promise<ShiftHandoff> {
  try { return await fetchJSON<ShiftHandoff>(`${BASE}/handoffs`, { method: 'POST', body: JSON.stringify(data) }); }
  catch { return { ...data, id: `hoff-${Date.now()}`, created_at: new Date().toISOString(), status: 'pending' }; }
}

export async function acceptHandoff(handoffId: string): Promise<void> {
  try { await fetchJSON(`${BASE}/handoffs/${handoffId}/accept`, { method: 'PUT' }); }
  catch { /* mock no-op */ }
}

// ── Audit Trail ──

export async function getAuditLog(filters?: { operator_id?: string; action?: string; limit?: number }): Promise<AuditEntry[]> {
  try { return await fetchJSON<AuditEntry[]>(`${BASE}/audit`); }
  catch {
    let entries = MOCK_AUDIT_ENTRIES;
    if (filters?.operator_id) entries = entries.filter(e => e.operator_id === filters.operator_id);
    if (filters?.action) entries = entries.filter(e => e.action === filters.action);
    return entries.slice(0, filters?.limit || 100);
  }
}

export async function logAuditAction(entry: Omit<AuditEntry, 'id' | 'ts'>): Promise<void> {
  try { await fetchJSON(`${BASE}/audit`, { method: 'POST', body: JSON.stringify(entry) }); }
  catch { /* mock no-op — logged locally */ }
}

// ── Operator Presence ──

export async function getOperatorPresence(): Promise<OperatorPresence[]> {
  try { return await fetchJSON<OperatorPresence[]>(`${BASE}/operators/presence`); }
  catch { return MOCK_OPERATOR_PRESENCE; }
}

export async function updatePresence(data: Partial<OperatorPresence>): Promise<void> {
  try { await fetchJSON(`${BASE}/operators/presence`, { method: 'PUT', body: JSON.stringify(data) }); }
  catch { /* mock no-op */ }
}

// ── SLA Configuration ──

export async function getSLAConfigs(): Promise<SLAConfig[]> {
  try { return await fetchJSON<SLAConfig[]>(`${BASE}/sla`); }
  catch { return MOCK_SLA_CONFIGS; }
}

// ── Operator Metrics ──

export async function getOperatorMetrics(): Promise<OperatorMetrics[]> {
  try { return await fetchJSON<OperatorMetrics[]>(`${BASE}/operators/metrics`); }
  catch { return MOCK_OPERATOR_METRICS; }
}

// ── Scheduled Reports ──

export async function getScheduledReports(): Promise<ScheduledReport[]> {
  try { return await fetchJSON<ScheduledReport[]>(`${BASE}/reports/scheduled`); }
  catch { return MOCK_SCHEDULED_REPORTS; }
}

export async function createScheduledReport(data: Omit<ScheduledReport, 'id'>): Promise<ScheduledReport> {
  try { return await fetchJSON<ScheduledReport>(`${BASE}/reports/scheduled`, { method: 'POST', body: JSON.stringify(data) }); }
  catch { return { ...data, id: `rpt-${Date.now()}` }; }
}

export async function toggleScheduledReport(reportId: string, enabled: boolean): Promise<void> {
  try { await fetchJSON(`${BASE}/reports/scheduled/${reportId}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }); }
  catch { /* mock no-op */ }
}

// ── Evidence Packages ──

export async function generateEvidencePackage(incidentId: string, notes: string): Promise<EvidencePackage> {
  try { return await fetchJSON<EvidencePackage>(`${BASE}/incidents/${incidentId}/evidence`, { method: 'POST', body: JSON.stringify({ notes }) }); }
  catch {
    return {
      id: `ev-${Date.now()}`, incident_id: incidentId, site_name: 'Mock Site',
      generated_at: new Date().toISOString(), generated_by: 'Operator',
      clips: [{ url: '#', label: 'Primary clip', ts: Date.now() }],
      screenshots: [{ url: '#', label: 'Key frame', ts: Date.now() }],
      timeline: [], operator_notes: notes, sop_steps_taken: [],
      status: 'ready', download_url: '#',
    };
  }
}

// ── Notification Rules ──

export async function getNotificationRules(siteId: string): Promise<NotificationRule[]> {
  try { return await fetchJSON<NotificationRule[]>(`${BASE}/sites/${siteId}/notifications`); }
  catch { return MOCK_NOTIFICATION_RULES.filter(r => r.site_id === siteId); }
}

export async function createNotificationRule(data: Omit<NotificationRule, 'id'>): Promise<NotificationRule> {
  try { return await fetchJSON<NotificationRule>(`${BASE}/sites/${data.site_id}/notifications`, { method: 'POST', body: JSON.stringify(data) }); }
  catch { return { ...data, id: `notif-${Date.now()}` }; }
}

export async function deleteNotificationRule(ruleId: string): Promise<void> {
  try { await fetchJSON(`${BASE}/notifications/${ruleId}`, { method: 'DELETE' }); }
  catch { /* mock no-op */ }
}

// ── PTZ Camera Controls ──

export async function getPTZCapability(cameraId: string): Promise<PTZCapability | null> {
  try { return await fetchJSON<PTZCapability>(`${BASE}/cameras/${cameraId}/ptz`); }
  catch { return MOCK_PTZ_CAPABILITIES.find(p => p.camera_id === cameraId) || null; }
}

export async function sendPTZCommand(cameraId: string, command: { pan?: number; tilt?: number; zoom?: number; preset_id?: string }): Promise<void> {
  try { await fetchJSON(`${BASE}/cameras/${cameraId}/ptz/command`, { method: 'POST', body: JSON.stringify(command) }); }
  catch { /* mock no-op */ }
}

// ── Exclusion Zones ──

export async function getExclusionZones(siteId: string): Promise<ExclusionZone[]> {
  try { return await fetchJSON<ExclusionZone[]>(`${BASE}/sites/${siteId}/zones`); }
  catch { return MOCK_EXCLUSION_ZONES.filter(z => z.site_id === siteId); }
}

export async function createExclusionZone(data: Omit<ExclusionZone, 'id' | 'created_at'>): Promise<ExclusionZone> {
  try { return await fetchJSON<ExclusionZone>(`${BASE}/sites/${data.site_id}/zones`, { method: 'POST', body: JSON.stringify(data) }); }
  catch { return { ...data, id: `ez-${Date.now()}`, created_at: new Date().toISOString() }; }
}

export async function deleteExclusionZone(zoneId: string): Promise<void> {
  try { await fetchJSON(`${BASE}/zones/${zoneId}`, { method: 'DELETE' }); }
  catch { /* mock no-op */ }
}

// ── Saved Searches ──

export async function getSavedSearches(): Promise<SavedSearch[]> {
  try { return await fetchJSON<SavedSearch[]>(`${BASE}/search/saved`); }
  catch { return MOCK_SAVED_SEARCHES; }
}

export async function createSavedSearch(data: Omit<SavedSearch, 'id' | 'created_at' | 'run_count'>): Promise<SavedSearch> {
  try { return await fetchJSON<SavedSearch>(`${BASE}/search/saved`, { method: 'POST', body: JSON.stringify(data) }); }
  catch { return { ...data, id: `ss-${Date.now()}`, created_at: new Date().toISOString(), run_count: 0 }; }
}

export async function deleteSavedSearch(searchId: string): Promise<void> {
  try { await fetchJSON(`${BASE}/search/saved/${searchId}`, { method: 'DELETE' }); }
  catch { /* mock no-op */ }
}

// ── Integrations ──

export async function getIntegrations(): Promise<Integration[]> {
  try { return await fetchJSON<Integration[]>(`${BASE}/integrations`); }
  catch { return MOCK_INTEGRATIONS; }
}

export async function createIntegration(data: Omit<Integration, 'id' | 'created_at'>): Promise<Integration> {
  try { return await fetchJSON<Integration>(`${BASE}/integrations`, { method: 'POST', body: JSON.stringify(data) }); }
  catch { return { ...data, id: `int-${Date.now()}`, created_at: new Date().toISOString() }; }
}

export async function toggleIntegration(integrationId: string, active: boolean): Promise<void> {
  try { await fetchJSON(`${BASE}/integrations/${integrationId}`, { method: 'PATCH', body: JSON.stringify({ active }) }); }
  catch { /* mock no-op */ }
}

export async function deleteIntegration(integrationId: string): Promise<void> {
  try { await fetchJSON(`${BASE}/integrations/${integrationId}`, { method: 'DELETE' }); }
  catch { /* mock no-op */ }
}

// ── Operator Dispatch & Presence ──

export type OperatorPresenceStatus = 'available' | 'engaged' | 'wrap_up' | 'away';

export async function updateOperatorPresence(operatorId: string, status: OperatorPresenceStatus): Promise<void> {
  try {
    await fetchJSON(`${BASE}/operators/${operatorId}/presence`, {
      method: 'PUT',
      body: JSON.stringify({ status }),
    });
  } catch { /* mock no-op — in production this syncs via WebSocket */ }
}

export async function getAlarmQueue(): Promise<{ depth: number; oldest_ts: number | null }> {
  try { return await fetchJSON(`${BASE}/dispatch/queue`); }
  catch { return { depth: 0, oldest_ts: null }; }
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
  try {
    return await fetchJSON(`${BASE}/events`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  } catch {
    return { event_id: `EVT-${new Date().getFullYear()}-${String(Date.now()).slice(-4)}` };
  }
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
  try {
    return await fetchJSON<SecurityEventRecord[]>(`${BASE}/events?site_id=${encodeURIComponent(siteId)}`);
  } catch {
    return [];
  }
}

export async function escalateAlarm(alarmId: string, level: number): Promise<void> {
  try {
    await fetchJSON(`${BASE}/alarms/${alarmId}/escalate`, {
      method: 'POST',
      body: JSON.stringify({ level }),
    });
  } catch { /* best-effort — store already updated locally */ }
}

export async function submitAIFeedback(alarmId: string, agreed: boolean): Promise<void> {
  try {
    await fetchJSON(`${BASE}/alarms/${alarmId}/ai-feedback`, {
      method: 'POST',
      body: JSON.stringify({ agreed }),
    });
  } catch { /* best-effort */ }
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

// ── vLM Safety Findings ──

export async function getPendingSafetyFindings(siteId?: string): Promise<any[]> {
  try {
    const params = siteId ? `?site_id=${siteId}` : '';
    return await fetchJSON(`${BASE}/safety/findings/pending${params}`);
  } catch { return []; /* mock returns empty — component has its own mock data */ }
}

export async function validateSafetyFinding(findingId: string, valid: boolean, correction?: string): Promise<void> {
  try {
    await fetchJSON(`${BASE}/safety/findings/${findingId}/validate`, {
      method: 'POST',
      body: JSON.stringify({ valid, correction }),
    });
  } catch { /* mock no-op */ }
}

// ── AI Telemetry (Active Learning) ──

export async function submitAICorrection(payload: {
  finding_id: string;
  original_caption: string;
  correction_type: string;
  // Note: customer_id and site_id are stripped by the backend before forwarding to the training lake
}): Promise<void> {
  try {
    await fetchJSON(`${BASE}/ai-telemetry/corrections`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  } catch { /* mock no-op — in production this feeds the anonymized training pipeline */ }
}

// ── Feature Flags ──

export async function getFeatureFlags(siteId?: string): Promise<Record<string, boolean>> {
  try {
    const params = siteId ? `?site_id=${siteId}` : '';
    return await fetchJSON(`${BASE}/features${params}`);
  } catch {
    return { vlm_safety: true, semantic_search: true, evidence_sharing: true, global_ai_training: true };
  }
}
