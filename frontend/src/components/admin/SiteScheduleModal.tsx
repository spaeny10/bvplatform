'use client';

import { useState, useEffect } from 'react';
import { useSite, useUpdateSite } from '@/hooks/useSites';
import type { MonitoringWindow } from '@/types/ironsight';
import ScheduleWindowsEditor, { Preset } from './ScheduleWindowsEditor';

const PRESETS: Preset[] = [
  {
    label: 'After-hours (Weeknights + Weekends)',
    windows: [
      { label: 'Weeknights', days: [1,2,3,4,5], start_time: '18:00', end_time: '06:00', enabled: true },
      { label: 'Weekends 24hr', days: [0,6], start_time: '00:00', end_time: '23:59', enabled: true },
    ],
  },
  {
    label: '24/7 Monitoring',
    windows: [
      { label: '24/7', days: [0,1,2,3,4,5,6], start_time: '00:00', end_time: '23:59', enabled: true },
    ],
  },
  {
    label: 'Weeknights only',
    windows: [
      { label: 'Weeknights', days: [1,2,3,4,5], start_time: '18:00', end_time: '06:00', enabled: true },
    ],
  },
];

interface Props {
  siteId: string;
  onClose: () => void;
  embedded?: boolean;
}

export default function SiteScheduleModal({ siteId, onClose, embedded }: Props) {
  const { data: site } = useSite(siteId);
  const updateSite = useUpdateSite();
  const [windows, setWindows] = useState<MonitoringWindow[]>([]);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (site?.monitoring_schedule) {
      setWindows(site.monitoring_schedule);
    }
  }, [site]);

  const handleSave = async () => {
    try {
      await updateSite.mutateAsync({
        id: siteId,
        data: { monitoring_schedule: windows } as any,
      });
      onClose();
    } catch { /* error displayed below */ }
  };

  const bodyContent = (
    <div>
      <ScheduleWindowsEditor
        windows={windows}
        onChange={next => { setWindows(next); setDirty(true); }}
        presets={PRESETS}
        accent="#a855f7"
        emptyHint="No monitoring windows configured. Use a preset or add a custom window."
      />

      {updateSite.isError && (
        <div style={{ padding: '10px 0', fontSize: 11, color: '#EF4444' }}>
          Failed to save schedule — check backend logs.
        </div>
      )}

      <div style={{ padding: '12px 0', borderTop: '1px solid rgba(255,255,255,0.06)', display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
        <button
          className="admin-btn admin-btn-primary"
          onClick={handleSave}
          disabled={!dirty || updateSite.isPending}
        >
          {updateSite.isPending ? 'Saving...' : 'Save Schedule'}
        </button>
      </div>
    </div>
  );

  if (embedded) return bodyContent;

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal" onClick={e => e.stopPropagation()} style={{ maxWidth: 600 }}>
        <div className="admin-modal-header">
          <div className="admin-modal-title">Monitoring Schedule{site ? ` — ${site.name}` : ''}</div>
          <button className="admin-modal-close" onClick={onClose}>x</button>
        </div>
        <div className="admin-modal-body">{bodyContent}</div>
        <div className="admin-modal-footer">
          <button className="admin-btn admin-btn-ghost" onClick={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  );
}
