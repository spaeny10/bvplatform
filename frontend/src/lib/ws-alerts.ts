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
    // Default to same-origin WebSocket. Proto follows page scheme so HTTPS
    // pages correctly upgrade to wss, host follows window.location so
    // reverse-proxy deployments don't need a hardcoded port.
    if (wsUrl) {
      this.url = wsUrl;
    } else if (typeof window !== 'undefined') {
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      this.url = `${proto}//${window.location.host}/ws/alerts`;
    } else {
      this.url = 'ws://localhost:8080/ws/alerts';
    }
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

  /** Start the WebSocket connection.
   *
   * P1-A-02 part 2: the session JWT is no longer in localStorage.
   * We fetch a short-lived WS ticket from GET /api/auth/ws-ticket
   * (cookie auto-attached via credentials:'include') and pass it as
   * ?ticket=<wsTicket> on the upgrade URL. This matches the P1-A-02-part1
   * design: the 5-min ticket limits blast radius if the URL leaks.
   */
  connect(): void {
    if (this.destroyed) return;
    this.setStatus(this.reconnectAttempts > 0 ? 'reconnecting' : 'connecting');

    // Fetch a WS ticket then open the socket. The ticket endpoint is
    // GET (CSRF-exempt) behind RequireAuth (cookie accepted).
    if (typeof window === 'undefined') return;
    fetch('/api/auth/ws-ticket', { credentials: 'include' })
      .then(res => {
        if (!res.ok) throw new Error(`ws-ticket: ${res.status}`);
        return res.json() as Promise<{ ticket: string }>;
      })
      .then(({ ticket }) => {
        if (this.destroyed) return;
        const url = `${this.url}?ticket=${encodeURIComponent(ticket)}`;
        this._openWS(url);
      })
      .catch(() => {
        // Ticket fetch failed (401, network error). Fall back to ticketless URL
        // so the SSO-path (X-Forwarded-Email) users still get a connection
        // attempt — the WS handler accepts SSO identity too.
        if (!this.destroyed) this._openWS(this.url);
      });
  }

  /** Open a WebSocket to the given URL and wire up the event handlers. */
  private _openWS(url: string): void {
    try {
      this.ws = new WebSocket(url);
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
