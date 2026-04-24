'use client';

import { useMemo } from 'react';
import { MOCK_ALERTS, MOCK_INCIDENTS } from '@/lib/ironsight-mock';

interface TimelineEvent {
  id: string;
  ts: number;
  type: 'alert' | 'incident' | 'escalation' | 'handoff' | 'system';
  severity: 'critical' | 'high' | 'medium' | 'low' | 'info';
  title: string;
  site: string;
  camera?: string;
  icon: string;
}

const TYPE_COLORS: Record<string, string> = {
  alert: '#EF4444',
  incident: '#EF4444',
  escalation: '#a855f7',
  handoff: '#E8732A',
  system: '#4A5268',
};

const SEVERITY_COLORS: Record<string, string> = {
  critical: '#EF4444',
  high: '#EF4444',
  medium: '#E89B2A',
  low: '#E8732A',
  info: '#4A5268',
};

function buildTimeline(): TimelineEvent[] {
  const events: TimelineEvent[] = [];

  // Alerts
  MOCK_ALERTS.forEach(a => {
    events.push({
      id: `alert-${a.id}`,
      ts: a.ts,
      type: 'alert',
      severity: a.severity,
      title: a.description,
      site: a.site_name,
      camera: a.camera_name,
      icon: a.severity === 'critical' ? '🚨' : a.severity === 'high' ? '⚠️' : '📋',
    });

    // Add escalation events for escalated alerts
    if (a.escalation_level > 0 && a.escalated_at) {
      events.push({
        id: `esc-${a.id}`,
        ts: a.escalated_at,
        type: 'escalation',
        severity: a.severity,
        title: `Alert ${a.id} escalated to Level ${a.escalation_level}`,
        site: a.site_name,
        icon: '⚡',
      });
    }
  });

  // Incidents
  MOCK_INCIDENTS.forEach(inc => {
    events.push({
      id: `inc-${inc.id}`,
      ts: inc.ts,
      type: 'incident',
      severity: inc.severity,
      title: inc.title,
      site: inc.site_name,
      camera: inc.camera_id,
      icon: '📌',
    });
  });

  // System events (synthetic)
  events.push(
    { id: 'sys-shift', ts: Date.now() - 3600000, type: 'system', severity: 'info', title: 'Shift change: Day → Night', site: 'All Sites', icon: '🔄' },
    { id: 'sys-handoff', ts: Date.now() - 2700000, type: 'handoff', severity: 'info', title: 'OP-1 accepted site lock for TX-203', site: 'Southgate Power', icon: '🤝' },
    { id: 'sys-lock', ts: Date.now() - 2400000, type: 'system', severity: 'info', title: 'Site TX-203 locked by OP-1 (Falcon)', site: 'Southgate Power', icon: '🔒' },
  );

  return events.sort((a, b) => b.ts - a.ts);
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

function formatRelative(ts: number): string {
  const d = Date.now() - ts;
  if (d < 60000) return 'Just now';
  if (d < 3600000) return `${Math.floor(d / 60000)}m ago`;
  if (d < 86400000) return `${Math.floor(d / 3600000)}h ago`;
  return `${Math.floor(d / 86400000)}d ago`;
}

interface Props {
  maxEvents?: number;
}

export default function IncidentTimeline({ maxEvents = 30 }: Props) {
  const events = useMemo(buildTimeline, []);
  const displayed = events.slice(0, maxEvents);

  // Time range for the visual axis
  const minTs = displayed.length > 0 ? displayed[displayed.length - 1].ts : Date.now();
  const maxTs = displayed.length > 0 ? displayed[0].ts : Date.now();
  const range = maxTs - minTs || 1;

  return (
    <div style={{
      background: '#0E1117',
      border: '1px solid rgba(255,255,255,0.04)',
      borderRadius: 6,
      padding: 16,
    }}>
      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        marginBottom: 14,
      }}>
        <div>
          <div style={{
            fontSize: 11, fontWeight: 700, letterSpacing: 1, textTransform: 'uppercase',
            color: '#8891A5',
          }}>
            🕐 Event Correlation Timeline
          </div>
          <div style={{ fontSize: 9, color: '#4A5268', marginTop: 2 }}>
            {displayed.length} events · {formatRelative(minTs)} — Now
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          {Object.entries(TYPE_COLORS).map(([type, color]) => (
            <div key={type} style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 8, color: '#4A5268' }}>
              <div style={{ width: 6, height: 6, borderRadius: '50%', background: color }} />
              {type}
            </div>
          ))}
        </div>
      </div>

      {/* Visual time axis */}
      <div style={{ position: 'relative', height: 48, marginBottom: 12, borderRadius: 4, overflow: 'hidden' }}>
        {/* Background track */}
        <div style={{
          position: 'absolute', inset: 0,
          background: 'rgba(255,255,255,0.01)',
          border: '1px solid rgba(255,255,255,0.03)',
          borderRadius: 4,
        }} />

        {/* Event markers on the time axis */}
        {displayed.map(ev => {
          const pos = ((ev.ts - minTs) / range) * 100;
          const color = TYPE_COLORS[ev.type];
          const size = ev.severity === 'critical' ? 10 : ev.severity === 'high' ? 8 : 6;
          return (
            <div
              key={ev.id}
              title={`${formatTime(ev.ts)} — ${ev.title}`}
              style={{
                position: 'absolute',
                left: `${pos}%`,
                top: '50%',
                transform: 'translate(-50%, -50%)',
                width: size, height: size,
                borderRadius: '50%',
                background: color,
                border: `1px solid ${color}`,
                boxShadow: ev.severity === 'critical' ? `0 0 6px ${color}` : 'none',
                cursor: 'pointer',
                transition: 'transform 0.15s',
                zIndex: ev.severity === 'critical' ? 3 : 1,
              }}
            />
          );
        })}

        {/* Time labels */}
        <div style={{
          position: 'absolute', bottom: 2, left: 4,
          fontSize: 7, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace",
        }}>
          {formatTime(minTs)}
        </div>
        <div style={{
          position: 'absolute', bottom: 2, right: 4,
          fontSize: 7, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace",
        }}>
          {formatTime(maxTs)}
        </div>
      </div>

      {/* Event list */}
      <div style={{ maxHeight: 280, overflow: 'auto' }}>
        {displayed.map((ev, idx) => {
          const color = TYPE_COLORS[ev.type];
          const sevColor = SEVERITY_COLORS[ev.severity];
          const isLast = idx === displayed.length - 1;

          return (
            <div key={ev.id} style={{ display: 'flex', gap: 10, position: 'relative' }}>
              {/* Timeline connector */}
              <div style={{
                width: 24, flexShrink: 0,
                display: 'flex', flexDirection: 'column', alignItems: 'center',
              }}>
                <div style={{
                  width: 8, height: 8, borderRadius: '50%',
                  background: color, flexShrink: 0,
                  boxShadow: ev.severity === 'critical' ? `0 0 8px ${color}` : 'none',
                  border: `2px solid #0E1117`,
                }} />
                {!isLast && (
                  <div style={{
                    width: 1, flex: 1, minHeight: 20,
                    background: `linear-gradient(to bottom, ${color}40, rgba(255,255,255,0.03))`,
                  }} />
                )}
              </div>

              {/* Content */}
              <div style={{ flex: 1, paddingBottom: 12, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 2 }}>
                  <span style={{ fontSize: 12 }}>{ev.icon}</span>
                  <span style={{
                    fontSize: 7, fontWeight: 700, padding: '1px 5px',
                    borderRadius: 2, letterSpacing: 0.8,
                    background: `${color}15`, color, border: `1px solid ${color}30`,
                    fontFamily: "'JetBrains Mono', monospace", textTransform: 'uppercase',
                  }}>
                    {ev.type}
                  </span>
                  {ev.severity !== 'info' && (
                    <span style={{
                      fontSize: 7, fontWeight: 600, color: sevColor,
                      fontFamily: "'JetBrains Mono', monospace", textTransform: 'uppercase',
                    }}>
                      {ev.severity}
                    </span>
                  )}
                  <span style={{
                    marginLeft: 'auto', fontSize: 8, color: '#4A5268',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}>
                    {formatTime(ev.ts)}
                  </span>
                </div>
                <div style={{
                  fontSize: 10, color: '#E4E8F0', lineHeight: 1.4,
                  whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
                }}>
                  {ev.title}
                </div>
                <div style={{ fontSize: 8, color: '#4A5268', marginTop: 1 }}>
                  {ev.site}{ev.camera ? ` · ${ev.camera}` : ''}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
