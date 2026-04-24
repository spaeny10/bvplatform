'use client';

import { useState, useEffect, useMemo } from 'react';
import { MOCK_ALERTS, MOCK_SITES, MOCK_INCIDENTS } from '@/lib/ironsight-mock';
import { BRAND } from '@/lib/branding';

interface Insight {
  id: string;
  severity: 'critical' | 'warning' | 'info' | 'positive';
  icon: string;
  title: string;
  body: string;
  metric?: string;
  timestamp: number;
  category: 'pattern' | 'anomaly' | 'recommendation' | 'summary';
}

function generateInsights(): Insight[] {
  const now = Date.now();
  const critAlerts = MOCK_ALERTS.filter(a => a.severity === 'critical');
  const highAlerts = MOCK_ALERTS.filter(a => a.severity === 'high');
  const unacked = MOCK_ALERTS.filter(a => !a.acknowledged);
  const worstSite = [...MOCK_SITES].sort((a, b) => a.compliance_score - b.compliance_score)[0];
  const bestSite = [...MOCK_SITES].sort((a, b) => b.compliance_score - a.compliance_score)[0];

  return [
    {
      id: 'i1', severity: 'critical', icon: '🚨',
      title: 'Repeat Violation Pattern Detected',
      body: `Scaffold Tower L4 at Southgate Power has had ${critAlerts.length} harness violations in the last 2 hours. This camera zone shows a 340% increase vs. the 7-day average. Consider dispatching a safety officer.`,
      metric: `${critAlerts.length} violations`,
      timestamp: now - 120000,
      category: 'pattern',
    },
    {
      id: 'i2', severity: 'warning', icon: '📉',
      title: 'SLA Compliance Dropping',
      body: `SLA compliance dropped from 96.1% to 88.4% since the last shift change at 14:00. ${unacked.length} alerts are currently unacknowledged. Response times have increased by 23s on average.`,
      metric: '−7.7% SLA',
      timestamp: now - 300000,
      category: 'anomaly',
    },
    {
      id: 'i3', severity: 'warning', icon: '👷',
      title: 'Abnormal Worker Count',
      body: `${worstSite.name} currently has ${worstSite.workers_on_site} workers on site, which is 40% above normal for this time of day (expected: ~30). High density correlates with increased PPE violation rates.`,
      metric: `${worstSite.workers_on_site} workers`,
      timestamp: now - 600000,
      category: 'anomaly',
    },
    {
      id: 'i4', severity: 'info', icon: '🎯',
      title: 'Recommended Focus Area',
      body: `Harness compliance across all sites is at 76% — the lowest PPE category. Top 3 contributing sites: Southgate Power (68%), Atlanta Interchange (72%), Bayshore Medical (74%). Consider targeted training for elevated work teams.`,
      metric: '76% harness rate',
      timestamp: now - 900000,
      category: 'recommendation',
    },
    {
      id: 'i5', severity: 'positive', icon: '✅',
      title: 'Improvement Trend: Midtown Transit',
      body: `${bestSite.name} has maintained ${bestSite.compliance_score}% compliance for 5 consecutive days with zero incidents. This site can serve as a best-practice model for other locations.`,
      metric: `${bestSite.compliance_score}% score`,
      timestamp: now - 1200000,
      category: 'summary',
    },
    {
      id: 'i6', severity: 'info', icon: '🔄',
      title: 'Shift Summary — Night Shift In Progress',
      body: `Current shift started at 18:00 with ${MOCK_SITES.reduce((s, site) => s + site.cameras_online, 0)} cameras online. ${MOCK_INCIDENTS.filter(i => i.status !== 'resolved').length} incidents carry over from day shift. ${highAlerts.length} high-priority alerts pending.`,
      metric: `${highAlerts.length} pending`,
      timestamp: now - 1800000,
      category: 'summary',
    },
  ];
}

const SEVERITY_STYLES: Record<string, { bg: string; border: string; accent: string; glow: string }> = {
  critical: { bg: 'rgba(255,51,85,0.04)', border: 'rgba(255,51,85,0.15)', accent: '#EF4444', glow: '0 0 12px rgba(255,51,85,0.1)' },
  warning: { bg: 'rgba(255,204,0,0.04)', border: 'rgba(255,204,0,0.12)', accent: '#E89B2A', glow: '0 0 12px rgba(255,204,0,0.08)' },
  info: { bg: 'rgba(0,212,255,0.03)', border: 'rgba(0,212,255,0.1)', accent: '#E8732A', glow: '0 0 12px rgba(0,212,255,0.06)' },
  positive: { bg: 'rgba(0,229,160,0.04)', border: 'rgba(0,229,160,0.12)', accent: '#22C55E', glow: '0 0 12px rgba(0,229,160,0.08)' },
};

const CATEGORY_LABELS: Record<string, string> = {
  pattern: 'PATTERN',
  anomaly: 'ANOMALY',
  recommendation: 'ACTION',
  summary: 'BRIEF',
};

function formatTimeAgo(ts: number): string {
  const diff = Date.now() - ts;
  if (diff < 60000) return 'Just now';
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
  return `${Math.floor(diff / 3600000)}h ago`;
}

interface Props {
  collapsed?: boolean;
  onToggle?: () => void;
}

export default function AIInsightsPanel({ collapsed = false, onToggle }: Props) {
  const insights = useMemo(generateInsights, []);
  const [filter, setFilter] = useState<'all' | 'critical' | 'warning' | 'info'>('all');
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [animatedCount, setAnimatedCount] = useState(0);

  // Staggered entrance animation
  useEffect(() => {
    if (collapsed) return;
    const timer = setInterval(() => {
      setAnimatedCount(c => {
        if (c >= insights.length) { clearInterval(timer); return c; }
        return c + 1;
      });
    }, 150);
    return () => clearInterval(timer);
  }, [collapsed, insights.length]);

  const filtered = filter === 'all' ? insights : insights.filter(i => i.severity === filter);

  if (collapsed) {
    return (
      <button
        onClick={onToggle}
        style={{
          position: 'fixed', right: 12, bottom: 12, zIndex: 5000,
          width: 48, height: 48, borderRadius: '50%',
          background: 'linear-gradient(135deg, #0E1117 0%, #141c24 100%)',
          border: '1px solid rgba(0,212,255,0.2)',
          color: '#E8732A', fontSize: 20, cursor: 'pointer',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          boxShadow: '0 4px 24px rgba(0,0,0,0.4), 0 0 16px rgba(0,212,255,0.1)',
          transition: 'all 0.2s',
        }}
        title="AI Insights"
      >
        🤖
      </button>
    );
  }

  return (
    <div style={{
      position: 'fixed', right: 12, bottom: 12, top: 52, width: 340, zIndex: 5000,
      background: 'linear-gradient(180deg, #0E1117 0%, #080c10 100%)',
      border: '1px solid rgba(255,255,255,0.06)',
      borderRadius: 8,
      display: 'flex', flexDirection: 'column',
      boxShadow: '0 8px 48px rgba(0,0,0,0.5), 0 0 24px rgba(0,212,255,0.05)',
      animation: 'slideout-enter 0.25s ease-out',
      overflow: 'hidden',
    }}>
      {/* Header */}
      <div style={{
        padding: '12px 14px', borderBottom: '1px solid rgba(255,255,255,0.06)',
        display: 'flex', alignItems: 'center', gap: 8,
        background: 'rgba(0,0,0,0.2)',
      }}>
        <span style={{ fontSize: 16 }}>🤖</span>
        <div style={{ flex: 1 }}>
          <div style={{
            fontSize: 11, fontWeight: 700, letterSpacing: 1.5,
            color: '#E8732A',
            fontFamily: "'JetBrains Mono', monospace",
          }}>
            AI INTELLIGENCE BRIEF
          </div>
          <div style={{ fontSize: 9, color: '#4A5268', marginTop: 1 }}>
            {insights.length} insights · Updated {formatTimeAgo(insights[0].timestamp)}
          </div>
        </div>
        <button
          onClick={onToggle}
          style={{
            width: 24, height: 24, borderRadius: 4,
            background: 'rgba(255,255,255,0.03)',
            border: '1px solid rgba(255,255,255,0.06)',
            color: '#4A5268', cursor: 'pointer', fontSize: 10,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}
        >✕</button>
      </div>

      {/* Filter tabs */}
      <div style={{
        padding: '6px 14px', display: 'flex', gap: 4,
        borderBottom: '1px solid rgba(255,255,255,0.04)',
      }}>
        {(['all', 'critical', 'warning', 'info'] as const).map(f => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            style={{
              padding: '3px 8px', borderRadius: 3, fontSize: 9,
              fontWeight: 600, letterSpacing: 0.5, cursor: 'pointer',
              background: filter === f ? 'rgba(0,212,255,0.08)' : 'transparent',
              border: `1px solid ${filter === f ? 'rgba(0,212,255,0.2)' : 'rgba(255,255,255,0.04)'}`,
              color: filter === f ? '#E8732A' : '#4A5268',
              textTransform: 'uppercase',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {f}
          </button>
        ))}
      </div>

      {/* Insights List */}
      <div style={{ flex: 1, overflow: 'auto', padding: '8px 10px' }}>
        {filtered.map((insight, idx) => {
          const style = SEVERITY_STYLES[insight.severity];
          const isExpanded = expandedId === insight.id;
          const isVisible = idx < animatedCount;

          return (
            <div
              key={insight.id}
              onClick={() => setExpandedId(isExpanded ? null : insight.id)}
              style={{
                marginBottom: 8,
                background: style.bg,
                border: `1px solid ${style.border}`,
                borderRadius: 6,
                padding: '10px 12px',
                cursor: 'pointer',
                transition: 'all 0.2s',
                opacity: isVisible ? 1 : 0,
                transform: isVisible ? 'translateX(0)' : 'translateX(20px)',
                boxShadow: isExpanded ? style.glow : 'none',
              }}
            >
              {/* Top row */}
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                <span style={{ fontSize: 16, flexShrink: 0 }}>{insight.icon}</span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 3 }}>
                    <span style={{
                      fontSize: 7, fontWeight: 700, padding: '1px 5px',
                      borderRadius: 2, letterSpacing: 1,
                      background: `${style.accent}15`,
                      color: style.accent,
                      border: `1px solid ${style.accent}30`,
                      fontFamily: "'JetBrains Mono', monospace",
                    }}>
                      {CATEGORY_LABELS[insight.category]}
                    </span>
                    <span style={{ fontSize: 8, color: '#4A5268' }}>
                      {formatTimeAgo(insight.timestamp)}
                    </span>
                  </div>
                  <div style={{
                    fontSize: 11, fontWeight: 600, color: '#E4E8F0',
                    lineHeight: 1.3,
                  }}>
                    {insight.title}
                  </div>
                </div>
                {insight.metric && (
                  <div style={{
                    fontSize: 10, fontWeight: 700, color: style.accent,
                    fontFamily: "'JetBrains Mono', monospace",
                    flexShrink: 0, textAlign: 'right',
                  }}>
                    {insight.metric}
                  </div>
                )}
              </div>

              {/* Expanded body */}
              {isExpanded && (
                <div style={{
                  marginTop: 8, paddingTop: 8,
                  borderTop: `1px solid ${style.border}`,
                  fontSize: 10, color: '#8891A5', lineHeight: 1.6,
                  animation: 'cam-fullscreen-enter 0.15s ease-out',
                }}>
                  {insight.body}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Footer */}
      <div style={{
        padding: '8px 14px',
        borderTop: '1px solid rgba(255,255,255,0.04)',
        display: 'flex', justifyContent: 'space-between',
        fontSize: 8, color: '#4A5268',
        fontFamily: "'JetBrains Mono', monospace",
      }}>
        <span>Powered by {BRAND.name} AI v2.1</span>
        <span>Refreshes every 60s</span>
      </div>
    </div>
  );
}
