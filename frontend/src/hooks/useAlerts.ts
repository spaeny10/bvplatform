// ── Alerts React Query Hook + WebSocket Stream ──

import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { getAlerts, getActiveIncidents } from '@/lib/ironsight-api';
import { AlertStream } from '@/lib/ws-alerts';
import type { ConnectionStatus } from '@/lib/ws-alerts';
import { useOperatorStore } from '@/stores/operator-store';
import type { AlertEvent } from '@/types/ironsight';

/** REST-based alert polling (used by portal, not operator) */
export function useAlerts(filters?: {
  site_id?: string;
  severity?: string;
  limit?: number;
}) {
  return useQuery<AlertEvent[]>({
    queryKey: ['alerts', filters],
    queryFn: () => getAlerts(filters),
    refetchInterval: 15_000,
    staleTime: 5_000,
  });
}

/**
 * WebSocket-backed alert stream for the operator console.
 * Pushes incoming alerts into the Zustand operator store.
 * Returns the current WebSocket connection status.
 */
export function useAlertStream(): { wsStatus: ConnectionStatus } {
  const addAlert = useOperatorStore((s) => s.addAlert);
  const upsertIncident = useOperatorStore((s) => s.upsertIncident);
  const patchAlertSnapshot = useOperatorStore((s) => s.patchAlertSnapshot);
  const patchAlertAI = useOperatorStore((s) => s.patchAlertAI);
  const streamRef = useRef<AlertStream | null>(null);
  const [wsStatus, setWsStatus] = useState<ConnectionStatus>('disconnected');

  useEffect(() => {
    // Seed the store with existing active incidents on mount
    getActiveIncidents().then((incidents) => {
      incidents.forEach((inc) => upsertIncident(inc));
    });

    const stream = new AlertStream();
    streamRef.current = stream;

    stream.onAlert((alert) => {
      addAlert(alert);
    });

    stream.onIncident((incident) => {
      upsertIncident(incident);
    });

    stream.onSnapshot((alarmId, snapshotUrl) => {
      patchAlertSnapshot(alarmId, snapshotUrl);
    });

    stream.onAI((data) => {
      patchAlertAI(data.alarm_id, {
        ai_description: data.ai_description,
        ai_threat_level: data.ai_threat_level,
        ai_recommended_action: data.ai_recommended_action,
        ai_false_positive_pct: data.ai_false_positive_pct,
        ai_objects: data.ai_objects,
        ai_detections: data.ai_detections,
        ai_ppe_violations: data.ai_ppe_violations,
      });
    });

    stream.onStatusChange((status) => {
      setWsStatus(status);
    });

    stream.connect();

    return () => {
      stream.destroy();
      streamRef.current = null;
    };
  }, [addAlert, upsertIncident, patchAlertSnapshot, patchAlertAI]);

  return { wsStatus };
}
