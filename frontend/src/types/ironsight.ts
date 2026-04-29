// ── Ironsight AI Type Definitions ──

export type Severity = 'critical' | 'high' | 'medium' | 'low';
export type IncidentStatus = 'open' | 'in_review' | 'resolved';
export type SiteStatus = 'active' | 'idle' | 'critical';

// ── SOC Operators ──
// Internal monitoring staff (our personnel, not customer users).
// Operators log into the operator console, lock sites, and claim alerts.

export interface SOCOperator {
  id: string;
  name: string;
  callsign: string;           // "OP-1", "OP-2" — compact display in UI
  avatar_url?: string;
  status: 'on_shift' | 'off_shift' | 'break';
  shift_start?: string;       // ISO timestamp
}

// ── Site Locking ──
// An operator locks a site to claim exclusive monitoring responsibility.

export interface SiteLock {
  site_id: string;
  operator_id: string;
  operator_callsign: string;
  locked_at: string;          // ISO timestamp
}

// ── Sites ──

/**
 * Determines which product tier is active for a site.
 * Decided at the site level — a customer can have a mix.
 *   security_only        → Cameras, Recordings, SOC events, Security reports
 *   security_and_safety  → All of the above + PPE compliance, OSHA, vLM safety engine
 */
export type SiteFeatureMode = 'security_only' | 'security_and_safety';

/** A monitoring time window — e.g. Mon–Fri 18:00–06:00 */
export interface MonitoringWindow {
  id: string;
  label: string;             // "Weeknight", "Weekend 24hr"
  days: number[];            // 0=Sun … 6=Sat
  start_time: string;        // "18:00" (24h)
  end_time: string;          // "06:00"
  enabled: boolean;
}

/** Snooze/disarm state for a site — set by customer */
export interface SiteSnooze {
  active: boolean;
  reason: string;
  snoozed_by: string;        // user name / email
  snoozed_at: string;        // ISO
  expires_at: string;        // ISO — auto-rearm
}

export interface SiteSummary {
  id: string;              // e.g. "TX-203"
  name: string;
  status: SiteStatus;
  compliance_score: number; // 0-100 (only meaningful for security_and_safety sites)
  cameras_online: number;
  cameras_total: number;
  open_incidents: number;
  workers_on_site: number;
  last_activity: string;    // ISO timestamp
  trend: 'up' | 'down' | 'flat';
  company_id?: string;      // owning company
  feature_mode?: SiteFeatureMode;
  monitoring_schedule?: MonitoringWindow[];
  snooze?: SiteSnooze;
}

export interface SiteDetail extends SiteSummary {
  address: string;
  latitude: number;
  longitude: number;
  cameras: SiteCamera[];
  compliance_history: ComplianceDataPoint[];
  ppe_breakdown: PPEBreakdown;
  risk_notes: string[];
}

export interface SiteCamera {
  id: string;
  name: string;
  location: string;        // "North Perimeter", "Crane Zone", etc.
  status: 'online' | 'offline' | 'degraded';
  has_alert: boolean;
  stream_url?: string;
  snapshot_url?: string;
}

// ── Compliance ──

export interface ComplianceDataPoint {
  date: string;            // ISO date
  hard_hat: number;        // 0-100
  harness: number;
  hi_vis: number;
  boots: number;
  overall: number;
}

export interface ComplianceHistory {
  site_id: string;
  data: ComplianceDataPoint[];
}

export interface PPEBreakdown {
  hard_hat: number;
  harness: number;
  hi_vis: number;
  boots: number;
  gloves: number;
}

// ── Alerts ──

export interface AlertEvent {
  id: string;
  incident_id?: string;    // parent incident grouping
  site_id: string;
  site_name: string;
  camera_id: string;
  camera_name: string;
  severity: Severity;
  type: string;            // "no_hard_hat" | "zone_breach" | "no_harness" ...
  description: string;
  snapshot_url: string;
  clip_url: string;
  ts: number;              // Unix ms
  acknowledged: boolean;
  // SOC operator assignment
  assigned_operator_id?: string;
  assigned_operator_callsign?: string;
  // Escalation
  escalation_level: number;       // 0 = none, 1 = first, 2 = supervisor, 3 = management
  escalated_at?: number;          // Unix ms when last escalated
  sla_deadline_ms?: number;       // Unix ms deadline for acknowledgment
  // AI analytics metadata (from Milesight WebSocket)
  ai_score?: number;              // 0-1 confidence from camera neural network
  obj_type?: string;              // "human", "vehicle", "face" — what was detected
  rule_name?: string;             // VCA rule that triggered (e.g., "Intrusion Zone 1")
  bounding_boxes?: { x: number; y: number; w: number; h: number; label?: string }[];
  // AI pipeline enrichment (YOLO + Qwen vLM)
  ai_description?: string;        // Natural language scene description from Qwen
  ai_threat_level?: string;       // "critical" | "high" | "medium" | "low" | "none"
  ai_recommended_action?: string;  // Specific action for the SOC operator
  ai_false_positive_pct?: number;  // 0-1 false positive likelihood
  ai_objects?: { type: string; attributes: Record<string, unknown> }[];
  ai_detections?: { class: string; confidence: number; bbox_normalized: { x1: number; y1: number; x2: number; y2: number } }[];
  ai_ppe_violations?: { class: string; confidence: number; missing?: string; bbox_normalized: { x1: number; y1: number; x2: number; y2: number } }[];
}

/** Groups related alarms from the same site within a correlation window. */
export interface SOCIncident {
  id: string;
  site_id: string;
  site_name: string;
  severity: Severity;
  status: 'active' | 'acknowledged';
  alarm_count: number;
  camera_ids: string[];
  camera_names: string[];
  types: string[];          // unique event types in this incident
  latest_type: string;
  description: string;
  snapshot_url: string;
  clip_url: string;
  first_alarm_ts: number;   // Unix ms
  last_alarm_ts: number;    // Unix ms
  sla_deadline_ms: number;
}

// ── Incidents ──

export interface IncidentSummary {
  id: string;              // "EVT-2026-1234"
  severity: Severity;
  status: IncidentStatus;
  title: string;
  site_id: string;
  site_name: string;
  camera_id: string;
  camera_name?: string;
  ts: number;              // Unix ms
  resolved_at?: number;    // Unix ms
  workers_identified: number;
  type: string;            // "intrusion", "linecross", etc.
  // SOC disposition
  disposition_code?: string;   // "false_positive_shadow", "verified_police_dispatched"
  disposition_label?: string;  // "False Positive — Shadow/Light"
  operator_callsign?: string;  // "ADMIN"
  // SOC operator assignment
  assigned_operator_id?: string;
  // Escalation
  escalation_level?: number;
}

export interface IncidentDetail {
  id: string;
  severity: Severity;
  status: IncidentStatus;
  title: string;
  site_id: string;
  site_name: string;
  camera_id: string;
  camera_name: string;
  ts: number;
  duration_ms: number;     // ts → resolved_at; the SOC's response window
  workers_identified: number;
  ai_confidence: number;   // 0-1
  ai_caption: string;
  findings: Finding[];
  detections: Detection[];
  workers: WorkerPPE[];
  timeline: TimelineEvent[];
  notifications: NotificationLog[];
  comments: Comment[];
  clip_url: string;
  keyframes: Keyframe[];
  osha_classification: string;
  related_incidents: string[];
  // SOC actions surfaced to customer for transparency. The portal's
  // "what we did" panel renders these so the customer sees the
  // operator who handled it, what they decided, and any context they
  // recorded — not just an opaque "resolved" status.
  operator_callsign?: string;
  operator_notes?: string;
  disposition_code?: string;   // see operator's DISPOSITION_OPTIONS
  disposition_label?: string;
}

export interface Finding {
  id: string;
  title: string;
  description: string;
  severity: Severity;
  icon: string;            // emoji
}

export interface TimelineEvent {
  ts: number;
  label: string;
  type: 'detection' | 'alert' | 'action' | 'system';
}

export interface NotificationLog {
  ts: number;
  channel: string;         // "sms" | "email" | "push"
  recipient: string;
  status: 'sent' | 'failed';
}

export interface Comment {
  id: string;
  author: string;
  text: string;
  ts: number;
}

export interface Keyframe {
  ts: number;
  thumbnail_url: string;
  is_key: boolean;
}

// ── Detections ──

export interface Detection {
  class: string;           // "person" | "hard_hat" | "excavator" ...
  subclass?: string;       // "no_hard_hat" | "no_harness" ...
  confidence: number;      // 0-1
  bbox: [number, number, number, number]; // [x1, y1, x2, y2] pixels
  track_id?: number;
  in_exclusion_zone: boolean;
  violation: boolean;
}

export interface WorkerPPE {
  worker_id: string;
  name: string;
  hard_hat: boolean;
  harness: boolean;
  hi_vis: boolean;
  boots: boolean;
  gloves: boolean;
  in_zone: boolean;
}

// ── Search ──

export interface SearchResult {
  frame_id: string;
  site_id: string;
  site_name: string;
  camera_id: string;
  camera_name: string;
  ts: number;
  relevance_score: number; // 0-1
  caption: string;
  thumbnail_url: string;
  clip_url: string;
  detections: Detection[];
  violation_flags: Record<string, boolean>;
  token_matches: TokenMatch[];
}

export interface TokenMatch {
  token: string;
  score: number;
  source: 'visual' | 'caption';
}

export interface SearchFilters {
  query: string;
  violation_types?: string[];
  site_ids?: string[];
  date_range?: { start: string; end: string };
  confidence_min?: number;
  time_of_day?: { start: string; end: string };
  model?: 'hybrid' | 'visual' | 'caption';
}

// ── Reports ──

export interface ReportRequest {
  type: 'safety' | 'ppe' | 'incidents' | 'equipment';
  site_id?: string;
  date_range: { start: string; end: string };
}

export interface ReportStatus {
  id: string;
  status: 'pending' | 'generating' | 'complete' | 'failed';
  download_url?: string;
  created_at: string;
}

// ── Companies ──
// A company is the top-level org. It owns users and sites.

export interface Company {
  id: string;
  name: string;
  logo_url?: string;
  plan: 'starter' | 'professional' | 'enterprise';
  contact_name: string;
  contact_email: string;
  created_at: string;
}

// ── Company Users ──
// Users belong to a company. They can be assigned to one or more of that company's sites.

export interface CompanyUser {
  id: string;
  company_id: string;
  name: string;
  email: string;
  phone?: string;
  role: 'admin' | 'safety_manager' | 'supervisor' | 'viewer';
  avatar_url?: string;
  assigned_site_ids: string[];   // sites this user has access to
  created_at: string;
}

// ── Site Management ──
// Sites belong to a company.

export interface SiteCreate {
  name: string;
  address: string;
  company_id: string;          // required — every site belongs to a company
  latitude?: number;
  longitude?: number;
}

// ── Site User Assignment ──
// Which users (from the owning company) are assigned to a site.

export interface SiteUserAssignment {
  site_id: string;
  user_id: string;
  user_name: string;
  user_email: string;
  role: 'admin' | 'safety_manager' | 'supervisor' | 'viewer';
  assigned_at: string;
}

// ── Camera Assignment ──
// Links an IRONSight camera to a Ironsight site.

export interface CameraAssignment {
  site_id: string;
  camera_id: string;         // IRONSight camera UUID
  camera_name: string;
  location_label: string;    // "North Perimeter", "Crane Zone", etc.
  assigned_at: string;
}

// ── IRONSight Camera (from existing NVR system) ──

export interface IRONSightCamera {
  id: string;
  name: string;
  site_id?: string | null;    // null = unassigned
  location?: string;          // site-scoped alias (shown to operators/customers)
  onvif_address: string;      // admin-only: physical identity
  status: string;
  manufacturer: string;
  model: string;
  rtsp_uri?: string;
  stream_url?: string;
  recording: boolean;
}

/** Speaker with platform-layer site assignment info */
export interface PlatformSpeaker {
  id: string;
  name: string;
  onvif_address: string;  // admin-only
  zone: string;           // original zone name
  location: string;       // site-scoped alias
  status: string;
  site_id?: string | null;
  manufacturer: string;
  model: string;
}

// ── Site SOPs (Standard Operating Procedures) ──
// Each site has one or more SOPs defining how operators should respond to events.

export interface SiteSOP {
  id: string;
  site_id: string;
  title: string;                // "Fire Alarm Response", "Unauthorized Access", etc.
  category: 'emergency' | 'safety' | 'access' | 'equipment' | 'general';
  priority: 'critical' | 'high' | 'normal';
  steps: string[];              // ordered procedure steps
  contacts: SOPContact[];       // who to notify
  updated_at: string;           // ISO timestamp
  updated_by: string;           // who last edited
}

export interface SOPContact {
  name: string;
  role: string;
  phone?: string;
  email?: string;
}

// ── Site Map ──
// An uploaded site layout image with camera markers pinned to specific locations.

export interface SiteMapData {
  site_id: string;
  image_url: string;            // URL to the uploaded floor plan / aerial image
  image_width: number;          // native image width in px
  image_height: number;         // native image height in px
  markers: SiteMapMarker[];
  updated_at: string;
}

export interface SiteMapMarker {
  id: string;
  camera_id: string;            // links to CameraAssignment.camera_id
  camera_name: string;
  x: number;                    // position as percentage (0-100) of image width
  y: number;                    // position as percentage (0-100) of image height
  rotation?: number;            // camera FOV direction in degrees
  fov_angle?: number;           // field-of-view cone angle
  label: string;                // "North Gate", "Loading Dock", etc.
}

// ════════════════════════════════════════════════════════════════
// Complete Platform Types — SOC Operations, Analytics, Integrations
// ════════════════════════════════════════════════════════════════

// ── Shift Handoff ──
// Operator-to-operator transfer at end of shift.

export interface ShiftHandoff {
  id: string;
  from_operator_id: string;
  from_operator_callsign: string;
  to_operator_id: string;
  to_operator_callsign: string;
  locked_site_ids: string[];     // sites being transferred
  active_alert_ids: string[];    // alerts being transferred
  notes: string;                 // free-text context for incoming operator
  status: 'pending' | 'accepted' | 'declined';
  created_at: string;            // ISO timestamp
  accepted_at?: string;
}

// ── Escalation Rules ──
// Per-severity auto-escalation timers attached to SOPs.

export interface EscalationRule {
  id: string;
  site_id: string;
  severity: Severity;
  acknowledge_timeout_ms: number;  // time before auto-escalation
  escalation_chain: EscalationStep[];
}

export interface EscalationStep {
  level: number;                  // 1, 2, 3...
  label: string;                  // "Supervisor", "Site Manager", "Emergency"
  timeout_ms: number;             // time at this level before next
  notify_channels: ('sms' | 'email' | 'push' | 'pa')[];
  notify_contacts: string[];      // names or IDs
}

// ── Audit Trail ──
// Every operator action logged for compliance.

export type AuditAction =
  | 'alert_claimed' | 'alert_released' | 'alert_acknowledged' | 'alert_escalated'
  | 'site_locked' | 'site_unlocked'
  | 'shift_handoff_created' | 'shift_handoff_accepted'
  | 'sop_viewed' | 'incident_created' | 'incident_updated'
  | 'evidence_exported' | 'ptz_command' | 'zone_edited'
  | 'login' | 'logout';

export interface AuditEntry {
  id: string;
  action: AuditAction;
  operator_id: string;
  operator_callsign: string;
  entity_type: 'alert' | 'site' | 'incident' | 'sop' | 'shift' | 'system';
  entity_id: string;
  metadata?: Record<string, string>; // extra context
  ts: number;                        // Unix ms
}

// ── Operator Presence ──
// Real-time awareness of what each operator is viewing.

export interface OperatorPresence {
  operator_id: string;
  operator_callsign: string;
  status: 'on_shift' | 'off_shift' | 'break';
  viewing_site_id?: string;
  viewing_camera_id?: string;
  last_seen: number;                 // Unix ms
}

// ── SLA Configuration ──
// Per-severity response time thresholds.

export interface SLAConfig {
  severity: Severity;
  acknowledge_sla_ms: number;        // time to acknowledge
  resolve_sla_ms: number;            // time to resolve
  label: string;                     // "Critical: 30s ack / 5m resolve"
}

// ── Operator Metrics ──
// Performance analytics for SOC operators.

export interface OperatorMetrics {
  operator_id: string;
  operator_callsign: string;
  operator_name: string;
  shift_hours: number;
  events_handled: number;
  avg_response_time_ms: number;
  avg_resolve_time_ms: number;
  sla_compliance_pct: number;        // 0-100
  alerts_claimed: number;
  alerts_escalated: number;
  sites_monitored: number;
}

// ── Scheduled Reports ──
// Automated report delivery to customers.

export interface ScheduledReport {
  id: string;
  name: string;
  type: 'safety' | 'compliance' | 'incidents' | 'executive';
  frequency: 'daily' | 'weekly' | 'monthly';
  site_ids: string[];                // which sites to include
  recipients: string[];              // email addresses
  next_run: string;                  // ISO timestamp
  last_run?: string;
  enabled: boolean;
}

// ── Evidence Package ──
// Bundled incident evidence for export.

export interface EvidencePackage {
  id: string;
  incident_id: string;
  site_name: string;
  generated_at: string;
  generated_by: string;
  clips: { url: string; label: string; ts: number }[];
  screenshots: { url: string; label: string; ts: number }[];
  timeline: TimelineEvent[];
  operator_notes: string;
  sop_steps_taken: string[];
  status: 'generating' | 'ready' | 'expired';
  download_url?: string;
}

// ── Notification Rules ──
// Per-site customer notification configuration.

export interface NotificationRule {
  id: string;
  site_id: string;
  name: string;
  severity_filter: Severity[];       // which severities trigger
  channels: ('sms' | 'email' | 'webhook' | 'push')[];
  recipients: NotificationRecipient[];
  schedule: 'immediate' | 'digest_hourly' | 'digest_daily';
  enabled: boolean;
}

export interface NotificationRecipient {
  name: string;
  email?: string;
  phone?: string;
  webhook_url?: string;
}

// ── PTZ Camera Controls ──
// Pan-Tilt-Zoom capability and presets.

export interface PTZCapability {
  camera_id: string;
  supports_ptz: boolean;
  supports_presets: boolean;
  presets: PTZPreset[];
}

export interface PTZPreset {
  id: string;
  name: string;                      // "Gate Overview", "Loading Dock Close-up"
  pan: number;                       // -180 to 180
  tilt: number;                      // -90 to 90
  zoom: number;                      // 1x to 30x
}

// ── Exclusion Zones ──
// Geofenced areas that trigger alerts when breached.

export interface ExclusionZone {
  id: string;
  site_id: string;
  name: string;
  polygon: [number, number][];       // array of [x%, y%] points
  severity: Severity;
  active: boolean;
  created_at: string;
  schedule?: { start_hour: number; end_hour: number }; // optional time window
}

// ── Saved Searches ──
// Reusable semantic search queries.

export interface SavedSearch {
  id: string;
  name: string;
  query: string;
  filters: Partial<SearchFilters>;
  created_by: string;
  shared: boolean;                   // visible to all operators
  created_at: string;
  run_count: number;
}

// ── Integrations ──
// Third-party service connections.

export type IntegrationType = 'webhook' | 'slack' | 'teams' | 'api_key';

export interface Integration {
  id: string;
  type: IntegrationType;
  name: string;                      // "Slack #construction-alerts"
  config: WebhookConfig | SlackConfig | APIKeyConfig;
  site_ids: string[];                // scoped to specific sites, or [] for all
  active: boolean;
  created_at: string;
  last_triggered?: string;
}

export interface WebhookConfig {
  url: string;
  secret?: string;
  events: string[];                  // ["alert.created", "incident.escalated"]
}

export interface SlackConfig {
  workspace: string;
  channel: string;
  bot_token?: string;
}

export interface APIKeyConfig {
  key_prefix: string;                // "sg_live_abc..." (first 12 chars)
  permissions: ('read' | 'write' | 'admin')[];
  expires_at?: string;
}
