// ── Ironsight Mock Data ──
// Rich mock data for all Ironsight interfaces when backend is unavailable.

import type {
  SiteSummary, SiteDetail, IncidentSummary, IncidentDetail,
  AlertEvent, SearchResult, Detection, WorkerPPE, Finding,
  Company, CompanyUser, CameraAssignment, SiteUserAssignment,
  SOCOperator, SiteLock,
  SiteSOP, SiteMapData,
  ShiftHandoff, EscalationRule, AuditEntry, OperatorPresence,
  SLAConfig, OperatorMetrics, ScheduledReport, NotificationRule,
  PTZCapability, ExclusionZone, SavedSearch, Integration,
  MonitoringWindow,
} from '@/types/ironsight';

// ── Default monitoring schedules ──
const SCHEDULE_WEEKNIGHT: MonitoringWindow = { id: 'mw-1', label: 'Weeknights', days: [1,2,3,4,5], start_time: '18:00', end_time: '06:00', enabled: true };
const SCHEDULE_WEEKEND: MonitoringWindow = { id: 'mw-2', label: 'Weekends 24hr', days: [0,6], start_time: '00:00', end_time: '23:59', enabled: true };
const SCHEDULE_247: MonitoringWindow = { id: 'mw-3', label: '24/7', days: [0,1,2,3,4,5,6], start_time: '00:00', end_time: '23:59', enabled: true };

// ── Sites ──

export const MOCK_SITES: SiteSummary[] = [
  { id: 'TX-203', name: 'Southgate Power Station',   status: 'critical', compliance_score: 78, cameras_online: 8,  cameras_total: 8,  open_incidents: 3, workers_on_site: 42, last_activity: new Date(Date.now() - 120000).toISOString(),  trend: 'down', company_id: 'co-turner',  feature_mode: 'security_and_safety', monitoring_schedule: [SCHEDULE_WEEKNIGHT, SCHEDULE_WEEKEND] },
  { id: 'TX-104', name: 'Riverside Bridge Expansion', status: 'active',   compliance_score: 94, cameras_online: 6,  cameras_total: 6,  open_incidents: 0, workers_on_site: 28, last_activity: new Date(Date.now() - 300000).toISOString(),  trend: 'up',   company_id: 'co-turner',  feature_mode: 'security_and_safety', monitoring_schedule: [SCHEDULE_WEEKNIGHT, SCHEDULE_WEEKEND] },
  { id: 'CA-089', name: 'Marina Bay Tower',           status: 'active',   compliance_score: 91, cameras_online: 5,  cameras_total: 5,  open_incidents: 1, workers_on_site: 35, last_activity: new Date(Date.now() - 60000).toISOString(),   trend: 'flat', company_id: 'co-turner',  feature_mode: 'security_only', monitoring_schedule: [SCHEDULE_247] },
  { id: 'FL-312', name: 'Bayshore Medical Center',    status: 'active',   compliance_score: 88, cameras_online: 4,  cameras_total: 5,  open_incidents: 2, workers_on_site: 19, last_activity: new Date(Date.now() - 600000).toISOString(),  trend: 'down', company_id: 'co-bechtel', feature_mode: 'security_only', monitoring_schedule: [SCHEDULE_247] },
  { id: 'NY-445', name: 'Midtown Transit Hub',        status: 'active',   compliance_score: 96, cameras_online: 10, cameras_total: 10, open_incidents: 0, workers_on_site: 64, last_activity: new Date(Date.now() - 45000).toISOString(),   trend: 'up',   company_id: 'co-kiewit',  feature_mode: 'security_and_safety', monitoring_schedule: [SCHEDULE_WEEKNIGHT, SCHEDULE_WEEKEND] },
  { id: 'WA-178', name: 'Puget Sound Data Center',   status: 'idle',     compliance_score: 92, cameras_online: 3,  cameras_total: 4,  open_incidents: 0, workers_on_site: 8,  last_activity: new Date(Date.now() - 7200000).toISOString(), trend: 'flat', company_id: 'co-kiewit',  feature_mode: 'security_only', monitoring_schedule: [SCHEDULE_WEEKNIGHT] },
  { id: 'IL-267', name: "O'Hare Cargo Terminal",     status: 'active',   compliance_score: 85, cameras_online: 7,  cameras_total: 8,  open_incidents: 1, workers_on_site: 31, last_activity: new Date(Date.now() - 180000).toISOString(),  trend: 'up',   company_id: 'co-kiewit',  feature_mode: 'security_and_safety', monitoring_schedule: [SCHEDULE_WEEKNIGHT, SCHEDULE_WEEKEND] },
  { id: 'GA-091', name: 'Atlanta Interchange',       status: 'active',   compliance_score: 82, cameras_online: 6,  cameras_total: 6,  open_incidents: 2, workers_on_site: 48, last_activity: new Date(Date.now() - 90000).toISOString(),   trend: 'down', company_id: 'co-bechtel', feature_mode: 'security_and_safety', monitoring_schedule: [SCHEDULE_WEEKNIGHT, SCHEDULE_WEEKEND] },
];

export function MOCK_SITE_DETAIL(id: string): SiteDetail {
  const site = MOCK_SITES.find(s => s.id === id) || MOCK_SITES[0];
  return {
    ...site,
    address: '4200 Industrial Blvd, Houston, TX 77001',
    latitude: 29.7604,
    longitude: -95.3698,
    cameras: [
      { id: 'cam-01', name: 'North Perimeter', location: 'Gate A', status: 'online', has_alert: false },
      { id: 'cam-02', name: 'Crane Zone A', location: 'Tower Crane #1', status: 'online', has_alert: true },
      { id: 'cam-03', name: 'Excavation Pit', location: 'Foundation Level B2', status: 'online', has_alert: true },
      { id: 'cam-04', name: 'South Loading', location: 'Dock 3', status: 'online', has_alert: false },
      { id: 'cam-05', name: 'Scaffold Tower', location: 'East Wing L4', status: 'online', has_alert: false },
      { id: 'cam-06', name: 'Material Storage', location: 'Yard C', status: 'online', has_alert: false },
    ],
    compliance_history: Array.from({ length: 7 }, (_, i) => ({
      date: new Date(Date.now() - (6 - i) * 86400000).toISOString().slice(0, 10),
      hard_hat: 85 + Math.random() * 12,
      harness: 78 + Math.random() * 15,
      hi_vis: 90 + Math.random() * 8,
      boots: 92 + Math.random() * 6,
      overall: 82 + Math.random() * 14,
    })),
    ppe_breakdown: { hard_hat: 89, harness: 76, hi_vis: 94, boots: 97, gloves: 82 },
    risk_notes: ['Gate 3 latch is broken — stuck open until repair crew arrives Thursday', 'Guard dog on premises after 10PM — do not dispatch to rear lot without alerting guard first', 'Material storage yard has high-value copper inventory — priority monitoring zone'],
  };
}

// ── Incidents ──

export const MOCK_INCIDENTS: IncidentSummary[] = [
  { id: 'INC-2026-0847', severity: 'critical', status: 'open', title: 'Worker without harness at height — Scaffold Tower L4', site_id: 'TX-203', site_name: 'Southgate Power Station', camera_id: 'cam-05', ts: Date.now() - 480000, workers_identified: 1, type: 'no_harness' },
  { id: 'INC-2026-0846', severity: 'high', status: 'in_review', title: 'Exclusion zone breach — Crane Zone A', site_id: 'TX-203', site_name: 'Southgate Power Station', camera_id: 'cam-02', ts: Date.now() - 1200000, workers_identified: 2, type: 'zone_breach' },
  { id: 'INC-2026-0845', severity: 'medium', status: 'open', title: 'Missing hard hat — North Perimeter', site_id: 'TX-203', site_name: 'Southgate Power Station', camera_id: 'cam-01', ts: Date.now() - 3600000, workers_identified: 1, type: 'no_hard_hat' },
  { id: 'INC-2026-0844', severity: 'critical', status: 'resolved', title: 'Vehicle operating near workers without spotter', site_id: 'GA-091', site_name: 'Atlanta Interchange', camera_id: 'cam-03', ts: Date.now() - 7200000, workers_identified: 3, type: 'vehicle_hazard' },
  { id: 'INC-2026-0843', severity: 'high', status: 'open', title: 'Unsecured material at height — Marina Bay Tower', site_id: 'CA-089', site_name: 'Marina Bay Tower', camera_id: 'cam-04', ts: Date.now() - 14400000, workers_identified: 0, type: 'unsecured_material' },
  { id: 'INC-2026-0842', severity: 'medium', status: 'in_review', title: 'Missing hi-vis vest in active zone', site_id: 'FL-312', site_name: 'Bayshore Medical Center', camera_id: 'cam-02', ts: Date.now() - 28800000, workers_identified: 1, type: 'no_hi_vis' },
];

export function MOCK_INCIDENT_DETAIL(id: string): IncidentDetail {
  const inc = MOCK_INCIDENTS.find(i => i.id === id) || MOCK_INCIDENTS[0];
  return {
    ...inc,
    camera_name: 'Scaffold Tower',
    duration_ms: 42000,
    ai_confidence: 0.94,
    ai_caption: 'A worker is observed at approximately 12 meters elevation on the east scaffold structure without a fall-arrest harness. The individual is moving along an unsecured edge near the fourth-floor slab. Hard hat and hi-vis vest are present. Nearest anchor point is approximately 3 meters away but unclipped.',
    findings: [
      { id: 'f1', title: 'No Fall Harness', description: 'Worker at height without fall-arrest equipment', severity: 'critical', icon: '⚠️' },
      { id: 'f2', title: 'Exclusion Zone', description: 'Worker within 1.5m of unguarded edge', severity: 'high', icon: '🚧' },
      { id: 'f3', title: 'Hard Hat OK', description: 'Hard hat detected and properly worn', severity: 'low', icon: '✓' },
      { id: 'f4', title: 'Hi-Vis OK', description: 'High-visibility vest detected', severity: 'low', icon: '✓' },
    ],
    detections: [
      { class: 'person', subclass: 'no_harness', confidence: 0.94, bbox: [320, 180, 420, 380], in_exclusion_zone: true, violation: true },
      { class: 'person', confidence: 0.91, bbox: [580, 220, 660, 400], in_exclusion_zone: false, violation: false },
      { class: 'hard_hat', confidence: 0.88, bbox: [338, 175, 370, 200], in_exclusion_zone: false, violation: false },
      { class: 'scaffold', confidence: 0.96, bbox: [100, 100, 800, 450], in_exclusion_zone: false, violation: false },
    ],
    workers: [
      { worker_id: 'W-1042', name: 'R. Martinez', hard_hat: true, harness: false, hi_vis: true, boots: true, gloves: false, in_zone: true },
      { worker_id: 'W-1038', name: 'J. Chen', hard_hat: true, harness: true, hi_vis: true, boots: true, gloves: true, in_zone: false },
    ],
    timeline: [
      { ts: Date.now() - 600000, label: 'Worker enters scaffold area', type: 'detection' },
      { ts: Date.now() - 540000, label: 'Harness not detected — alert triggered', type: 'alert' },
      { ts: Date.now() - 480000, label: 'Incident created automatically', type: 'system' },
      { ts: Date.now() - 420000, label: 'Supervisor notified via SMS', type: 'action' },
      { ts: Date.now() - 300000, label: 'Worker approaches unguarded edge', type: 'detection' },
    ],
    notifications: [
      { ts: Date.now() - 420000, channel: 'sms', recipient: 'Site Supervisor (J. Vance)', status: 'sent' },
      { ts: Date.now() - 400000, channel: 'email', recipient: 'safety@southgatepower.com', status: 'sent' },
      { ts: Date.now() - 380000, channel: 'push', recipient: 'SG Mobile App', status: 'sent' },
    ],
    comments: [
      { id: 'c1', author: 'J. Vance (Supervisor)', text: 'Worker has been contacted. Moving to safe area.', ts: Date.now() - 240000 },
    ],
    clip_url: '#',
    keyframes: Array.from({ length: 8 }, (_, i) => ({
      ts: Date.now() - 600000 + i * 75000,
      thumbnail_url: '',
      is_key: i === 2 || i === 5,
    })),
    osha_classification: '1926.502(d) — Fall Protection Systems',
    related_incidents: ['INC-2026-0832', 'INC-2026-0819'],
    // SOC actions surfaced to the customer for proof-of-work. The mock
    // values cover the common shape: an operator picked it up quickly,
    // dispositioned it cleanly, and recorded what they saw. The portal
    // renders the "How the SOC handled this" panel only when at least
    // one of these fields is present.
    operator_callsign: 'CTORRES',
    operator_notes: 'Subject was a maintenance worker authorized for the area but missing fall protection. Notified site supervisor (J. Vance) via SMS at 02:14; supervisor confirmed worker pulled and re-trained. No injury, no escalation needed.',
    disposition_code: 'verified-threat-trespasser',
    disposition_label: 'Verified — Safety Violation, Site Notified',
  };
}

// ── Alerts ──

export const MOCK_ALERTS: AlertEvent[] = [
  // ── Security Alarms: Person/Vehicle detections during monitoring hours ──
  { id: 'a1', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-01', camera_name: 'North Perimeter', severity: 'critical', type: 'person_detected', description: 'Person detected at North Perimeter gate — off-hours intrusion', snapshot_url: '', clip_url: '', ts: Date.now() - 120000, acknowledged: false, escalation_level: 0, sla_deadline_ms: Date.now() + 30000 },
  { id: 'a2', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-04', camera_name: 'South Loading', severity: 'high', type: 'vehicle_detected', description: 'Unknown vehicle approaching loading dock — no scheduled delivery', snapshot_url: '', clip_url: '', ts: Date.now() - 300000, acknowledged: false, escalation_level: 0, sla_deadline_ms: Date.now() + 60000 },
  { id: 'a3', site_id: 'GA-091', site_name: 'Atlanta Interchange', camera_id: 'cam-01', camera_name: 'North Gate', severity: 'critical', type: 'person_detected', description: 'Two individuals climbing perimeter fence — North Gate sector', snapshot_url: '', clip_url: '', ts: Date.now() - 480000, acknowledged: false, escalation_level: 0, sla_deadline_ms: Date.now() + 45000 },
  { id: 'a4', site_id: 'CA-089', site_name: 'Marina Bay Tower', camera_id: 'cam-02', camera_name: 'Parking Structure', severity: 'high', type: 'person_detected', description: 'Person detected in restricted parking structure — Level 3', snapshot_url: '', clip_url: '', ts: Date.now() - 600000, acknowledged: false, escalation_level: 0, sla_deadline_ms: Date.now() + 120000 },
  { id: 'a5', site_id: 'FL-312', site_name: 'Bayshore Medical', camera_id: 'cam-03', camera_name: 'East Perimeter', severity: 'medium', type: 'motion_detected', description: 'Sustained motion detected at east perimeter — possible animal or debris', snapshot_url: '', clip_url: '', ts: Date.now() - 900000, acknowledged: true, escalation_level: 0 },
  { id: 'a6', site_id: 'IL-267', site_name: 'O\'Hare Cargo', camera_id: 'cam-02', camera_name: 'Cargo Yard', severity: 'high', type: 'vehicle_detected', description: 'Unregistered vehicle entered cargo yard through Gate B', snapshot_url: '', clip_url: '', ts: Date.now() - 1200000, acknowledged: false, escalation_level: 0 },
  { id: 'a7', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-06', camera_name: 'Material Storage', severity: 'medium', type: 'person_detected', description: 'Person detected near material storage yard — after hours', snapshot_url: '', clip_url: '', ts: Date.now() - 1500000, acknowledged: false, escalation_level: 0 },
  { id: 'a8', site_id: 'NY-445', site_name: 'Midtown Transit', camera_id: 'cam-04', camera_name: 'Tunnel Entry', severity: 'critical', type: 'person_detected', description: 'Person detected entering restricted tunnel access — unauthorized entry', snapshot_url: '', clip_url: '', ts: Date.now() - 1800000, acknowledged: false, escalation_level: 0, sla_deadline_ms: Date.now() + 90000 },
];

// ── Search ──

export function MOCK_SEARCH_RESULTS(query: string): SearchResult[] {
  return [
    { frame_id: 'f001', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-05', camera_name: 'Scaffold Tower', ts: Date.now() - 600000, relevance_score: 0.96, caption: `Worker at elevation without fall harness near unguarded edge — ${query}`, thumbnail_url: '', clip_url: '', detections: [{ class: 'person', subclass: 'no_harness', confidence: 0.94, bbox: [320, 180, 420, 380], in_exclusion_zone: true, violation: true }], violation_flags: { no_harness: true }, token_matches: [{ token: query.split(' ')[0] || 'worker', score: 0.96, source: 'visual' }] },
    { frame_id: 'f002', site_id: 'GA-091', site_name: 'Atlanta Interchange', camera_id: 'cam-01', camera_name: 'North Gate', ts: Date.now() - 3600000, relevance_score: 0.89, caption: `Person entering site without hard hat — ${query}`, thumbnail_url: '', clip_url: '', detections: [{ class: 'person', subclass: 'no_hard_hat', confidence: 0.91, bbox: [200, 100, 300, 350], in_exclusion_zone: false, violation: true }], violation_flags: { no_hard_hat: true }, token_matches: [{ token: 'hard hat', score: 0.89, source: 'caption' }] },
    { frame_id: 'f003', site_id: 'CA-089', site_name: 'Marina Bay Tower', camera_id: 'cam-03', camera_name: 'Foundation', ts: Date.now() - 7200000, relevance_score: 0.82, caption: `Workers near excavation with PPE compliant — ${query}`, thumbnail_url: '', clip_url: '', detections: [{ class: 'person', confidence: 0.88, bbox: [150, 200, 250, 400], in_exclusion_zone: false, violation: false }], violation_flags: {}, token_matches: [{ token: 'workers', score: 0.82, source: 'visual' }] },
    { frame_id: 'f004', site_id: 'FL-312', site_name: 'Bayshore Medical', camera_id: 'cam-02', camera_name: 'Parking Deck', ts: Date.now() - 14400000, relevance_score: 0.75, caption: `Vehicle near worker area, no exclusion zone violation — ${query}`, thumbnail_url: '', clip_url: '', detections: [{ class: 'vehicle', confidence: 0.92, bbox: [400, 250, 700, 450], in_exclusion_zone: false, violation: false }], violation_flags: {}, token_matches: [{ token: 'vehicle', score: 0.75, source: 'caption' }] },
    { frame_id: 'f005', site_id: 'TX-203', site_name: 'Southgate Power', camera_id: 'cam-02', camera_name: 'Crane Zone A', ts: Date.now() - 28800000, relevance_score: 0.71, caption: `Zone breach detected in crane operational area — ${query}`, thumbnail_url: '', clip_url: '', detections: [{ class: 'person', subclass: 'zone_breach', confidence: 0.87, bbox: [350, 150, 430, 370], in_exclusion_zone: true, violation: true }], violation_flags: { zone_breach: true }, token_matches: [{ token: 'zone', score: 0.71, source: 'visual' }] },
    { frame_id: 'f006', site_id: 'NY-445', site_name: 'Midtown Transit', camera_id: 'cam-06', camera_name: 'Tunnel Entry', ts: Date.now() - 43200000, relevance_score: 0.68, caption: `Multiple workers with compliant PPE in tunnel staging area — ${query}`, thumbnail_url: '', clip_url: '', detections: [{ class: 'person', confidence: 0.90, bbox: [100, 200, 200, 400], in_exclusion_zone: false, violation: false }, { class: 'person', confidence: 0.88, bbox: [300, 210, 380, 390], in_exclusion_zone: false, violation: false }], violation_flags: {}, token_matches: [{ token: 'workers', score: 0.68, source: 'caption' }] },
  ];
}

// ── Companies ──

export const MOCK_COMPANIES: Company[] = [
  { id: 'co-turner', name: 'Turner Construction', logo_url: '', plan: 'enterprise', contact_name: 'J. Vance', contact_email: 'jvance@turner.com', created_at: '2025-06-01T00:00:00Z' },
  { id: 'co-bechtel', name: 'Bechtel Group', logo_url: '', plan: 'professional', contact_name: 'S. Weir', contact_email: 'sweir@bechtel.com', created_at: '2025-08-15T00:00:00Z' },
  { id: 'co-kiewit', name: 'Kiewit Corp', logo_url: '', plan: 'enterprise', contact_name: 'M. Torres', contact_email: 'mtorres@kiewit.com', created_at: '2025-03-10T00:00:00Z' },
];

// ── Company Users ──
// Users belong to a company. assigned_site_ids controls which sites they can access.

export const MOCK_COMPANY_USERS: CompanyUser[] = [
  // Turner Construction users
  { id: 'u-001', company_id: 'co-turner', name: 'J. Vance', email: 'jvance@turner.com', role: 'admin', assigned_site_ids: ['TX-203', 'TX-104', 'CA-089'], created_at: '2025-06-01T00:00:00Z' },
  { id: 'u-002', company_id: 'co-turner', name: 'R. Martinez', email: 'rmartinez@turner.com', role: 'safety_manager', assigned_site_ids: ['TX-203', 'TX-104'], created_at: '2025-07-10T00:00:00Z' },
  { id: 'u-003', company_id: 'co-turner', name: 'L. Park', email: 'lpark@turner.com', role: 'supervisor', assigned_site_ids: ['CA-089'], created_at: '2025-08-01T00:00:00Z' },
  { id: 'u-004', company_id: 'co-turner', name: 'D. Okafor', email: 'dokafor@turner.com', role: 'viewer', assigned_site_ids: ['TX-203'], created_at: '2025-09-15T00:00:00Z' },

  // Bechtel Group users
  { id: 'u-005', company_id: 'co-bechtel', name: 'S. Weir', email: 'sweir@bechtel.com', role: 'admin', assigned_site_ids: ['FL-312', 'GA-091'], created_at: '2025-08-15T00:00:00Z' },
  { id: 'u-006', company_id: 'co-bechtel', name: 'K. Huang', email: 'khuang@bechtel.com', role: 'safety_manager', assigned_site_ids: ['FL-312'], created_at: '2025-09-01T00:00:00Z' },
  { id: 'u-007', company_id: 'co-bechtel', name: 'P. Jones', email: 'pjones@bechtel.com', role: 'supervisor', assigned_site_ids: ['GA-091'], created_at: '2025-09-20T00:00:00Z' },

  // Kiewit Corp users
  { id: 'u-008', company_id: 'co-kiewit', name: 'M. Torres', email: 'mtorres@kiewit.com', role: 'admin', assigned_site_ids: ['NY-445', 'WA-178', 'IL-267'], created_at: '2025-03-10T00:00:00Z' },
  { id: 'u-009', company_id: 'co-kiewit', name: 'A. Singh', email: 'asingh@kiewit.com', role: 'safety_manager', assigned_site_ids: ['NY-445', 'IL-267'], created_at: '2025-04-01T00:00:00Z' },
  { id: 'u-010', company_id: 'co-kiewit', name: 'C. Reeves', email: 'creeves@kiewit.com', role: 'supervisor', assigned_site_ids: ['WA-178'], created_at: '2025-05-01T00:00:00Z' },
];

// ── Camera Assignments ──

export const MOCK_CAMERA_ASSIGNMENTS: CameraAssignment[] = [
  { site_id: 'TX-203', camera_id: 'cam-uuid-001', camera_name: 'Front Gate Camera', location_label: 'North Perimeter', assigned_at: '2025-09-01T00:00:00Z' },
  { site_id: 'TX-203', camera_id: 'cam-uuid-002', camera_name: 'Parking Lot West', location_label: 'Crane Zone A', assigned_at: '2025-09-01T00:00:00Z' },
  { site_id: 'TX-203', camera_id: 'cam-uuid-003', camera_name: 'Loading Dock A', location_label: 'Excavation Pit', assigned_at: '2025-09-15T00:00:00Z' },
  { site_id: 'TX-104', camera_id: 'cam-uuid-004', camera_name: 'Roof Overview', location_label: 'Bridge Deck', assigned_at: '2025-10-01T00:00:00Z' },
  { site_id: 'CA-089', camera_id: 'cam-uuid-005', camera_name: 'Interior Office', location_label: 'Tower Lobby', assigned_at: '2025-10-01T00:00:00Z' },
];

// ── Site User Assignments ──
// Denormalized view: which users are assigned to each site.

export const MOCK_SITE_USER_ASSIGNMENTS: SiteUserAssignment[] = [
  // TX-203 (Turner) — Vance, Martinez, Okafor
  { site_id: 'TX-203', user_id: 'u-001', user_name: 'J. Vance', user_email: 'jvance@turner.com', role: 'admin', assigned_at: '2025-06-01T00:00:00Z' },
  { site_id: 'TX-203', user_id: 'u-002', user_name: 'R. Martinez', user_email: 'rmartinez@turner.com', role: 'safety_manager', assigned_at: '2025-07-10T00:00:00Z' },
  { site_id: 'TX-203', user_id: 'u-004', user_name: 'D. Okafor', user_email: 'dokafor@turner.com', role: 'viewer', assigned_at: '2025-09-15T00:00:00Z' },
  // TX-104 (Turner) — Vance, Martinez
  { site_id: 'TX-104', user_id: 'u-001', user_name: 'J. Vance', user_email: 'jvance@turner.com', role: 'admin', assigned_at: '2025-06-01T00:00:00Z' },
  { site_id: 'TX-104', user_id: 'u-002', user_name: 'R. Martinez', user_email: 'rmartinez@turner.com', role: 'safety_manager', assigned_at: '2025-07-10T00:00:00Z' },
  // CA-089 (Turner) — Vance, Park
  { site_id: 'CA-089', user_id: 'u-001', user_name: 'J. Vance', user_email: 'jvance@turner.com', role: 'admin', assigned_at: '2025-06-01T00:00:00Z' },
  { site_id: 'CA-089', user_id: 'u-003', user_name: 'L. Park', user_email: 'lpark@turner.com', role: 'supervisor', assigned_at: '2025-08-01T00:00:00Z' },
  // FL-312 (Bechtel) — Weir, Huang
  { site_id: 'FL-312', user_id: 'u-005', user_name: 'S. Weir', user_email: 'sweir@bechtel.com', role: 'admin', assigned_at: '2025-08-15T00:00:00Z' },
  { site_id: 'FL-312', user_id: 'u-006', user_name: 'K. Huang', user_email: 'khuang@bechtel.com', role: 'safety_manager', assigned_at: '2025-09-01T00:00:00Z' },
  // GA-091 (Bechtel) — Weir, Jones
  { site_id: 'GA-091', user_id: 'u-005', user_name: 'S. Weir', user_email: 'sweir@bechtel.com', role: 'admin', assigned_at: '2025-08-15T00:00:00Z' },
  { site_id: 'GA-091', user_id: 'u-007', user_name: 'P. Jones', user_email: 'pjones@bechtel.com', role: 'supervisor', assigned_at: '2025-09-20T00:00:00Z' },
  // NY-445 (Kiewit) — Torres, Singh
  { site_id: 'NY-445', user_id: 'u-008', user_name: 'M. Torres', user_email: 'mtorres@kiewit.com', role: 'admin', assigned_at: '2025-03-10T00:00:00Z' },
  { site_id: 'NY-445', user_id: 'u-009', user_name: 'A. Singh', user_email: 'asingh@kiewit.com', role: 'safety_manager', assigned_at: '2025-04-01T00:00:00Z' },
  // WA-178 (Kiewit) — Torres, Reeves
  { site_id: 'WA-178', user_id: 'u-008', user_name: 'M. Torres', user_email: 'mtorres@kiewit.com', role: 'admin', assigned_at: '2025-03-10T00:00:00Z' },
  { site_id: 'WA-178', user_id: 'u-010', user_name: 'C. Reeves', user_email: 'creeves@kiewit.com', role: 'supervisor', assigned_at: '2025-05-01T00:00:00Z' },
  // IL-267 (Kiewit) — Torres, Singh
  { site_id: 'IL-267', user_id: 'u-008', user_name: 'M. Torres', user_email: 'mtorres@kiewit.com', role: 'admin', assigned_at: '2025-03-10T00:00:00Z' },
  { site_id: 'IL-267', user_id: 'u-009', user_name: 'A. Singh', user_email: 'asingh@kiewit.com', role: 'safety_manager', assigned_at: '2025-04-01T00:00:00Z' },
];

// ── SOC Operators ──
// Internal monitoring staff. These are our people, not customer users.

export const MOCK_SOC_OPERATORS: SOCOperator[] = [
  { id: 'op-001', name: 'Sarah Chen', callsign: 'OP-1', status: 'on_shift', shift_start: new Date(Date.now() - 14400000).toISOString() },
  { id: 'op-002', name: 'Marcus Webb', callsign: 'OP-2', status: 'on_shift', shift_start: new Date(Date.now() - 10800000).toISOString() },
  { id: 'op-003', name: 'Priya Nair', callsign: 'OP-3', status: 'on_shift', shift_start: new Date(Date.now() - 7200000).toISOString() },
  { id: 'op-004', name: 'James Kowalski', callsign: 'OP-4', status: 'break', shift_start: new Date(Date.now() - 18000000).toISOString() },
  { id: 'op-005', name: 'Diana Reyes', callsign: 'OP-5', status: 'off_shift' },
];

// Current operator for this session (first operator)
export const MOCK_CURRENT_OPERATOR = MOCK_SOC_OPERATORS[0];

// ── Site Locks ──
// Which sites are currently locked by which operator.

export const MOCK_SITE_LOCKS: SiteLock[] = [
  { site_id: 'TX-203', operator_id: 'op-001', operator_callsign: 'OP-1', locked_at: new Date(Date.now() - 1800000).toISOString() },
  { site_id: 'NY-445', operator_id: 'op-002', operator_callsign: 'OP-2', locked_at: new Date(Date.now() - 3600000).toISOString() },
  { site_id: 'GA-091', operator_id: 'op-003', operator_callsign: 'OP-3', locked_at: new Date(Date.now() - 900000).toISOString() },
];

// ── Site SOPs ──
// Standard operating procedures per site.

export const MOCK_SITE_SOPS: SiteSOP[] = [
  // TX-203 SOPs
  {
    id: 'sop-001', site_id: 'TX-203', title: 'Unauthorized Access – Exclusion Zone',
    category: 'access', priority: 'critical',
    steps: [
      'Immediately activate audio warning via PA system',
      'Zoom camera to isolate individual(s) and capture clear facial image',
      'Contact site supervisor: R. Martinez (832-555-0142)',
      'If no response within 2 minutes, dispatch on-site security',
      'Log incident in Ironsight with screenshot evidence',
      'Notify client safety manager within 15 minutes',
    ],
    contacts: [
      { name: 'R. Martinez', role: 'Site Supervisor', phone: '832-555-0142', email: 'rmartinez@turner.com' },
      { name: 'Security Desk', role: 'On-site Security', phone: '832-555-0100' },
    ],
    updated_at: '2026-03-15T14:00:00Z', updated_by: 'J. Vance',
  },
  {
    id: 'sop-002', site_id: 'TX-203', title: 'Person Detected – After Hours',
    category: 'access', priority: 'critical',
    steps: [
      'Review event clip — confirm human presence (not animal/debris)',
      'Switch to live view and check adjacent cameras for movement',
      'If confirmed: call on-site guard first',
      'If guard unavailable: call site supervisor R. Martinez',
      'If threat is imminent or active break-in: dispatch local PD',
      'Log all actions with timestamps in disposition notes',
      'Bookmark event clip on NVR for evidence',
    ],
    contacts: [
      { name: 'On-Site Guard', role: 'Night Security', phone: '832-555-0100' },
      { name: 'R. Martinez', role: 'Site Supervisor', phone: '832-555-0142', email: 'rmartinez@turner.com' },
      { name: 'Houston PD Non-Emergency', role: 'Law Enforcement', phone: '713-884-3131' },
      { name: 'Houston PD Emergency', role: 'Emergency', phone: '911' },
    ],
    updated_at: '2026-03-20T10:00:00Z', updated_by: 'S. Chen',
  },
  {
    id: 'sop-003', site_id: 'TX-203', title: 'Vehicle Detected – After Hours',
    category: 'access', priority: 'high',
    steps: [
      'Review event clip — identify vehicle type and license plate if visible',
      'Check if vehicle matches any scheduled deliveries or contractor vehicles',
      'Switch to live view — track vehicle movement across cameras',
      'If unauthorized: call on-site guard',
      'If vehicle is stationary near material storage: escalate to supervisor',
      'If active theft in progress: dispatch local PD immediately',
      'Capture clear screenshots of vehicle and occupants',
    ],
    contacts: [
      { name: 'On-Site Guard', role: 'Night Security', phone: '832-555-0100' },
      { name: 'R. Martinez', role: 'Site Supervisor', phone: '832-555-0142' },
    ],
    updated_at: '2026-04-01T09:00:00Z', updated_by: 'S. Chen',
  },
  {
    id: 'sop-004', site_id: 'TX-203', title: 'Fire / Smoke Detection',
    category: 'emergency', priority: 'critical',
    steps: [
      'Verify visual confirmation on camera — rule out steam/dust',
      'If confirmed: call 911 immediately',
      'Activate fire alarm via panel (Building C, Panel 2)',
      'Notify site supervisor and all field personnel',
      'Monitor evacuation routes on cameras 3, 5, 7',
      'Document timeline of events until fire department arrives',
    ],
    contacts: [
      { name: '911', role: 'Emergency Services', phone: '911' },
      { name: 'R. Martinez', role: 'Site Supervisor', phone: '832-555-0142' },
    ],
    updated_at: '2026-02-10T08:00:00Z', updated_by: 'J. Vance',
  },
  // NY-445 SOPs
  {
    id: 'sop-005', site_id: 'NY-445', title: 'After-Hours Motion Detection',
    category: 'access', priority: 'high',
    steps: [
      'Verify motion is not wildlife/debris — check surrounding cameras',
      'If human presence confirmed, activate spotlight on zone',
      'Record 60-second clip starting 10s before event',
      'Call site security: transit police desk',
      'Escalate to Kiewit duty manager if no response in 5 min',
    ],
    contacts: [
      { name: 'Transit Police Desk', role: 'Security', phone: '212-555-0300' },
      { name: 'M. Torres', role: 'Duty Manager', phone: '212-555-0280' },
    ],
    updated_at: '2026-03-28T16:00:00Z', updated_by: 'M. Torres',
  },
  {
    id: 'sop-006', site_id: 'NY-445', title: 'General Site Procedure',
    category: 'general', priority: 'normal',
    steps: [
      'Maintain visual sweep of all 10 cameras every 15 minutes',
      'Log notable observations in shift notes',
      'Coordinate with incoming shift operator on active situations',
      'Ensure all recording indicators are green before shift end',
    ],
    contacts: [
      { name: 'SOC Supervisor', role: 'Internal', phone: '555-555-0001' },
    ],
    updated_at: '2026-01-15T12:00:00Z', updated_by: 'SOC Admin',
  },
];

// ── Site Maps ──
// Layout images with camera marker positions.

export const MOCK_SITE_MAPS: SiteMapData[] = [
  {
    site_id: 'TX-203',
    image_url: '/site-maps/tx-203-layout.svg', // placeholder — would be uploaded
    image_width: 1200,
    image_height: 800,
    markers: [
      { id: 'sm-001', camera_id: 'cam-uuid-001', camera_name: 'Front Gate Camera', x: 12, y: 85, rotation: 0, fov_angle: 90, label: 'North Gate' },
      { id: 'sm-002', camera_id: 'cam-uuid-002', camera_name: 'Parking Lot West', x: 25, y: 60, rotation: 180, fov_angle: 120, label: 'Crane Zone A' },
      { id: 'sm-003', camera_id: 'cam-uuid-003', camera_name: 'Loading Dock A', x: 65, y: 35, rotation: 90, fov_angle: 80, label: 'Excavation Pit' },
    ],
    updated_at: '2026-03-01T10:00:00Z',
  },
  {
    site_id: 'NY-445',
    image_url: '/site-maps/ny-445-layout.svg',
    image_width: 1400,
    image_height: 900,
    markers: [
      { id: 'sm-004', camera_id: 'cam-uuid-006', camera_name: 'Platform A', x: 15, y: 30, rotation: 270, fov_angle: 110, label: 'Platform A' },
      { id: 'sm-005', camera_id: 'cam-uuid-007', camera_name: 'Concourse Entry', x: 50, y: 10, rotation: 180, fov_angle: 90, label: 'Main Entry' },
      { id: 'sm-006', camera_id: 'cam-uuid-008', camera_name: 'Loading Bay', x: 85, y: 65, rotation: 0, fov_angle: 100, label: 'Loading Bay' },
    ],
    updated_at: '2026-02-20T14:00:00Z',
  },
];

// ════════════════════════════════════════════════════════════════
// Complete Platform Mock Data
// ════════════════════════════════════════════════════════════════

// ── SLA Configuration ──

export const MOCK_SLA_CONFIGS: SLAConfig[] = [
  { severity: 'critical', acknowledge_sla_ms: 30000, resolve_sla_ms: 300000, label: 'Critical: 30s ack / 5m resolve' },
  { severity: 'high', acknowledge_sla_ms: 120000, resolve_sla_ms: 900000, label: 'High: 2m ack / 15m resolve' },
  { severity: 'medium', acknowledge_sla_ms: 300000, resolve_sla_ms: 1800000, label: 'Medium: 5m ack / 30m resolve' },
  { severity: 'low', acknowledge_sla_ms: 600000, resolve_sla_ms: 3600000, label: 'Low: 10m ack / 1h resolve' },
];

// ── Escalation Rules ──

export const MOCK_ESCALATION_RULES: EscalationRule[] = [
  {
    id: 'esc-001', site_id: 'TX-203', severity: 'critical', acknowledge_timeout_ms: 30000,
    escalation_chain: [
      { level: 1, label: 'SOC Supervisor', timeout_ms: 60000, notify_channels: ['push'], notify_contacts: ['SOC Supervisor'] },
      { level: 2, label: 'Site Manager', timeout_ms: 120000, notify_channels: ['sms', 'email'], notify_contacts: ['R. Martinez'] },
      { level: 3, label: 'Emergency Services', timeout_ms: 180000, notify_channels: ['sms', 'pa'], notify_contacts: ['911', 'Safety Director'] },
    ],
  },
  {
    id: 'esc-002', site_id: 'TX-203', severity: 'high', acknowledge_timeout_ms: 120000,
    escalation_chain: [
      { level: 1, label: 'SOC Supervisor', timeout_ms: 180000, notify_channels: ['push'], notify_contacts: ['SOC Supervisor'] },
      { level: 2, label: 'Site Manager', timeout_ms: 300000, notify_channels: ['email'], notify_contacts: ['R. Martinez'] },
    ],
  },
];

// ── Audit Trail ──

export const MOCK_AUDIT_ENTRIES: AuditEntry[] = [
  { id: 'aud-001', action: 'login', operator_id: 'op-001', operator_callsign: 'OP-1', entity_type: 'system', entity_id: 'session', ts: Date.now() - 14400000 },
  { id: 'aud-002', action: 'site_locked', operator_id: 'op-001', operator_callsign: 'OP-1', entity_type: 'site', entity_id: 'TX-203', ts: Date.now() - 14000000 },
  { id: 'aud-003', action: 'alert_claimed', operator_id: 'op-001', operator_callsign: 'OP-1', entity_type: 'alert', entity_id: 'a1', metadata: { severity: 'critical' }, ts: Date.now() - 13000000 },
  { id: 'aud-004', action: 'sop_viewed', operator_id: 'op-001', operator_callsign: 'OP-1', entity_type: 'sop', entity_id: 'sop-001', metadata: { title: 'Unauthorized Access' }, ts: Date.now() - 12500000 },
  { id: 'aud-005', action: 'alert_acknowledged', operator_id: 'op-001', operator_callsign: 'OP-1', entity_type: 'alert', entity_id: 'a3', ts: Date.now() - 12000000 },
  { id: 'aud-006', action: 'site_locked', operator_id: 'op-002', operator_callsign: 'OP-2', entity_type: 'site', entity_id: 'NY-445', ts: Date.now() - 11000000 },
  { id: 'aud-007', action: 'alert_claimed', operator_id: 'op-002', operator_callsign: 'OP-2', entity_type: 'alert', entity_id: 'a6', ts: Date.now() - 10000000 },
  { id: 'aud-008', action: 'alert_escalated', operator_id: 'op-003', operator_callsign: 'OP-3', entity_type: 'alert', entity_id: 'a8', metadata: { level: '2' }, ts: Date.now() - 9000000 },
  { id: 'aud-009', action: 'ptz_command', operator_id: 'op-001', operator_callsign: 'OP-1', entity_type: 'site', entity_id: 'cam-05', metadata: { command: 'zoom_in' }, ts: Date.now() - 8000000 },
  { id: 'aud-010', action: 'evidence_exported', operator_id: 'op-001', operator_callsign: 'OP-1', entity_type: 'incident', entity_id: 'INC-2026-0847', ts: Date.now() - 7000000 },
  { id: 'aud-011', action: 'shift_handoff_created', operator_id: 'op-004', operator_callsign: 'OP-4', entity_type: 'shift', entity_id: 'hoff-001', metadata: { to: 'OP-1' }, ts: Date.now() - 6000000 },
  { id: 'aud-012', action: 'incident_created', operator_id: 'op-002', operator_callsign: 'OP-2', entity_type: 'incident', entity_id: 'INC-2026-0848', ts: Date.now() - 5000000 },
];

// ── Operator Presence ──

export const MOCK_OPERATOR_PRESENCE: OperatorPresence[] = [
  { operator_id: 'op-001', operator_callsign: 'OP-1', status: 'on_shift', viewing_site_id: 'TX-203', viewing_camera_id: 'cam-05', last_seen: Date.now() - 1000 },
  { operator_id: 'op-002', operator_callsign: 'OP-2', status: 'on_shift', viewing_site_id: 'NY-445', viewing_camera_id: 'cam-06', last_seen: Date.now() - 3000 },
  { operator_id: 'op-003', operator_callsign: 'OP-3', status: 'on_shift', viewing_site_id: 'GA-091', last_seen: Date.now() - 5000 },
  { operator_id: 'op-004', operator_callsign: 'OP-4', status: 'break', last_seen: Date.now() - 120000 },
  { operator_id: 'op-005', operator_callsign: 'OP-5', status: 'off_shift', last_seen: Date.now() - 28800000 },
];

// ── Operator Metrics ──

export const MOCK_OPERATOR_METRICS: OperatorMetrics[] = [
  { operator_id: 'op-001', operator_callsign: 'OP-1', operator_name: 'Sarah Chen', shift_hours: 4.2, events_handled: 23, avg_response_time_ms: 18400, avg_resolve_time_ms: 234000, sla_compliance_pct: 96, alerts_claimed: 18, alerts_escalated: 2, sites_monitored: 4 },
  { operator_id: 'op-002', operator_callsign: 'OP-2', operator_name: 'Marcus Webb', shift_hours: 3.1, events_handled: 15, avg_response_time_ms: 24600, avg_resolve_time_ms: 312000, sla_compliance_pct: 88, alerts_claimed: 12, alerts_escalated: 4, sites_monitored: 3 },
  { operator_id: 'op-003', operator_callsign: 'OP-3', operator_name: 'Priya Nair', shift_hours: 2.0, events_handled: 11, avg_response_time_ms: 15200, avg_resolve_time_ms: 198000, sla_compliance_pct: 100, alerts_claimed: 11, alerts_escalated: 0, sites_monitored: 2 },
  { operator_id: 'op-004', operator_callsign: 'OP-4', operator_name: 'James Kowalski', shift_hours: 5.0, events_handled: 31, avg_response_time_ms: 22100, avg_resolve_time_ms: 278000, sla_compliance_pct: 91, alerts_claimed: 24, alerts_escalated: 5, sites_monitored: 5 },
];

// ── Scheduled Reports ──

export const MOCK_SCHEDULED_REPORTS: ScheduledReport[] = [
  { id: 'rpt-001', name: 'Daily Safety Digest', type: 'safety', frequency: 'daily', site_ids: ['TX-203', 'NY-445'], recipients: ['rmartinez@turner.com', 'safety@ironsight.ai'], next_run: '2026-04-16T06:00:00Z', last_run: '2026-04-15T06:00:00Z', enabled: true },
  { id: 'rpt-002', name: 'Weekly Compliance Report', type: 'compliance', frequency: 'weekly', site_ids: ['TX-203', 'GA-091', 'CA-089', 'FL-312', 'NY-445', 'IL-267'], recipients: ['compliance@turner.com', 'admin@ironsight.ai'], next_run: '2026-04-21T08:00:00Z', last_run: '2026-04-14T08:00:00Z', enabled: true },
  { id: 'rpt-003', name: 'Monthly Executive Summary', type: 'executive', frequency: 'monthly', site_ids: [], recipients: ['cfo@turner.com', 'ceo@kiewit.com'], next_run: '2026-05-01T09:00:00Z', last_run: '2026-04-01T09:00:00Z', enabled: true },
  { id: 'rpt-004', name: 'Incident Summary (Paused)', type: 'incidents', frequency: 'daily', site_ids: ['TX-203'], recipients: ['rmartinez@turner.com'], next_run: '2026-04-16T18:00:00Z', enabled: false },
];

// ── Notification Rules ──

export const MOCK_NOTIFICATION_RULES: NotificationRule[] = [
  { id: 'notif-001', site_id: 'TX-203', name: 'Critical Alert – Immediate', severity_filter: ['critical'], channels: ['sms', 'push'], recipients: [{ name: 'R. Martinez', phone: '832-555-0142' }, { name: 'Safety Desk', phone: '832-555-0100' }], schedule: 'immediate', enabled: true },
  { id: 'notif-002', site_id: 'TX-203', name: 'Daily Digest', severity_filter: ['high', 'medium'], channels: ['email'], recipients: [{ name: 'Site Team', email: 'team@turner.com' }], schedule: 'digest_daily', enabled: true },
  { id: 'notif-003', site_id: 'NY-445', name: 'All Events Webhook', severity_filter: ['critical', 'high', 'medium', 'low'], channels: ['webhook'], recipients: [{ name: 'SIEM Integration', webhook_url: 'https://siem.kiewit.com/webhook/ironsight' }], schedule: 'immediate', enabled: true },
];

// ── PTZ Capabilities ──

export const MOCK_PTZ_CAPABILITIES: PTZCapability[] = [
  { camera_id: 'cam-05', supports_ptz: true, supports_presets: true, presets: [
    { id: 'ptz-p1', name: 'Scaffold Overview', pan: 0, tilt: -15, zoom: 1 },
    { id: 'ptz-p2', name: 'Level 4 Close-up', pan: 30, tilt: -45, zoom: 8 },
    { id: 'ptz-p3', name: 'Ground Entry', pan: -90, tilt: 0, zoom: 3 },
  ]},
  { camera_id: 'cam-02', supports_ptz: true, supports_presets: true, presets: [
    { id: 'ptz-p4', name: 'Crane Zone Wide', pan: 0, tilt: 0, zoom: 1 },
    { id: 'ptz-p5', name: 'Operator Cab', pan: 45, tilt: -30, zoom: 12 },
  ]},
  { camera_id: 'cam-01', supports_ptz: false, supports_presets: false, presets: [] },
];

// ── Exclusion Zones ──

export const MOCK_EXCLUSION_ZONES: ExclusionZone[] = [
  { id: 'ez-001', site_id: 'TX-203', name: 'Crane Swing Radius', polygon: [[20, 30], [45, 25], [50, 55], [25, 60]], severity: 'critical', active: true, created_at: '2026-03-01T00:00:00Z' },
  { id: 'ez-002', site_id: 'TX-203', name: 'Excavation Pit', polygon: [[55, 50], [80, 48], [82, 72], [58, 75]], severity: 'high', active: true, created_at: '2026-03-05T00:00:00Z' },
  { id: 'ez-003', site_id: 'TX-203', name: 'After-Hours Perimeter', polygon: [[5, 5], [95, 5], [95, 95], [5, 95]], severity: 'medium', active: true, created_at: '2026-02-20T00:00:00Z', schedule: { start_hour: 20, end_hour: 6 } },
  { id: 'ez-004', site_id: 'NY-445', name: 'Platform Edge', polygon: [[10, 20], [40, 15], [42, 50], [12, 55]], severity: 'critical', active: true, created_at: '2026-02-15T00:00:00Z' },
];

// ── Saved Searches ──

export const MOCK_SAVED_SEARCHES: SavedSearch[] = [
  { id: 'ss-001', name: 'Workers near crane w/o harness', query: 'worker near crane without harness', filters: { violation_types: ['no_harness'], site_ids: ['TX-203'] }, created_by: 'OP-1', shared: true, created_at: '2026-03-20T10:00:00Z', run_count: 47 },
  { id: 'ss-002', name: 'After-hours motion', query: 'person moving after dark', filters: { time_of_day: { start: '20:00', end: '06:00' } }, created_by: 'OP-2', shared: true, created_at: '2026-03-25T14:00:00Z', run_count: 23 },
  { id: 'ss-003', name: 'Excavation zone breach', query: 'zone breach excavation area', filters: { violation_types: ['zone_breach'] }, created_by: 'OP-1', shared: false, created_at: '2026-04-01T08:00:00Z', run_count: 12 },
  { id: 'ss-004', name: 'Vehicle in pedestrian area', query: 'vehicle forklift pedestrian', filters: {}, created_by: 'OP-3', shared: true, created_at: '2026-04-05T11:00:00Z', run_count: 8 },
];

// ── Integrations ──

export const MOCK_INTEGRATIONS: Integration[] = [
  { id: 'int-001', type: 'slack', name: 'Slack #construction-alerts', config: { workspace: 'turner-corp', channel: '#construction-alerts' }, site_ids: ['TX-203', 'NY-445'], active: true, created_at: '2026-02-01T00:00:00Z', last_triggered: '2026-04-15T19:45:00Z' },
  { id: 'int-002', type: 'webhook', name: 'Kiewit SIEM', config: { url: 'https://siem.kiewit.com/webhook/ironsight', secret: 'sk_***', events: ['alert.created', 'incident.escalated', 'incident.resolved'] }, site_ids: ['NY-445', 'IL-267'], active: true, created_at: '2026-03-01T00:00:00Z', last_triggered: '2026-04-15T20:12:00Z' },
  { id: 'int-003', type: 'api_key', name: 'Turner API Access', config: { key_prefix: 'sg_live_abc123', permissions: ['read'], expires_at: '2027-01-01T00:00:00Z' }, site_ids: [], active: true, created_at: '2026-01-15T00:00:00Z' },
  { id: 'int-004', type: 'teams', name: 'MS Teams Safety Channel', config: { workspace: 'Turner Construction', channel: 'Safety Alerts' }, site_ids: ['TX-203'], active: false, created_at: '2026-03-15T00:00:00Z' },
];

// ── Portal Summary ──

// Frontend-only fallback for the customer "what we handled" panel
// when the backend isn't reachable. Numbers are weighted to tell
// the value story: heavy false-positive filter, fast response,
// thin slice of verified threats.
export interface PortalSummary {
  period_days: number;
  period_start: string;
  period_end: string;
  events_handled: number;
  verified_threats: number;
  false_positives: number;
  alarms_total: number;
  avg_response_sec: number;
  p95_response_sec: number;
  within_sla: number;
  over_sla: number;
}

export function MOCK_PORTAL_SUMMARY(days: number): PortalSummary {
  const events = Math.round(4.2 * days);
  return {
    period_days: days,
    period_start: new Date(Date.now() - days * 86_400_000).toISOString(),
    period_end:   new Date().toISOString(),
    events_handled:   events,
    verified_threats: Math.round(events * 0.06),
    false_positives:  Math.round(events * 0.68),
    alarms_total:     events,
    avg_response_sec: 38,
    p95_response_sec: 142,
    within_sla:       Math.round(events * 0.94),
    over_sla:         Math.round(events * 0.06),
  };
}

// ── Shift Handoffs ──

export const MOCK_SHIFT_HANDOFFS: ShiftHandoff[] = [
  {
    id: 'hoff-001',
    from_operator_id: 'op-004', from_operator_callsign: 'OP-4',
    to_operator_id: 'op-001', to_operator_callsign: 'OP-1',
    locked_site_ids: ['TX-203', 'GA-091'],
    active_alert_ids: ['a1', 'a4'],
    notes: 'Critical harness violation on TX-203 Scaffold Tower L4 still active. Martinez has been notified. GA-091 quiet last 2 hours, routine monitoring only.',
    status: 'pending',
    created_at: new Date(Date.now() - 3600000).toISOString(),
  },
];
