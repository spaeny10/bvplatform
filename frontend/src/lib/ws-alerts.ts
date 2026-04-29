// ── WebSocket Alert Stream Manager ──
// Connects to /ws/alerts and streams real-time AlertEvent objects.
// Implements exponential backoff reconnect with connection state tracking.

import type { AlertEvent, SOCIncident } from '@/types/ironsight';

export type AlertHandler = (alert: AlertEvent) => void;
export type IncidentHandler = (incident: SOCIncident, isNew: boolean) => void;
export type SnapshotHandler = (alarmId: string, snapshotUrl: string) => void;
export interface AIEnrichment {
  alarm_id: string;
  incident_id: string;
  ai_description: string;
  ai_threat_level: string;
  ai_recommended_action: string;
  ai_false_positive_pct: number;
  ai_objects: { type: string; attributes: Record<string, unknown> }[];
  ai_detections: { class: string; confidence: number; bbox_normalized: { x1: number; y1: number; x2: number; y2: number } }[];
  ai_ppe_violations?: { class: string; confidence: number; bbox_normalized: { x1: number; y1: number; x2: number; y2: number } }[];
}
export type AIHandler = (data: AIEnrichment) => void;
export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected' | 'reconnecting';
export type StatusHandler = (status: ConnectionStatus) => void;

export class AlertStream {
  private ws: WebSocket | null = null;
  private handlers: AlertHandler[] = [];
  private incidentHandlers: IncidentHandler[] = [];
  private snapshotHandlers: SnapshotHandler[] = [];
  private aiHandlers: AIHandler[] = [];
  private statusHandlers: StatusHandler[] = [];
  private reconnectDelay = 3000;
  private maxReconnectDelay = 30000;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private destroyed = false;
  private url: string;
  private _status: ConnectionStatus = 'disconnected';
  private reconnectAttempts = 0;

  constructor(wsUrl?: string) {
    this.url = wsUrl || `ws://${typeof window !== 'undefined' ? window.location.hostname : 'localhost'}:8080/ws/alerts`;
  }

  /** Current connection status */
  get status(): ConnectionStatus {
    return this._status;
  }

  /** Subscribe to incoming alerts. Returns an unsubscribe function. */
  onAlert(handler: AlertHandler): () => void {
    this.handlers.push(handler);
    return () => {
      this.handlers = this.handlers.filter(h => h !== handler);
    };
  }

  /** Subscribe to alarm_ai messages (YOLO + Qwen enrichment). */
  onAI(handler: AIHandler): () => void {
    this.aiHandlers.push(handler);
    return () => {
      this.aiHandlers = this.aiHandlers.filter(h => h !== handler);
    };
  }

  /** Subscribe to incident_new / incident_update messages. */
  onIncident(handler: IncidentHandler): () => void {
    this.incidentHandlers.push(handler);
    return () => {
      this.incidentHandlers = this.incidentHandlers.filter(h => h !== handler);
    };
  }

  /** Subscribe to alarm_snapshot messages (snapshot ready after async capture). */
  onSnapshot(handler: SnapshotHandler): () => void {
    this.snapshotHandlers.push(handler);
    return () => {
      this.snapshotHandlers = this.snapshotHandlers.filter(h => h !== handler);
    };
  }

  /** Subscribe to connection status changes. */
  onStatusChange(handler: StatusHandler): () => void {
    this.statusHandlers.push(handler);
    // Immediately fire current status
    handler(this._status);
    return () => {
      this.statusHandlers = this.statusHandlers.filter(h => h !== handler);
    };
  }

  /** Start the WebSocket connection. */
  connect(): void {
    if (this.destroyed) return;
    this.setStatus(this.reconnectAttempts > 0 ? 'reconnecting' : 'connecting');

    const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : null;
    const urlWithToken = token ? `${this.url}?token=${token}` : this.url;

    try {
      this.ws = new WebSocket(urlWithToken);
    } catch {
      this.setStatus('disconnected');
      this.scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this.reconnectDelay = 3000; // reset backoff
      this.reconnectAttempts = 0;
      this.setStatus('connected');
    };

    this.ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === 'alert' && msg.data) {
          this.handlers.forEach(h => h(msg.data as AlertEvent));
        }
        if ((msg.type === 'incident_new' || msg.type === 'incident_update') && msg.data) {
          this.incidentHandlers.forEach(h => h(msg.data as SOCIncident, msg.type === 'incident_new'));
        }
        if (msg.type === 'alarm_snapshot' && msg.data) {
          this.snapshotHandlers.forEach(h => h(msg.data.alarm_id as string, msg.data.snapshot_url as string));
        }
        if (msg.type === 'alarm_ai' && msg.data) {
          this.aiHandlers.forEach(h => h(msg.data as AIEnrichment));
        }
      } catch {
        // Ignore parse errors
      }
    };

    this.ws.onclose = () => {
      this.ws = null;
      this.setStatus('disconnected');
      this.scheduleReconnect();
    };

    this.ws.onerror = () => {
      // onclose will fire and handle reconnect
    };
  }

  /** Stop and clean up. */
  destroy(): void {
    this.destroyed = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    if (this.ws) this.ws.close();
    this.handlers = [];
    this.incidentHandlers = [];
    this.snapshotHandlers = [];
    this.aiHandlers = [];
    this.statusHandlers = [];
  }

  private setStatus(status: ConnectionStatus): void {
    this._status = status;
    this.statusHandlers.forEach(h => h(status));
  }

  private scheduleReconnect(): void {
    if (this.destroyed) return;
    this.reconnectAttempts++;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
      this.connect();
    }, this.reconnectDelay);
  }
}
