'use client';

import './analytics.css';
import '../operator/operator.css';
import { useMemo } from 'react';
import { MOCK_SITES, MOCK_ALERTS, MOCK_INCIDENTS } from '@/lib/ironsight-mock';
import IncidentTimeline from '@/components/operator/IncidentTimeline';
import Logo from '@/components/shared/Logo';
import Link from 'next/link';
import { BRAND } from '@/lib/branding';

// ── Data Generation ──────────────────────────────────────────

function generateHeatmapData() {
  const days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
  return days.map(day => ({
    day,
    hours: Array.from({ length: 24 }, (_, h) => {
      // Simulate realistic patterns: more violations during work hours
      const isWorkHour = h >= 6 && h <= 18;
      const isPeak = h >= 9 && h <= 14;
      const isWeekend = day === 'Sat' || day === 'Sun';
      const base = isWeekend ? 1 : isPeak ? 8 : isWorkHour ? 5 : 1;
      return Math.floor(base + Math.random() * base);
    }),
  }));
}

function generateComplianceTrend(days: number) {
  const data: Array<{ date: string; hardHat: number; harness: number; hiVis: number; boots: number; overall: number }> = [];
  let hardHat = 85, harness = 72, hiVis = 90, boots = 94;
  for (let i = 0; i < days; i++) {
    hardHat = Math.max(60, Math.min(100, hardHat + (Math.random() - 0.45) * 3));
    harness = Math.max(60, Math.min(100, harness + (Math.random() - 0.4) * 4));
    hiVis = Math.max(60, Math.min(100, hiVis + (Math.random() - 0.45) * 2));
    boots = Math.max(60, Math.min(100, boots + (Math.random() - 0.48) * 1.5));
    const d = new Date(Date.now() - (days - 1 - i) * 86400000);
    data.push({
      date: d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
      hardHat, harness, hiVis, boots,
      overall: (hardHat + harness + hiVis + boots) / 4,
    });
  }
  return data;
}

function generateResponseTimes(count: number) {
  const buckets = [
    { label: '<15s', min: 0, max: 15 },
    { label: '15-30s', min: 15, max: 30 },
    { label: '30-60s', min: 30, max: 60 },
    { label: '1-2m', min: 60, max: 120 },
    { label: '2-5m', min: 120, max: 300 },
    { label: '5m+', min: 300, max: 600 },
  ];
  return buckets.map(b => ({
    ...b,
    count: b.min < 60 ? Math.floor(15 + Math.random() * 25) : Math.floor(3 + Math.random() * 12),
  }));
}

function generateSeverityDistribution() {
  return [
    { label: 'Critical', count: 12, color: '#EF4444' },
    { label: 'High', count: 34, color: '#EF4444' },
    { label: 'Medium', count: 67, color: '#E89B2A' },
    { label: 'Low', count: 89, color: '#E8732A' },
  ];
}

// ── SVG Chart Components ─────────────────────────────────────

function SVGSparkline({ data, color, width = 80, height = 24 }: { data: number[]; color: string; width?: number; height?: number }) {
  if (data.length < 2) return null;
  const min = Math.min(...data);
  const max = Math.max(...data);
  const range = max - min || 1;
  const points = data.map((v, i) =>
    `${(i / (data.length - 1)) * width},${height - ((v - min) / range) * height}`
  ).join(' ');

  return (
    <svg width={width} height={height} className="sparkline-container">
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle
        cx={(data.length - 1) / (data.length - 1) * width}
        cy={height - ((data[data.length - 1] - min) / range) * height}
        r="2"
        fill={color}
      />
    </svg>
  );
}

function SVGDonut({ segments, size = 140 }: { segments: Array<{ label: string; count: number; color: string }>; size?: number }) {
  const total = segments.reduce((s, seg) => s + seg.count, 0);
  const radius = size / 2 - 12;
  const cx = size / 2;
  const cy = size / 2;
  const strokeWidth = 18;
  let cumulativeAngle = -90;

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
      {segments.map((seg, i) => {
        const angle = (seg.count / total) * 360;
        const startAngle = (cumulativeAngle * Math.PI) / 180;
        const endAngle = ((cumulativeAngle + angle) * Math.PI) / 180;
        cumulativeAngle += angle;

        const largeArc = angle > 180 ? 1 : 0;
        const x1 = cx + radius * Math.cos(startAngle);
        const y1 = cy + radius * Math.sin(startAngle);
        const x2 = cx + radius * Math.cos(endAngle);
        const y2 = cy + radius * Math.sin(endAngle);

        return (
          <path
            key={i}
            d={`M ${x1} ${y1} A ${radius} ${radius} 0 ${largeArc} 1 ${x2} ${y2}`}
            fill="none"
            stroke={seg.color}
            strokeWidth={strokeWidth}
            strokeLinecap="round"
            opacity={0.85}
          />
        );
      })}
      <text x={cx} y={cy - 4} textAnchor="middle" fill="#E4E8F0" fontSize="22" fontWeight="700" fontFamily="'JetBrains Mono', monospace">
        {total}
      </text>
      <text x={cx} y={cy + 12} textAnchor="middle" fill="#4A5268" fontSize="9" fontWeight="600" letterSpacing="1">
        TOTAL
      </text>
    </svg>
  );
}

function SVGLineChart({ datasets, labels, height = 200, colors }: {
  datasets: number[][];
  labels: string[];
  height?: number;
  colors: string[];
}) {
  const allValues = datasets.flat();
  const min = Math.min(...allValues) - 2;
  const max = Math.max(...allValues) + 2;
  const range = max - min || 1;
  const width = 100; // percentage-based

  // Y-axis gridlines
  const gridLines = [min, min + range * 0.25, min + range * 0.5, min + range * 0.75, max];

  return (
    <div style={{ position: 'relative', height, paddingLeft: 32 }}>
      {/* Y-axis labels */}
      {gridLines.map((v, i) => (
        <div key={i} style={{
          position: 'absolute',
          left: 0,
          top: `${100 - ((v - min) / range) * 100}%`,
          transform: 'translateY(-50%)',
          fontSize: 8,
          color: '#4A5268',
          fontFamily: "'JetBrains Mono', monospace",
        }}>{Math.round(v)}%</div>
      ))}

      <svg width="100%" height="100%" viewBox={`0 0 ${labels.length - 1} ${range}`} preserveAspectRatio="none" style={{ overflow: 'visible' }}>
        {/* Grid lines */}
        {gridLines.map((v, i) => (
          <line key={i} x1="0" y1={max - v} x2={labels.length - 1} y2={max - v}
            stroke="rgba(255,255,255,0.03)" strokeWidth="0.1" />
        ))}

        {/* Data lines */}
        {datasets.map((data, di) => {
          const points = data.map((v, i) => `${i},${max - v}`).join(' ');
          const areaPoints = `0,${range} ${points} ${data.length - 1},${range}`;
          return (
            <g key={di}>
              <polygon points={areaPoints} fill={colors[di]} opacity="0.05" />
              <polyline
                points={points}
                fill="none"
                stroke={colors[di]}
                strokeWidth="0.15"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </g>
          );
        })}
      </svg>

      {/* X-axis labels */}
      <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 4, paddingLeft: 0 }}>
        {labels.filter((_, i) => i % Math.ceil(labels.length / 10) === 0).map((label, i) => (
          <span key={i} style={{ fontSize: 8, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
            {label}
          </span>
        ))}
      </div>
    </div>
  );
}

// ── Heatmap Colors ───────────────────────────────────────────

function getHeatColor(value: number, max: number): string {
  if (value === 0) return 'rgba(255,255,255,0.02)';
  const intensity = value / max;
  if (intensity > 0.7) return `rgba(255,51,85,${0.3 + intensity * 0.6})`;
  if (intensity > 0.4) return `rgba(255,107,53,${0.2 + intensity * 0.5})`;
  if (intensity > 0.2) return `rgba(255,204,0,${0.15 + intensity * 0.4})`;
  return `rgba(0,212,255,${0.05 + intensity * 0.3})`;
}

// ── Main Page Component ──────────────────────────────────────

export default function AnalyticsPage() {
  const heatmap = useMemo(generateHeatmapData, []);
  const complianceTrend = useMemo(() => generateComplianceTrend(30), []);
  const responseTimes = useMemo(() => generateResponseTimes(6), []);
  const severityDist = useMemo(generateSeverityDistribution, []);
  const maxHeatValue = useMemo(() => Math.max(...heatmap.flatMap(d => d.hours)), [heatmap]);

  // KPIs derived from mock data
  const totalAlerts = MOCK_ALERTS.length;
  const criticalAlerts = MOCK_ALERTS.filter(a => a.severity === 'critical').length;
  const avgCompliance = Math.round(MOCK_SITES.reduce((s, site) => s + site.compliance_score, 0) / MOCK_SITES.length);
  const openIncidents = MOCK_INCIDENTS.filter(i => i.status !== 'resolved').length;
  const avgResponseSec = 47; // mock
  const slaRate = 94.2; // mock

  // Top violation sites
  const topSites = [...MOCK_SITES]
    .sort((a, b) => a.compliance_score - b.compliance_score)
    .slice(0, 5);

  return (
    <div className="analytics-shell">
      {/* ── Header ── */}
      <div className="analytics-header">
        <div className="analytics-header-title">
          <Link href="/" style={{ textDecoration: 'none', display: 'flex', alignItems: 'center' }}>
            <Logo height={18} />
          </Link>
          <span style={{ color: '#4A5268', fontWeight: 400, fontSize: 12, letterSpacing: 0.5, marginLeft: 8 }}>/ Analytics</span>
        </div>

        <div className="analytics-header-nav">
          <Link href="/operator" style={{ textDecoration: 'none' }}>
            <button className="analytics-nav-btn">SOC Monitor</button>
          </Link>
          <Link href="/portal" style={{ textDecoration: 'none' }}>
            <button className="analytics-nav-btn">Portal</button>
          </Link>
          <button className="analytics-nav-btn active">Analytics</button>
        </div>
      </div>

      {/* ── KPI Strip ── */}
      <div className="analytics-kpi-strip">
        <div className="analytics-kpi" data-accent="red">
          <div className="analytics-kpi-label">Total Alerts (24h)</div>
          <div className="analytics-kpi-value" style={{ color: '#EF4444' }}>{totalAlerts * 14}</div>
          <div className="analytics-kpi-sub">
            <span className="analytics-kpi-trend up">↑ 12%</span> vs yesterday
          </div>
        </div>
        <div className="analytics-kpi" data-accent="amber">
          <div className="analytics-kpi-label">Critical Incidents</div>
          <div className="analytics-kpi-value" style={{ color: '#EF4444' }}>{criticalAlerts + 4}</div>
          <div className="analytics-kpi-sub">
            <span className="analytics-kpi-trend down">↓ 18%</span> vs yesterday
          </div>
        </div>
        <div className="analytics-kpi" data-accent="green">
          <div className="analytics-kpi-label">Avg Compliance</div>
          <div className="analytics-kpi-value" style={{ color: '#22C55E' }}>{avgCompliance}%</div>
          <div className="analytics-kpi-sub">
            <span className="analytics-kpi-trend up">↑ 2.4%</span> 7-day trend
          </div>
        </div>
        <div className="analytics-kpi" data-accent="cyan">
          <div className="analytics-kpi-label">Avg Response Time</div>
          <div className="analytics-kpi-value" style={{ color: '#E8732A' }}>{avgResponseSec}s</div>
          <div className="analytics-kpi-sub">
            <span className="analytics-kpi-trend up">↑ 8s</span> faster this week
          </div>
        </div>
        <div className="analytics-kpi" data-accent="purple">
          <div className="analytics-kpi-label">SLA Compliance</div>
          <div className="analytics-kpi-value" style={{ color: '#a855f7' }}>{slaRate}%</div>
          <div className="analytics-kpi-sub">
            <span className="analytics-kpi-trend down">↓ 1.2%</span> from target
          </div>
        </div>
        <div className="analytics-kpi" data-accent="blue">
          <div className="analytics-kpi-label">Open Incidents</div>
          <div className="analytics-kpi-value" style={{ color: '#6c8fff' }}>{openIncidents}</div>
          <div className="analytics-kpi-sub">
            {openIncidents} unresolved across {MOCK_SITES.length} sites
          </div>
        </div>
      </div>

      {/* ── Row 1: Violation Heatmap + Severity Distribution ── */}
      <div className="analytics-grid">
        {/* Heatmap */}
        <div className="analytics-panel">
          <div className="analytics-panel-header">
            <span className="analytics-panel-title">🔥 Violation Heatmap (Hour × Day)</span>
            <span className="analytics-panel-badge">Last 7 Days</span>
          </div>

          <div className="heatmap-grid">
            {/* Hour labels header */}
            <div />
            {Array.from({ length: 24 }, (_, h) => (
              <div key={h} className="heatmap-hour-label">
                {h % 3 === 0 ? `${h.toString().padStart(2, '0')}` : ''}
              </div>
            ))}

            {/* Rows */}
            {heatmap.map(row => (
              <>
                <div key={`label-${row.day}`} className="heatmap-label">{row.day}</div>
                {row.hours.map((val, h) => (
                  <div
                    key={`${row.day}-${h}`}
                    className="heatmap-cell"
                    style={{ background: getHeatColor(val, maxHeatValue) }}
                    title={`${row.day} ${h.toString().padStart(2, '0')}:00 — ${val} violations`}
                  />
                ))}
              </>
            ))}
          </div>

          <div style={{ display: 'flex', gap: 12, marginTop: 10, justifyContent: 'flex-end' }}>
            {[
              { label: 'Low', color: 'rgba(0,212,255,0.15)' },
              { label: 'Med', color: 'rgba(255,204,0,0.35)' },
              { label: 'High', color: 'rgba(255,107,53,0.5)' },
              { label: 'Critical', color: 'rgba(255,51,85,0.7)' },
            ].map(l => (
              <div key={l.label} style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 8, color: '#4A5268' }}>
                <div style={{ width: 10, height: 10, borderRadius: 2, background: l.color }} />
                {l.label}
              </div>
            ))}
          </div>
        </div>

        {/* Severity Distribution */}
        <div className="analytics-panel">
          <div className="analytics-panel-header">
            <span className="analytics-panel-title">📊 Alert Distribution</span>
          </div>

          <div className="donut-container">
            <SVGDonut segments={severityDist} />
            <div className="donut-legend">
              {severityDist.map(seg => (
                <div key={seg.label} className="donut-legend-item">
                  <div className="donut-legend-dot" style={{ background: seg.color }} />
                  <span>{seg.label}</span>
                  <span className="donut-legend-value">{seg.count}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* ── Row 2: Compliance Trend + Response Times ── */}
      <div className="analytics-grid">
        {/* Compliance Trend */}
        <div className="analytics-panel">
          <div className="analytics-panel-header">
            <span className="analytics-panel-title">📈 PPE Compliance Trend (30 Day)</span>
            <div style={{ display: 'flex', gap: 12 }}>
              {[
                { label: 'Hard Hat', color: '#E8732A' },
                { label: 'Harness', color: '#EF4444' },
                { label: 'Hi-Vis', color: '#22C55E' },
                { label: 'Boots', color: '#a855f7' },
              ].map(l => (
                <div key={l.label} style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 9, color: '#8891A5' }}>
                  <div style={{ width: 8, height: 2, background: l.color, borderRadius: 1 }} />
                  {l.label}
                </div>
              ))}
            </div>
          </div>

          <SVGLineChart
            datasets={[
              complianceTrend.map(d => d.hardHat),
              complianceTrend.map(d => d.harness),
              complianceTrend.map(d => d.hiVis),
              complianceTrend.map(d => d.boots),
            ]}
            labels={complianceTrend.map(d => d.date)}
            colors={['#E8732A', '#EF4444', '#22C55E', '#a855f7']}
            height={180}
          />
        </div>

        {/* Response Time Distribution */}
        <div className="analytics-panel">
          <div className="analytics-panel-header">
            <span className="analytics-panel-title">⏱ Response Time Distribution</span>
          </div>

          <div className="bar-chart-container">
            {responseTimes.map((bucket, i) => {
              const maxCount = Math.max(...responseTimes.map(b => b.count));
              const heightPct = (bucket.count / maxCount) * 100;
              const color = bucket.max <= 30 ? '#22C55E' : bucket.max <= 120 ? '#E89B2A' : '#EF4444';
              return (
                <div key={i} className="bar-col">
                  <div style={{ fontSize: 8, color: '#8891A5', fontFamily: "'JetBrains Mono', monospace" }}>
                    {bucket.count}
                  </div>
                  <div
                    className="bar-fill"
                    style={{ height: `${heightPct}%`, background: color }}
                    title={`${bucket.label}: ${bucket.count} alerts`}
                  />
                  <div className="bar-label">{bucket.label}</div>
                </div>
              );
            })}
          </div>

          <div style={{ marginTop: 12, padding: '8px', background: 'rgba(0,229,160,0.04)', borderRadius: 4, border: '1px solid rgba(0,229,160,0.1)' }}>
            <div style={{ fontSize: 10, color: '#22C55E', fontWeight: 600 }}>
              82% of alerts acknowledged within 60 seconds
            </div>
            <div style={{ fontSize: 9, color: '#4A5268', marginTop: 2 }}>
              Target SLA: 90% within 120 seconds
            </div>
          </div>
        </div>
      </div>

      {/* ── Row 3: Top Violation Sites + Daily Volume + SLA Trend ── */}
      <div className="analytics-grid-3">
        {/* Top Violation Sites */}
        <div className="analytics-panel">
          <div className="analytics-panel-header">
            <span className="analytics-panel-title">🏗 Sites by Risk</span>
          </div>
          <table className="analytics-table">
            <thead>
              <tr>
                <th>Site</th>
                <th>Score</th>
                <th>Trend</th>
                <th>Incidents</th>
              </tr>
            </thead>
            <tbody>
              {topSites.map(site => {
                const scoreColor = site.compliance_score >= 90 ? '#22C55E' : site.compliance_score >= 75 ? '#E89B2A' : '#EF4444';
                return (
                  <tr key={site.id}>
                    <td>
                      <div style={{ fontWeight: 600, color: '#E4E8F0' }}>{site.name}</div>
                      <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>{site.id}</div>
                    </td>
                    <td>
                      <span style={{ fontWeight: 700, color: scoreColor, fontFamily: "'JetBrains Mono', monospace" }}>
                        {site.compliance_score}%
                      </span>
                    </td>
                    <td>
                      <SVGSparkline
                        data={Array.from({ length: 7 }, () => site.compliance_score + (Math.random() - 0.5) * 10)}
                        color={site.trend === 'up' ? '#22C55E' : site.trend === 'down' ? '#EF4444' : '#E89B2A'}
                      />
                    </td>
                    <td style={{ fontFamily: "'JetBrains Mono', monospace" }}>{site.open_incidents}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {/* Daily Alert Volume */}
        <div className="analytics-panel">
          <div className="analytics-panel-header">
            <span className="analytics-panel-title">📅 Daily Alert Volume</span>
          </div>
          <div className="bar-chart-container" style={{ height: 150 }}>
            {Array.from({ length: 14 }, (_, i) => {
              const d = new Date(Date.now() - (13 - i) * 86400000);
              const count = 20 + Math.floor(Math.random() * 40);
              const maxCount = 60;
              return (
                <div key={i} className="bar-col">
                  <div style={{ fontSize: 7, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>{count}</div>
                  <div className="bar-fill" style={{
                    height: `${(count / maxCount) * 100}%`,
                    background: i === 13 ? '#E8732A' : 'rgba(0,212,255,0.3)',
                  }} />
                  <div className="bar-label" style={{ fontSize: 7 }}>
                    {d.toLocaleDateString('en-US', { day: 'numeric' })}
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* SLA Trend */}
        <div className="analytics-panel">
          <div className="analytics-panel-header">
            <span className="analytics-panel-title">🎯 SLA Compliance Trend</span>
          </div>
          <SVGLineChart
            datasets={[Array.from({ length: 14 }, (_, i) => 88 + Math.random() * 10)]}
            labels={Array.from({ length: 14 }, (_, i) => {
              const d = new Date(Date.now() - (13 - i) * 86400000);
              return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
            })}
            colors={['#a855f7']}
            height={150}
          />
          <div style={{ marginTop: 8, display: 'flex', justifyContent: 'space-between' }}>
            <span style={{ fontSize: 9, color: '#4A5268' }}>Target: 95%</span>
            <span style={{ fontSize: 9, color: '#a855f7', fontWeight: 600, fontFamily: "'JetBrains Mono', monospace" }}>
              Current: {slaRate}%
            </span>
          </div>
        </div>
      </div>

      {/* ── Row 4: Event Correlation Timeline ── */}
      <div className="analytics-grid-full">
        <IncidentTimeline maxEvents={20} />
      </div>

      {/* ── Footer ── */}
      <div style={{
        padding: '12px 24px', borderTop: '1px solid rgba(255,255,255,0.04)',
        display: 'flex', justifyContent: 'space-between', alignItems: 'center',
        fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace",
      }}>
        <span>{BRAND.name} Analytics · {MOCK_SITES.length} sites · {MOCK_SITES.reduce((s, site) => s + site.cameras_online, 0)} cameras</span>
        <span>Last updated: {new Date().toLocaleTimeString('en-US', { hour12: false })}</span>
      </div>
    </div>
  );
}
