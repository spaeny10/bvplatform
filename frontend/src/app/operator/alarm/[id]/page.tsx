'use client';

import { useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useOperatorStore } from '@/stores/operator-store';
import ActiveAlarmView from '@/components/operator/ActiveAlarmView';
import { useAlerts } from '@/hooks/useAlerts';
import { getIncidentDetail } from '@/lib/ironsight-api';
import type { SOCIncident, AlertEvent } from '@/types/ironsight';

export default function AlarmInvestigationPage({ params }: { params: { id: string } }) {
  const router = useRouter();
  const activeAlarm = useOperatorStore(s => s.activeAlarm);
  const alertFeed = useOperatorStore(s => s.alertFeed);
  const engageAlarm = useOperatorStore(s => s.engageAlarm);
  const { data: restAlerts = [] } = useAlerts();

  // Incident-aware state
  const [incident, setIncident] = useState<SOCIncident | null>(null);
  const [childAlarms, setChildAlarms] = useState<AlertEvent[] | null>(null);
  const [incidentLoading, setIncidentLoading] = useState(false);

  const isIncidentId = params.id.startsWith('INC-');

  // If the ID is an incident ID (INC-*), fetch incident + child alarms
  useEffect(() => {
    if (!isIncidentId) {
      setIncident(null);
      setChildAlarms(null);
      return;
    }

    setIncidentLoading(true);
    getIncidentDetail(params.id)
      .then(({ incident: inc, alarms }) => {
        setIncident(inc);
        // Map backend alarm shape to AlertEvent
        const mapped: AlertEvent[] = alarms.map(a => ({
          id: a.id,
          incident_id: a.incident_id,
          site_id: a.site_id,
          site_name: a.site_name ?? inc.site_name,
          camera_id: a.camera_id,
          camera_name: a.camera_name,
          severity: a.severity as AlertEvent['severity'],
          type: a.type,
          description: a.description,
          snapshot_url: a.snapshot_url,
          clip_url: a.clip_url,
          ts: a.ts,
          acknowledged: a.acknowledged ?? false,
          escalation_level: a.escalation_level ?? 0,
          sla_deadline_ms: a.sla_deadline_ms,
          // AI pipeline fields
          ai_description: (a as any).ai_description,
          ai_threat_level: (a as any).ai_threat_level,
          ai_recommended_action: (a as any).ai_recommended_action,
          ai_false_positive_pct: (a as any).ai_false_positive_pct,
          ai_score: (a as any).ai_score,
          ai_detections: (a as any).ai_detections,
          ai_ppe_violations: (a as any).ai_ppe_violations,
          obj_type: (a as any).obj_type,
          rule_name: (a as any).rule_name,
        }));
        setChildAlarms(mapped);

        // Engage the latest alarm for backward compatibility
        const latest = mapped.length > 0
          ? mapped.reduce((a, b) => (b.ts > a.ts ? b : a), mapped[0])
          : null;
        if (latest && (!activeAlarm || activeAlarm.id !== latest.id)) {
          engageAlarm(latest);
        }
      })
      .catch(() => {
        setIncident(null);
        setChildAlarms(null);
      })
      .finally(() => setIncidentLoading(false));
  }, [params.id, isIncidentId]);

  // For non-incident IDs: original alarm-finding logic
  useEffect(() => {
    if (isIncidentId) return;
    if (activeAlarm && activeAlarm.id === params.id) return; // already engaged

    // Merge REST + WS -- REST wins for existing IDs
    const byId = new Map([...alertFeed, ...restAlerts].map(a => [a.id, a]));
    restAlerts.forEach(a => byId.set(a.id, a));
    const found = byId.get(params.id);
    if (found) {
      engageAlarm(found);
    }
  }, [params.id, isIncidentId, activeAlarm, alertFeed, restAlerts, engageAlarm]);

  // Determine the alarm to display
  const alarm = isIncidentId
    ? activeAlarm  // set by the incident fetch above
    : (activeAlarm?.id === params.id ? activeAlarm : null);

  const handleResolved = () => {
    router.push('/operator');
  };

  // Loading / not found
  if (!alarm || (isIncidentId && incidentLoading)) {
    return (
      <div style={{
        height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center',
        flexDirection: 'column', gap: 12,
        background: 'var(--bg-primary, #0A0C10)', color: 'var(--text-muted, #4A5268)',
      }}>
        <div style={{ fontSize: 32, opacity: 0.3 }}>🔍</div>
        <div style={{ fontSize: 13 }}>
          {isIncidentId ? `Loading incident ${params.id}...` : `Loading alarm ${params.id}...`}
        </div>
        <button
          onClick={() => router.push('/operator')}
          style={{
            marginTop: 8, padding: '6px 16px', borderRadius: 4, fontSize: 11, fontWeight: 600,
            background: 'rgba(232,115,42,0.1)', border: '1px solid rgba(232,115,42,0.25)',
            color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
          }}
        >
          ← Back to SOC Monitor
        </button>
      </div>
    );
  }

  return (
    <ActiveAlarmView
      alarm={alarm}
      incident={incident ?? undefined}
      childAlarms={childAlarms ?? undefined}
      onResolved={handleResolved}
    />
  );
}
