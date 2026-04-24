// ── Operator Console Store ──
// Global UI state for the SOC operator monitoring workflow.
// Supports the Directed Dispatch model with operator presence and active alarm state.

import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { AlertEvent, SOCIncident, SOCOperator, SiteLock } from '@/types/ironsight';

export type LayoutMode = '2x3' | '1x1' | '3x3';
export type OperatorStatus = 'available' | 'engaged' | 'wrap_up' | 'away' | 'break';

// ── Action log entry for the active alarm disposition workflow ──
export interface ActionLogEntry {
  ts: number;
  text: string;
  auto?: boolean; // true for system-generated entries (quick-log buttons)
}

// ── Disposition codes for closing a security alarm ──
export type DispositionCode =
  | 'false_positive_animal'
  | 'false_positive_weather'
  | 'false_positive_shadow'
  | 'false_positive_equipment'
  | 'false_positive_other'
  | 'verified_customer_notified'
  | 'verified_police_dispatched'
  | 'verified_guard_responded'
  | 'verified_no_threat'
  | 'verified_other';

export const DISPOSITION_OPTIONS: { code: DispositionCode; label: string; category: 'false' | 'verified' }[] = [
  { code: 'false_positive_animal', label: 'False Positive — Animal', category: 'false' },
  { code: 'false_positive_weather', label: 'False Positive — Weather', category: 'false' },
  { code: 'false_positive_shadow', label: 'False Positive — Shadow/Light', category: 'false' },
  { code: 'false_positive_equipment', label: 'False Positive — Equipment', category: 'false' },
  { code: 'false_positive_other', label: 'False Positive — Other', category: 'false' },
  { code: 'verified_customer_notified', label: 'Verified — Customer Notified', category: 'verified' },
  { code: 'verified_police_dispatched', label: 'Verified — Police Dispatched', category: 'verified' },
  { code: 'verified_guard_responded', label: 'Verified — Guard Responded', category: 'verified' },
  { code: 'verified_no_threat', label: 'Verified — No Active Threat', category: 'verified' },
  { code: 'verified_other', label: 'Verified — Other', category: 'verified' },
];

// ── Snapshot of the last resolved alarm — drives WrapUpOverlay + EvidenceExport ──
export interface LastDisposition {
  alarm: AlertEvent;
  dispositionCode: DispositionCode;
  actionLog: ActionLogEntry[];
  resolvedAt: number;
  responseMs: number; // ms from alarm ts to resolution
}

// ── Per-shift performance metrics ──
export interface ShiftStats {
  alarmsHandled: number;
  slaMissed: number;          // alarms resolved after SLA deadline
  shiftStartMs: number;
}

interface OperatorState {
  // ── Current operator identity ──
  currentOperator: SOCOperator | null;
  setCurrentOperator: (op: SOCOperator) => void;

  // ── Operator status (for directed dispatch routing) ──
  operatorStatus: OperatorStatus;
  setOperatorStatus: (status: OperatorStatus) => void;

  // ── Site selection (for idle grid view) ──
  selectedSiteId: string | null;
  selectSite: (id: string) => void;

  // ── Site locking (advisory) ──
  siteLocks: Record<string, SiteLock>;
  initLocks: (locks: SiteLock[]) => void;
  lockSite: (siteId: string) => void;
  unlockSite: (siteId: string) => void;
  getSiteLock: (siteId: string) => SiteLock | null;
  isMySiteLock: (siteId: string) => boolean;

  // ── Incident feed (groups alarms from same site within correlation window) ──
  incidents: SOCIncident[];
  upsertIncident: (incident: SOCIncident) => void;
  acknowledgeIncident: (id: string) => void;

  // ── Alert feed (WebSocket-driven, individual alarms within incidents) ──
  alertFeed: AlertEvent[];
  addAlert: (alert: AlertEvent) => void;
  acknowledgeAlert: (id: string) => void;
  clearAlerts: () => void;

  // ── Locally dismissed alarm IDs ──
  // Prevents REST polling from reviving alarms the operator has already resolved.
  dismissedIds: string[];
  dismissAlarm: (id: string) => void;

  // ── Snapshot patch (async delivery after alarm creation) ──
  patchAlertSnapshot: (alarmId: string, snapshotUrl: string) => void;

  // ── AI enrichment patch (YOLO + Qwen results, async after snapshot) ──
  patchAlertAI: (alarmId: string, data: {
    ai_description?: string;
    ai_threat_level?: string;
    ai_recommended_action?: string;
    ai_false_positive_pct?: number;
    ai_objects?: { type: string; attributes: Record<string, unknown> }[];
    ai_detections?: { class: string; confidence: number; bbox_normalized: { x1: number; y1: number; x2: number; y2: number } }[];
    ai_ppe_violations?: { class: string; confidence: number; bbox_normalized: { x1: number; y1: number; x2: number; y2: number } }[];
  }) => void;

  // ── Alert ownership ──
  claimAlert: (alertId: string) => void;
  releaseAlert: (alertId: string) => void;

  // ── Escalation ──
  escalateActiveAlarm: () => void;

  // ── Active Alarm (the claimed alarm being investigated) ──
  activeAlarm: AlertEvent | null;
  actionLog: ActionLogEntry[];
  engageAlarm: (alert: AlertEvent) => void;
  addActionLogEntry: (text: string, auto?: boolean) => void;
  resolveAlarm: (dispositionCode: DispositionCode) => void;
  abandonAlarm: () => void;

  // ── Wrap-up state ──
  lastDisposition: LastDisposition | null;
  exitWrapUp: () => void;

  // ── Shift performance ──
  shiftStats: ShiftStats;

  // ── Alarm queue metrics ──
  queueDepth: number;
  setQueueDepth: (n: number) => void;

  // ── Site filter ──
  siteFilter: string | null;
  setSiteFilter: (filter: string | null) => void;

  // ── Camera grid layout mode ──
  layoutMode: LayoutMode;
  setLayoutMode: (mode: LayoutMode) => void;

  // ── Audio mute ──
  audioMuted: boolean;
  setAudioMuted: (v: boolean) => void;
}

export const useOperatorStore = create<OperatorState>()(
  persist(
    (set, get) => ({
      // ── Current operator ──
      currentOperator: null,
      setCurrentOperator: (op) => set({ currentOperator: op }),

      // ── Operator status ──
      operatorStatus: 'available',
      setOperatorStatus: (status) => {
        set({ operatorStatus: status });
        const op = get().currentOperator;
        if (op) {
          // Map workflow states to presence status visible to other operators
          const presenceStatus: 'on_shift' | 'break' | 'off_shift' =
            status === 'break' ? 'break'
            : status === 'away' ? 'off_shift'
            : 'on_shift';
          import('@/lib/ironsight-api').then(({ updatePresence }) => {
            updatePresence({ operator_id: op.id, operator_callsign: op.callsign, status: presenceStatus });
          });
        }
      },

      // ── Site selection ──
      selectedSiteId: null,
      selectSite: (id) => set({ selectedSiteId: id }),

      // ── Site locking ──
      siteLocks: {},
      initLocks: (locks) => {
        if (!Array.isArray(locks)) return;
        set({ siteLocks: Object.fromEntries(locks.map((l) => [l.site_id, l])) });
      },
      lockSite: (siteId) => {
        const op = get().currentOperator;
        if (!op) return;
        set((state) => ({
          siteLocks: {
            ...state.siteLocks,
            [siteId]: {
              site_id: siteId,
              operator_id: op.id,
              operator_callsign: op.callsign,
              locked_at: new Date().toISOString(),
            },
          },
        }));
      },
      unlockSite: (siteId) =>
        set((state) => {
          const next = { ...state.siteLocks };
          delete next[siteId];
          return { siteLocks: next };
        }),
      getSiteLock: (siteId) => get().siteLocks[siteId] || null,
      isMySiteLock: (siteId) => {
        const lock = get().siteLocks[siteId];
        const op = get().currentOperator;
        return !!lock && !!op && lock.operator_id === op.id;
      },

      // ── Incident feed ──
      incidents: [],
      upsertIncident: (incident) =>
        set((state) => {
          const idx = state.incidents.findIndex((i) => i.id === incident.id);
          if (idx >= 0) {
            // Update existing incident in place
            const updated = [...state.incidents];
            updated[idx] = incident;
            return { incidents: updated };
          }
          return { incidents: [incident, ...state.incidents].slice(0, 200) };
        }),
      acknowledgeIncident: (id) =>
        set((state) => ({
          incidents: state.incidents.filter((i) => i.id !== id),
          alertFeed: state.alertFeed.map((a) =>
            a.incident_id === id ? { ...a, acknowledged: true } : a
          ),
          dismissedIds: [
            ...state.dismissedIds,
            ...state.alertFeed
              .filter((a) => a.incident_id === id)
              .map((a) => a.id),
          ].slice(-500),
        })),

      // ── Alert feed ──
      alertFeed: [],
      addAlert: (alert) =>
        set((state) => {
          // Don't re-add alerts the operator has already dismissed
          if (state.dismissedIds.includes(alert.id)) return state;
          return { alertFeed: [alert, ...state.alertFeed].slice(0, 200) };
        }),
      acknowledgeAlert: (id) =>
        set((state) => ({
          alertFeed: state.alertFeed.map((a) =>
            a.id === id ? { ...a, acknowledged: true } : a
          ),
          dismissedIds: state.dismissedIds.includes(id)
            ? state.dismissedIds
            : [...state.dismissedIds, id].slice(-500),
        })),
      clearAlerts: () => set({ alertFeed: [] }),

      // ── Snapshot patch ──
      patchAlertSnapshot: (alarmId, snapshotUrl) =>
        set((state) => {
          // Find the incident that owns this alarm and patch its snapshot too
          const alarm = state.alertFeed.find((a) => a.id === alarmId);
          const incidentId = alarm?.incident_id;
          return {
            alertFeed: state.alertFeed.map((a) =>
              a.id === alarmId ? { ...a, snapshot_url: snapshotUrl } : a
            ),
            incidents: incidentId
              ? state.incidents.map((i) =>
                  i.id === incidentId ? { ...i, snapshot_url: snapshotUrl } : i
                )
              : state.incidents,
            activeAlarm: state.activeAlarm?.id === alarmId
              ? { ...state.activeAlarm, snapshot_url: snapshotUrl }
              : state.activeAlarm,
          };
        }),

      // ── AI enrichment patch ──
      patchAlertAI: (alarmId, data) =>
        set((state) => ({
          alertFeed: state.alertFeed.map((a) =>
            a.id === alarmId ? { ...a, ...data } : a
          ),
          activeAlarm: state.activeAlarm?.id === alarmId
            ? { ...state.activeAlarm, ...data }
            : state.activeAlarm,
        })),

      // ── Locally dismissed alarms ──
      dismissedIds: [],
      dismissAlarm: (id) =>
        set((state) => ({
          dismissedIds: state.dismissedIds.includes(id)
            ? state.dismissedIds
            : [...state.dismissedIds, id].slice(-500),
        })),

      // ── Alert ownership ──
      claimAlert: (alertId) => {
        const op = get().currentOperator;
        if (!op) return;
        set((state) => ({
          alertFeed: state.alertFeed.map((a) =>
            a.id === alertId
              ? { ...a, assigned_operator_id: op.id, assigned_operator_callsign: op.callsign }
              : a
          ),
        }));
      },
      releaseAlert: (alertId) =>
        set((state) => ({
          alertFeed: state.alertFeed.map((a) =>
            a.id === alertId
              ? { ...a, assigned_operator_id: undefined, assigned_operator_callsign: undefined }
              : a
          ),
        })),

      // ── Escalation ──
      escalateActiveAlarm: () => {
        const alarm = get().activeAlarm;
        if (!alarm) return;
        const newLevel = (alarm.escalation_level || 0) + 1;
        const updatedAlarm = { ...alarm, escalation_level: newLevel, escalated_at: Date.now() };
        set((state) => ({
          activeAlarm: updatedAlarm,
          alertFeed: state.alertFeed.map((a) =>
            a.id === alarm.id ? updatedAlarm : a
          ),
        }));
        get().addActionLogEntry(`Escalated to Level ${newLevel}`, false);
        // Fire-and-forget API call
        import('@/lib/ironsight-api').then(({ escalateAlarm }) => {
          escalateAlarm(alarm.id, newLevel);
        });
      },

      // ── Active Alarm ──
      activeAlarm: null,
      actionLog: [],
      engageAlarm: (alert) => {
        const op = get().currentOperator;
        set({
          activeAlarm: alert,
          operatorStatus: 'engaged',
          actionLog: [{
            ts: Date.now(),
            text: `Alarm claimed by ${op?.callsign || 'operator'}`,
            auto: true,
          }],
        });
        get().claimAlert(alert.id);
      },
      addActionLogEntry: (text, auto = false) =>
        set((state) => ({
          actionLog: [...state.actionLog, { ts: Date.now(), text, auto }],
        })),

      resolveAlarm: (dispositionCode: DispositionCode) => {
        const alarm = get().activeAlarm;
        const actionLog = get().actionLog;
        const resolvedAt = Date.now();

        if (alarm) {
          // Immediately dismiss — prevents REST polling from reviving the alarm in the feed
          get().acknowledgeAlert(alarm.id);

          const responseMs = resolvedAt - alarm.ts;
          const slaMissed = alarm.sla_deadline_ms ? resolvedAt > alarm.sla_deadline_ms : false;

          // Save disposition snapshot for WrapUpOverlay + EvidenceExport
          const lastDisposition: LastDisposition = {
            alarm,
            dispositionCode,
            actionLog,
            resolvedAt,
            responseMs,
          };

          // NOTE: the API call to createSecurityEvent is handled by ActiveAlarmView.handleSubmit
          // before calling resolveAlarm, so we don't duplicate it here.

          set((state) => ({
            activeAlarm: null,
            actionLog: [],
            operatorStatus: 'wrap_up',
            lastDisposition,
            shiftStats: {
              ...state.shiftStats,
              alarmsHandled: state.shiftStats.alarmsHandled + 1,
              slaMissed: state.shiftStats.slaMissed + (slaMissed ? 1 : 0),
            },
          }));
        } else {
          set({ activeAlarm: null, actionLog: [], operatorStatus: 'wrap_up' });
        }
      },

      abandonAlarm: () => {
        const alarm = get().activeAlarm;
        if (alarm) get().releaseAlert(alarm.id);
        set({ activeAlarm: null, actionLog: [], operatorStatus: 'available' });
      },

      // ── Wrap-up ──
      lastDisposition: null,
      exitWrapUp: () => set({ operatorStatus: 'available', lastDisposition: null }),

      // ── Shift stats ──
      shiftStats: {
        alarmsHandled: 0,
        slaMissed: 0,
        shiftStartMs: Date.now(),
      },

      // ── Queue metrics ──
      queueDepth: 0,
      setQueueDepth: (n) => set({ queueDepth: n }),

      // ── Site filter ──
      siteFilter: null,
      setSiteFilter: (filter) => set({ siteFilter: filter }),

      // ── Layout ──
      layoutMode: '2x3',
      setLayoutMode: (mode) => set({ layoutMode: mode }),

      // ── Audio ──
      audioMuted: false,
      setAudioMuted: (v) => set({ audioMuted: v }),
    }),
    {
      name: 'sg-operator-store',
      partialize: (state) => ({
        // currentOperator intentionally excluded — re-derived from JWT on load
        selectedSiteId: state.selectedSiteId,
        siteLocks: state.siteLocks,
        layoutMode: state.layoutMode,
        siteFilter: state.siteFilter,
        audioMuted: state.audioMuted,
        shiftStats: state.shiftStats,
        dismissedIds: state.dismissedIds,
      }),
    }
  )
);
