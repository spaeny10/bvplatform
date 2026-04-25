'use client';

import { BRAND } from '@/lib/branding';
import { useMemo, useCallback, useState } from 'react';
import { getComplianceLevel, formatRelativeTime } from '@/lib/format';
import { useSites } from '@/hooks/useSites';
import { useIncidents } from '@/hooks/useIncidents';
import { usePortalStore } from '@/stores/portal-store';
import { ThemeProvider, ThemeToggle } from '@/hooks/useTheme';
import ErrorBoundary from '@/components/shared/ErrorBoundary';
import PendingReviewQueue from '@/components/portal/PendingReviewQueue';
import Logo from '@/components/shared/Logo';
import UserChip from '@/components/shared/UserChip';
// Side-effect import: registering the Skeleton component injects its
// shimmer keyframes into <head> so the inline placeholder cards below
// can use the `sg-skeleton-shimmer` animation without their own CSS.
import '@/components/shared/Skeleton';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import type { SiteFeatureMode } from '@/types/ironsight';

// ── Feature mode helpers ────────────────────────────────────────────
const MODE_BADGE: Record<SiteFeatureMode, { label: string; color: string; bg: string; border: string }> = {
  security_only:       { label: '🔒 Security',          color: '#38BDF8', bg: 'rgba(56,189,248,0.08)',  border: 'rgba(56,189,248,0.25)' },
  security_and_safety: { label: '🛡️ Security + Safety', color: '#a855f7', bg: 'rgba(168,85,247,0.08)', border: 'rgba(168,85,247,0.25)' },
};

type SiteView = 'cards' | 'list';

function PortalInner() {
  const { data: sites = [], isLoading: sitesLoading } = useSites();
  const { data: incidents = [] } = useIncidents({ limit: 10 });
  const { dateRange, setDateRange } = usePortalStore();
  const router = useRouter();
  const [siteView, setSiteView] = useState<SiteView>('cards');
  const [siteFilter, setSiteFilter] = useState<'all' | SiteFeatureMode>('all');

  // ── Portfolio composition ──────────────────────────────────────
  const safetySites       = useMemo(() => sites.filter(s => (s.feature_mode ?? 'security_and_safety') !== 'security_only'), [sites]);
  const securityOnlySites = useMemo(() => sites.filter(s => s.feature_mode === 'security_only'), [sites]);
  const hasSafety         = safetySites.length > 0;
  const hasSecurityOnly   = securityOnlySites.length > 0;

  const dashboardTitle = !hasSafety
    ? 'Security Dashboard'
    : !hasSecurityOnly
      ? 'Safety & Security Dashboard'
      : 'Security & Safety Dashboard';

  const filteredSites = useMemo(() => {
    if (siteFilter === 'all') return sites;
    return sites.filter(s => (s.feature_mode ?? 'security_and_safety') === siteFilter);
  }, [sites, siteFilter]);

  // ── Summary stats ──────────────────────────────────────────────
  const avgPPECompliance   = useMemo(() => {
    if (safetySites.length === 0) return 0;
    return Math.round(safetySites.reduce((s, site) => s + site.compliance_score, 0) / safetySites.length);
  }, [safetySites]);

  const totalCamerasOnline = useMemo(() => sites.reduce((s, site) => s + site.cameras_online, 0), [sites]);
  const totalCamerasTotal  = useMemo(() => sites.reduce((s, site) => s + site.cameras_total, 0), [sites]);
  const openIncidents      = useMemo(() => incidents.filter(i => i.status !== 'resolved').length, [incidents]);
  const totalWorkers       = useMemo(() => sites.reduce((s, site) => s + site.workers_on_site, 0), [sites]);

  const criticalUnread = useMemo(() =>
    incidents.filter(i => i.severity === 'critical' && i.status === 'open'),
    [incidents]
  );

  // Compliance chart data (mock 7-day)
  const chartDays = useMemo(() => {
    return Array.from({ length: 7 }, (_, i) => {
      const d = new Date(Date.now() - (6 - i) * 86400000);
      return {
        label: d.toLocaleDateString('en-US', { weekday: 'short' }),
        hardHat: 75 + Math.random() * 20,
        harness: 70 + Math.random() * 20,
        hiVis:   82 + Math.random() * 15,
        boots:   88 + Math.random() * 10,
      };
    });
  }, []);

  const sevColors: Record<string, string> = {
    critical: '#c0311a', high: '#a05800', medium: '#9a6f00', low: '#1a4f8a',
  };

  // ── CSV exports ────────────────────────────────────────────────
  const exportSitesCSV = useCallback(() => {
    const headers = ['Site', 'ID', 'Tier', 'Status', 'Cameras Online', 'Open Incidents', 'Workers', 'Last Activity'];
    const rows = sites.map(s => [
      s.name, s.id, s.feature_mode ?? 'security_and_safety', s.status,
      s.cameras_online, s.open_incidents, s.workers_on_site, s.last_activity,
    ]);
    const csv = [headers.join(','), ...rows.map(r => r.join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `${BRAND.shortName.toLowerCase().replace(/\s+/g, '-')}-sites-${dateRange.start}-to-${dateRange.end}.csv`; a.click();
    URL.revokeObjectURL(url);
  }, [sites, dateRange]);

  const exportIncidentsCSV = useCallback(() => {
    const headers = ['ID', 'Severity', 'Status', 'Title', 'Site', 'Type', 'Timestamp'];
    const rows = incidents.map(i => [
      i.id, i.severity, i.status, `"${i.title}"`, i.site_name, i.type, new Date(i.ts).toISOString(),
    ]);
    const csv = [headers.join(','), ...rows.map(r => r.join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a'); a.href = url;
    a.download = `${BRAND.shortName.toLowerCase().replace(/\s+/g, '-')}-incidents-${dateRange.start}-to-${dateRange.end}.csv`; a.click();
    URL.revokeObjectURL(url);
  }, [incidents, dateRange]);

  return (
    <div className="portal-shell">
      {/* ── SIDEBAR ── */}
      <div className="portal-sidebar" role="navigation" aria-label="Portal navigation">
        <div className="portal-sidebar-top">
          <div className="portal-brand">
            <Logo height={18} />
          </div>
          <div className="portal-org-selector">
            <div className="portal-org-avatar">TC</div>
            <div style={{ flex: 1 }}>
              <div className="portal-org-name">Turner Construction</div>
              <div className="portal-org-sub">Enterprise Plan</div>
            </div>
            <span style={{ fontSize: 10, color: 'var(--text-dim)' }}>▾</span>
          </div>
        </div>

        <div className="portal-nav">
          <div className="portal-nav-label">Overview</div>
          <button className="portal-nav-item active">
            <span className="portal-nav-icon">📊</span> {dashboardTitle}
          </button>
          <button className="portal-nav-item">
            <span className="portal-nav-icon">🏗️</span> Sites
            <span className="portal-nav-badge" style={{ background: 'var(--bg-warm)', color: 'var(--text-dim)', border: '1px solid var(--border)' }}>{sites.length}</span>
          </button>
          <button className="portal-nav-item">
            <span className="portal-nav-icon">⚠️</span> Incidents
            {openIncidents > 0 && <span className="portal-nav-badge">{openIncidents}</span>}
          </button>
          <Link href="/portal/history" style={{ textDecoration: 'none', color: 'inherit' }}>
            <button className="portal-nav-item">
              <span className="portal-nav-icon">🎞️</span> History
            </button>
          </Link>

          {hasSafety && (
            <>
              <div className="portal-nav-label">Safety Analytics</div>
              <button className="portal-nav-item">
                <span className="portal-nav-icon">🦺</span> PPE Compliance
              </button>
              <button className="portal-nav-item">
                <span className="portal-nav-icon">📈</span> Trends
              </button>
              <Link href="/search" style={{ textDecoration: 'none' }}>
                <button className="portal-nav-item">
                  <span className="portal-nav-icon">🔍</span> Search
                </button>
              </Link>
            </>
          )}

          <div className="portal-nav-label">Security</div>
          <button className="portal-nav-item">
            <span className="portal-nav-icon">📹</span> Camera Activity
          </button>
          <button className="portal-nav-item">
            <span className="portal-nav-icon">🛡️</span> SOC Events
          </button>

          <div className="portal-nav-label">Reports</div>
          <button className="portal-nav-item">
            <span className="portal-nav-icon">📄</span> Generate Report
          </button>
          <button className="portal-nav-item">
            <span className="portal-nav-icon">📥</span> Downloads
          </button>

          <div className="portal-nav-label">Navigation</div>
          <Link href="/operator" style={{ textDecoration: 'none' }}>
            <button className="portal-nav-item">
              <span className="portal-nav-icon">🖥️</span> SOC Monitor
            </button>
          </Link>
          <Link href="/" style={{ textDecoration: 'none' }}>
            <button className="portal-nav-item">
              <span className="portal-nav-icon">📹</span> NVR
            </button>
          </Link>
        </div>

        <div className="portal-sidebar-bottom">
          <UserChip />
        </div>
      </div>

      {/* ── MAIN ── */}
      <div className="portal-main" role="main">
        <div className="portal-header">
          <div>
            <div className="portal-header-title">{dashboardTitle}</div>
            <div style={{ fontSize: 12, color: 'var(--text-dim)', marginTop: 1 }}>
              {sites.length} active site{sites.length !== 1 ? 's' : ''} · Real-time monitoring
              {hasSecurityOnly && hasSafety && (
                <span style={{ marginLeft: 8, fontSize: 10, opacity: 0.7 }}>
                  · {safetySites.length} safety · {securityOnlySites.length} security only
                </span>
              )}
            </div>
          </div>
          <div className="portal-header-actions">
            <ThemeToggle />
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span style={{ fontSize: 12 }}>📅</span>
              <input type="date" value={dateRange.start} onChange={e => setDateRange({ ...dateRange, start: e.target.value })} aria-label="Start date" style={{ padding: '4px 8px', borderRadius: 4, fontSize: 11, background: 'var(--bg-warm)', border: '1px solid var(--border)', color: 'inherit', fontFamily: 'inherit' }} />
              <span style={{ fontSize: 11, color: 'var(--text-dim)' }}>–</span>
              <input type="date" value={dateRange.end} onChange={e => setDateRange({ ...dateRange, end: e.target.value })} aria-label="End date" style={{ padding: '4px 8px', borderRadius: 4, fontSize: 11, background: 'var(--bg-warm)', border: '1px solid var(--border)', color: 'inherit', fontFamily: 'inherit' }} />
            </div>
            <button className="portal-btn portal-btn-ghost" onClick={exportSitesCSV}>↗ Export CSV</button>
            <button className="portal-btn portal-btn-primary">📄 Generate Report</button>
          </div>
        </div>

        <div className="portal-content">

          {/* ── MORNING BRIEFING BANNER ── */}
          {criticalUnread.length > 0 && (
            <div className="portal-card" style={{ borderLeft: '4px solid var(--accent, #c84b2f)', animation: 'portal-fadeUp 0.3s ease both' }}>
              <div style={{ padding: '14px 18px', display: 'flex', alignItems: 'center', gap: 12 }}>
                <span style={{ fontSize: 24 }}>🚨</span>
                <div style={{ flex: 1 }}>
                  <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--accent, #c84b2f)' }}>
                    {criticalUnread.length} Critical Overnight Event{criticalUnread.length > 1 ? 's' : ''}
                  </div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary, #6b6560)', marginTop: 2, lineHeight: 1.5 }}>
                    {criticalUnread.map(i => i.title).join(' · ')}
                  </div>
                </div>
                <Link href={`/portal/incidents/${criticalUnread[0]?.id}`} style={{ textDecoration: 'none' }}>
                  <button className="portal-btn portal-btn-primary" style={{ fontSize: 11 }}>Review Now →</button>
                </Link>
              </div>
            </div>
          )}

          {/* ── PENDING SAFETY REVIEW ── */}
          {hasSafety && (
            <ErrorBoundary>
              <PendingReviewQueue />
            </ErrorBoundary>
          )}

          {/* ── KPI SUMMARY CARDS ── */}
          <div className="portal-summary-row">
            <div className="portal-summary-card blue">
              <div className="portal-card-label">📹 Cameras Online</div>
              <div className="portal-card-value" style={{ color: totalCamerasOnline === totalCamerasTotal ? 'var(--green)' : 'var(--yellow)' }}>
                {totalCamerasOnline}<span style={{ fontSize: 16, opacity: 0.5 }}>/{totalCamerasTotal}</span>
              </div>
              <div className="portal-card-delta" style={{ color: 'var(--text-dim)' }}>Across {sites.length} sites</div>
            </div>

            <div className="portal-summary-card red">
              <div className="portal-card-label">⚠️ Open Incidents</div>
              <div className="portal-card-value" style={{ color: openIncidents > 0 ? 'var(--accent)' : 'var(--green)' }}>
                {openIncidents}
              </div>
              <div className="portal-card-delta down">↓ 1 from yesterday</div>
            </div>

            {hasSafety ? (
              <div className="portal-summary-card green">
                <div className="portal-card-label">🦺 PPE Compliance</div>
                <div className="portal-card-value" style={{ color: avgPPECompliance >= 90 ? 'var(--green)' : avgPPECompliance >= 75 ? 'var(--yellow)' : 'var(--accent)' }}>
                  {avgPPECompliance}%
                </div>
                <div className="portal-card-delta up" style={{ color: 'var(--text-dim)', fontSize: 10 }}>
                  {safetySites.length} safety-enabled site{safetySites.length !== 1 ? 's' : ''}
                </div>
              </div>
            ) : (
              <div className="portal-summary-card green">
                <div className="portal-card-label">🔒 Sites Secured</div>
                <div className="portal-card-value" style={{ color: 'var(--green)' }}>
                  {sites.filter(s => s.status === 'active').length}
                </div>
                <div className="portal-card-delta" style={{ color: 'var(--text-dim)' }}>of {sites.length} monitored</div>
              </div>
            )}

            <div className="portal-summary-card yellow">
              <div className="portal-card-label">👷 Workers On Site</div>
              <div className="portal-card-value" style={{ color: 'var(--yellow)' }}>{totalWorkers}</div>
              <div className="portal-card-delta" style={{ color: 'var(--text-dim)' }}>Across {sites.length} sites</div>
            </div>
          </div>

          {/* ── SITES SECTION ── */}
          <div className="portal-card" style={{ animation: 'portal-fadeUp 0.3s 0.1s ease both', overflow: 'visible' }}>
            {/* Section header */}
            <div className="portal-card-header" style={{ paddingBottom: 12 }}>
              <div>
                <div className="portal-card-title">Your Sites</div>
                <div style={{ fontSize: 11, color: 'var(--text-dim)', marginTop: 1 }}>
                  Click any site to view cameras, events, and reports
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                {/* Tier filter */}
                {hasSecurityOnly && hasSafety && (
                  <div style={{ display: 'flex', gap: 4 }}>
                    {([
                      { value: 'all',                label: 'All' },
                      { value: 'security_only',      label: '🔒 Security' },
                      { value: 'security_and_safety', label: '🛡️ Safety' },
                    ] as const).map(opt => (
                      <button
                        key={opt.value}
                        onClick={() => setSiteFilter(opt.value)}
                        style={{
                          padding: '4px 10px', borderRadius: 4, fontSize: 11, cursor: 'pointer',
                          border: `1px solid ${siteFilter === opt.value ? 'rgba(168,85,247,0.5)' : 'var(--border)'}`,
                          background: siteFilter === opt.value ? 'rgba(168,85,247,0.1)' : 'transparent',
                          color: siteFilter === opt.value ? '#a855f7' : 'var(--text-secondary)',
                          fontFamily: 'inherit',
                        }}
                      >
                        {opt.label}
                      </button>
                    ))}
                  </div>
                )}
                {/* View toggle */}
                <div style={{ display: 'flex', border: '1px solid var(--border)', borderRadius: 4, overflow: 'hidden' }}>
                  {(['cards', 'list'] as const).map(v => (
                    <button
                      key={v}
                      onClick={() => setSiteView(v)}
                      title={v === 'cards' ? 'Card view' : 'List view'}
                      style={{
                        padding: '5px 10px', fontSize: 13, cursor: 'pointer', border: 'none',
                        background: siteView === v ? 'var(--hover-tint)' : 'transparent',
                        color: siteView === v ? 'var(--text-primary)' : 'var(--text-dim)',
                        fontFamily: 'inherit',
                      }}
                    >
                      {v === 'cards' ? '⊞' : '☰'}
                    </button>
                  ))}
                </div>
                <button className="portal-card-action" onClick={exportSitesCSV}>Export CSV →</button>
              </div>
            </div>

            {/* ── CARD GRID ── */}
            {siteView === 'cards' && (
              <div style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
                gap: 12,
                padding: '4px 16px 16px',
              }}>
                {/* Loading skeleton: render placeholder cards while
                    useSites is still fetching, so the grid shows
                    "data is on its way" instead of a blank gap. Six
                    skeleton cards is enough to fill a typical viewport
                    on the first paint. */}
                {sitesLoading && filteredSites.length === 0 && Array.from({ length: 6 }).map((_, i) => (
                  <div
                    key={`sk-${i}`}
                    style={{
                      height: 140,
                      borderRadius: 8,
                      background:
                        'linear-gradient(90deg, var(--sg-surface-1, rgba(255,255,255,0.04)) 0%, var(--sg-surface-2, rgba(255,255,255,0.08)) 50%, var(--sg-surface-1, rgba(255,255,255,0.04)) 100%)',
                      backgroundSize: '200% 100%',
                      animation: 'sg-skeleton-shimmer 1.4s ease-in-out infinite',
                      border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
                    }}
                    aria-hidden="true"
                  />
                ))}
                {filteredSites.map(site => {
                  const mode = (site.feature_mode ?? 'security_and_safety') as SiteFeatureMode;
                  const badge = MODE_BADGE[mode];
                  const isSecOnly = mode === 'security_only';
                  const snoozed = site.snooze?.active && new Date(site.snooze.expires_at) > new Date();
                  const camPct = site.cameras_total > 0 ? (site.cameras_online / site.cameras_total) * 100 : 100;
                  const camColor = camPct === 100 ? 'var(--green)' : camPct >= 80 ? 'var(--yellow)' : 'var(--accent)';
                  const compLevel = getComplianceLevel(site.compliance_score);
                  const compColors = { green: 'var(--green)', amber: 'var(--yellow)', red: 'var(--accent)' };

                  return (
                    <div
                      key={site.id}
                      onClick={() => router.push(`/portal/sites/${site.id}`)}
                      style={{
                        background: 'var(--bg-warm)',
                        border: '1px solid var(--border)',
                        borderRadius: 8,
                        padding: '14px 16px',
                        cursor: 'pointer',
                        transition: 'border-color 0.15s, background 0.15s',
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 10,
                      }}
                      onMouseEnter={e => {
                        (e.currentTarget as HTMLDivElement).style.borderColor = badge.border;
                        (e.currentTarget as HTMLDivElement).style.background = isSecOnly
                          ? 'rgba(56,189,248,0.04)'
                          : 'rgba(168,85,247,0.04)';
                      }}
                      onMouseLeave={e => {
                        (e.currentTarget as HTMLDivElement).style.borderColor = 'var(--border)';
                        (e.currentTarget as HTMLDivElement).style.background = 'var(--bg-warm)';
                      }}
                    >
                      {/* Card top */}
                      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 8 }}>
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            {site.name}
                          </div>
                          <div style={{ fontSize: 10, color: 'var(--text-dim)', marginTop: 2, fontFamily: "'JetBrains Mono', monospace" }}>
                            {site.id}
                          </div>
                        </div>
                        <div style={{ display: 'flex', gap: 4, flexShrink: 0 }}>
                          {snoozed && (
                            <span style={{
                              fontSize: 9, padding: '3px 7px', borderRadius: 3, fontWeight: 600, whiteSpace: 'nowrap',
                              color: '#E89B2A', background: 'rgba(232,155,42,0.08)', border: '1px solid rgba(232,155,42,0.25)',
                            }}>
                              ⏸ Snoozed
                            </span>
                          )}
                          <span style={{
                            fontSize: 9, padding: '3px 7px', borderRadius: 3, fontWeight: 600,
                            color: badge.color, background: badge.bg, border: `1px solid ${badge.border}`,
                            whiteSpace: 'nowrap',
                          }}>
                            {badge.label}
                          </span>
                        </div>
                      </div>

                      {/* Camera bar */}
                      <div>
                        <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4, fontSize: 10 }}>
                          <span style={{ color: 'var(--text-dim)' }}>📹 Cameras</span>
                          <span style={{ color: camColor, fontWeight: 600, fontFamily: "'JetBrains Mono', monospace" }}>
                            {site.cameras_online}/{site.cameras_total}
                          </span>
                        </div>
                        <div style={{ height: 4, background: 'var(--bar-track)', borderRadius: 2, overflow: 'hidden' }}>
                          <div style={{ height: '100%', width: `${camPct}%`, background: camColor, borderRadius: 2, transition: 'width 0.4s' }} />
                        </div>
                      </div>

                      {/* Tier-specific metric */}
                      {!isSecOnly ? (
                        <div>
                          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4, fontSize: 10 }}>
                            <span style={{ color: 'var(--text-dim)' }}>🦺 PPE Compliance</span>
                            <span style={{ color: compColors[compLevel], fontWeight: 600, fontFamily: "'JetBrains Mono', monospace" }}>
                              {Math.round(site.compliance_score)}%
                            </span>
                          </div>
                          <div style={{ height: 4, background: 'var(--bar-track)', borderRadius: 2, overflow: 'hidden' }}>
                            <div style={{ height: '100%', width: `${site.compliance_score}%`, background: compColors[compLevel], borderRadius: 2, transition: 'width 0.4s' }} />
                          </div>
                        </div>
                      ) : (
                        <div style={{ fontSize: 10, color: 'var(--text-dim)', display: 'flex', alignItems: 'center', gap: 5 }}>
                          <span style={{ width: 6, height: 6, borderRadius: '50%', background: 'var(--green)', display: 'inline-block', flexShrink: 0 }} />
                          Security monitoring active
                        </div>
                      )}

                      {/* Stats row */}
                      <div style={{ display: 'flex', gap: 12, paddingTop: 4, borderTop: '1px solid var(--border)' }}>
                        <div style={{ flex: 1, textAlign: 'center' }}>
                          <div style={{ fontSize: 15, fontWeight: 700, color: site.open_incidents > 0 ? 'var(--accent)' : 'var(--green)' }}>
                            {site.open_incidents}
                          </div>
                          <div style={{ fontSize: 9, color: 'var(--text-dim)', marginTop: 1 }}>Incidents</div>
                        </div>
                        <div style={{ flex: 1, textAlign: 'center', borderLeft: '1px solid var(--border)' }}>
                          <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--yellow)' }}>
                            {site.workers_on_site}
                          </div>
                          <div style={{ fontSize: 9, color: 'var(--text-dim)', marginTop: 1 }}>Workers</div>
                        </div>
                        <div style={{ flex: 2, textAlign: 'right', borderLeft: '1px solid var(--border)', paddingLeft: 10 }}>
                          <div style={{ fontSize: 10, color: 'var(--text-dim)', lineHeight: 1.4 }}>
                            Last activity<br />
                            <span style={{ color: 'var(--text-secondary)' }}>{formatRelativeTime(new Date(site.last_activity).getTime())}</span>
                          </div>
                        </div>
                      </div>

                      {/* CTA row */}
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                        <span
                          onClick={e => { e.stopPropagation(); router.push(`/?site_id=${site.id}`); }}
                          style={{
                            fontSize: 10, fontWeight: 600, color: 'var(--text-dim)',
                            cursor: 'pointer', padding: '2px 0',
                          }}
                        >
                          📹 Open NVR
                        </span>
                        <span style={{
                          fontSize: 11, fontWeight: 600,
                          color: badge.color, opacity: 0.8,
                        }}>
                          View Site →
                        </span>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}

            {/* ── LIST VIEW ── */}
            {siteView === 'list' && (
              <table className="portal-table">
                <thead>
                  <tr>
                    <th>Site</th>
                    <th>Tier</th>
                    <th>Cameras</th>
                    {hasSafety && <th>Compliance</th>}
                    <th>Incidents</th>
                    <th>Workers</th>
                    <th>Last Activity</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredSites.map(site => {
                    const mode = (site.feature_mode ?? 'security_and_safety') as SiteFeatureMode;
                    const badge = MODE_BADGE[mode];
                    const level = getComplianceLevel(site.compliance_score);
                    const listSnoozed = site.snooze?.active && new Date(site.snooze.expires_at) > new Date();
                    return (
                      <tr key={site.id} onClick={() => router.push(`/portal/sites/${site.id}`)} style={{ cursor: 'pointer' }}>
                        <td className="portal-td-site">
                          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                            <span style={{ fontWeight: 500 }}>{site.name}</span>
                            {listSnoozed && (
                              <span style={{ fontSize: 8, padding: '1px 5px', borderRadius: 3, fontWeight: 700, color: '#E89B2A', background: 'rgba(232,155,42,0.08)', border: '1px solid rgba(232,155,42,0.2)' }}>⏸</span>
                            )}
                          </div>
                          <div style={{ fontSize: 10, color: 'var(--text-dim)', marginTop: 1 }}>{site.id}</div>
                        </td>
                        <td>
                          <span style={{
                            display: 'inline-block', fontSize: 9, padding: '2px 7px', borderRadius: 3,
                            fontWeight: 600, letterSpacing: 0.3, whiteSpace: 'nowrap',
                            color: badge.color, background: badge.bg, border: `1px solid ${badge.border}`,
                          }}>
                            {badge.label}
                          </span>
                        </td>
                        <td style={{ fontVariantNumeric: 'tabular-nums' }}>
                          <span style={{ color: site.cameras_online === site.cameras_total ? 'var(--green)' : 'var(--yellow)' }}>
                            {site.cameras_online}
                          </span>
                          <span style={{ color: 'var(--text-dim)' }}>/{site.cameras_total}</span>
                        </td>
                        {hasSafety && (
                          <td>
                            {mode === 'security_only' ? (
                              <span style={{ fontSize: 10, color: 'var(--text-dim)' }}>—</span>
                            ) : (
                              <span className={`portal-score-badge ${level}`}>{Math.round(site.compliance_score)}%</span>
                            )}
                          </td>
                        )}
                        <td>
                          {site.open_incidents > 0
                            ? <span style={{ color: 'var(--accent)', fontWeight: 600 }}>{site.open_incidents}</span>
                            : <span style={{ color: 'var(--green)' }}>0</span>}
                        </td>
                        <td>{site.workers_on_site}</td>
                        <td style={{ fontSize: 11, color: 'var(--text-dim)' }}>
                          {formatRelativeTime(new Date(site.last_activity).getTime())}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>

          {/* ── ANALYTICS ROW ── */}
          <div className="portal-two-col">
            {hasSafety ? (
              <ErrorBoundary>
                <div className="portal-card">
                  <div className="portal-card-header">
                    <div>
                      <div className="portal-card-title">PPE Compliance Trend</div>
                      <div style={{ fontSize: 11, color: 'var(--text-dim)', marginTop: 1 }}>
                        7-day breakdown · {safetySites.length} safety site{safetySites.length !== 1 ? 's' : ''}
                      </div>
                    </div>
                    <button className="portal-card-action">View Details →</button>
                  </div>
                  <div className="portal-chart-area">
                    <svg width="100%" height="200" viewBox="0 0 700 200" style={{ overflow: 'visible' }} role="img" aria-label="PPE compliance bar chart">
                      {[0, 25, 50, 75, 100].map(v => (
                        <g key={v}>
                          <line x1="40" y1={180 - v * 1.6} x2="690" y2={180 - v * 1.6} stroke="var(--border)" strokeWidth="0.5" opacity="0.3" />
                          <text x="36" y={184 - v * 1.6} textAnchor="end" fontSize="9" fill="var(--text-dim)" fontFamily="'JetBrains Mono', monospace">{v}%</text>
                        </g>
                      ))}
                      {chartDays.map((day, idx) => {
                        const x = 60 + idx * 90;
                        const w = 16;
                        const cats = [
                          { val: day.hardHat, color: '#1a7a4a', label: 'Hard Hat' },
                          { val: day.harness, color: '#2d7dd2', label: 'Harness' },
                          { val: day.hiVis,   color: '#d4a000', label: 'Hi-Vis' },
                          { val: day.boots,   color: '#c84b2f', label: 'Boots' },
                        ];
                        return (
                          <g key={idx}>
                            {cats.map((cat, ci) => {
                              const h = cat.val * 1.6;
                              const bx = x + ci * (w + 2);
                              return (
                                <rect key={ci} x={bx} y={180 - h} width={w} height={h} fill={cat.color} rx="2" opacity="0.85" style={{ transition: 'all 0.3s ease' }}>
                                  <title>{cat.label}: {Math.round(cat.val)}% — {day.label}</title>
                                </rect>
                              );
                            })}
                            <text x={x + 36} y="196" textAnchor="middle" fontSize="10" fill="var(--text-secondary)" fontFamily="'Inter', sans-serif">{day.label}</text>
                          </g>
                        );
                      })}
                    </svg>
                    <div className="portal-chart-legend">
                      <div className="portal-legend-item"><div className="portal-legend-dot" style={{ background: '#1a7a4a' }} /> Hard Hat</div>
                      <div className="portal-legend-item"><div className="portal-legend-dot" style={{ background: '#2d7dd2' }} /> Harness</div>
                      <div className="portal-legend-item"><div className="portal-legend-dot" style={{ background: '#d4a000' }} /> Hi-Vis</div>
                      <div className="portal-legend-item"><div className="portal-legend-dot" style={{ background: '#c84b2f' }} /> Boots</div>
                    </div>
                  </div>
                </div>
              </ErrorBoundary>
            ) : (
              <div className="portal-card">
                <div className="portal-card-header">
                  <div>
                    <div className="portal-card-title">Security Activity</div>
                    <div style={{ fontSize: 11, color: 'var(--text-dim)', marginTop: 1 }}>Recent SOC events across all sites</div>
                  </div>
                  <button className="portal-card-action" onClick={exportIncidentsCSV}>Export CSV →</button>
                </div>
                <div style={{ padding: '0 4px' }}>
                  {incidents.length === 0 && (
                    <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-dim)', fontSize: 12 }}>No recent security events</div>
                  )}
                  {incidents.slice(0, 6).map(inc => (
                    <Link key={inc.id} href={`/portal/incidents/${inc.id}`} className="portal-incident-item">
                      <div className="portal-incident-severity" style={{ background: sevColors[inc.severity] || '#a09990' }} />
                      <div className="portal-incident-body">
                        <div className="portal-incident-title">{inc.title}</div>
                        <div className="portal-incident-meta">
                          <span>📍 {inc.site_name}</span>
                          <span>🕐 {formatRelativeTime(inc.ts)}</span>
                        </div>
                      </div>
                      <span className={`portal-incident-status ${inc.status}`}>
                        {inc.status === 'in_review' ? 'In Review' : inc.status.charAt(0).toUpperCase() + inc.status.slice(1)}
                      </span>
                    </Link>
                  ))}
                </div>
              </div>
            )}

            {/* Recent Incidents — always shown */}
            <div className="portal-card">
              <div className="portal-card-header">
                <div className="portal-card-title">Recent Incidents</div>
                <button className="portal-card-action" onClick={exportIncidentsCSV}>Export CSV →</button>
              </div>
              <div>
                {incidents.slice(0, 5).map(inc => (
                  <Link key={inc.id} href={`/portal/incidents/${inc.id}`} className="portal-incident-item">
                    <div className="portal-incident-severity" style={{ background: sevColors[inc.severity] || '#a09990' }} />
                    <div className="portal-incident-body">
                      <div className="portal-incident-title">{inc.title}</div>
                      <div className="portal-incident-meta">
                        <span>📍 {inc.site_name}</span>
                        <span>🕐 {formatRelativeTime(inc.ts)}</span>
                      </div>
                    </div>
                    <span className={`portal-incident-status ${inc.status}`}>
                      {inc.status === 'in_review' ? 'In Review' : inc.status.charAt(0).toUpperCase() + inc.status.slice(1)}
                    </span>
                  </Link>
                ))}
              </div>
            </div>
          </div>

          {/* ── QUICK REPORTS ── */}
          <div className="portal-card" style={{ animation: 'portal-fadeUp 0.4s 0.4s ease both' }}>
            <div className="portal-card-header">
              <div className="portal-card-title">Quick Reports</div>
            </div>
            <div className="portal-report-row">
              <button className="portal-report-btn">🛡️ Security Summary PDF</button>
              <button className="portal-report-btn">⚠️ Incidents Report PDF</button>
              {hasSafety && <button className="portal-report-btn">🦺 PPE Compliance PDF</button>}
              {hasSafety && <button className="portal-report-btn">📊 Safety Analytics XLSX</button>}
              {!hasSafety && <button className="portal-report-btn">📹 Camera Activity PDF</button>}
              {!hasSafety && <button className="portal-report-btn">📊 SOC Performance XLSX</button>}
            </div>
          </div>

        </div>
      </div>
    </div>
  );
}

export default function PortalPage() {
  return (
    <ThemeProvider>
      <PortalInner />
    </ThemeProvider>
  );
}
