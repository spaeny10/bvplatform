'use client';

import { useState, useEffect, useCallback } from 'react';
import Link from 'next/link';
import { ThemeProvider } from '@/hooks/useTheme';
import { useSites } from '@/hooks/useSites';
import { getComplianceSummary, downloadComplianceReport } from '@/lib/api';
import type { ComplianceSummary, CompliancePeriod } from '@/types/ironsight';
import ComplianceSummaryCards from '@/components/portal/ComplianceSummaryCards';
import ComplianceViolationsChart from '@/components/portal/ComplianceViolationsChart';
import ComplianceCamerasTable from '@/components/portal/ComplianceCamerasTable';
import ComplianceFindingsTable from '@/components/portal/ComplianceFindingsTable';
import './compliance.css';

const PERIODS: { value: CompliancePeriod; label: string }[] = [
  { value: 'today', label: 'Today' },
  { value: 'week', label: '7 days' },
  { value: 'month', label: '30 days' },
  { value: '90days', label: '90 days' },
];

function CompliancePage() {
  const { data: sites = [] } = useSites();
  const [period, setPeriod] = useState<CompliancePeriod>('week');
  const [siteID, setSiteID] = useState<string>('');
  const [data, setData] = useState<ComplianceSummary | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pdfLoading, setPdfLoading] = useState(false);

  const fetchSummary = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await getComplianceSummary({
        period,
        site_id: siteID || undefined,
      });
      setData(result);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load compliance data.');
      setData(null);
    } finally {
      setIsLoading(false);
    }
  }, [period, siteID]);

  useEffect(() => {
    fetchSummary();
  }, [fetchSummary]);

  async function handleDownload() {
    setPdfLoading(true);
    try {
      await downloadComplianceReport({
        period,
        site_id: siteID || undefined,
      });
    } catch (e: unknown) {
      alert(e instanceof Error ? e.message : 'PDF download failed.');
    } finally {
      setPdfLoading(false);
    }
  }

  const truncUnit = period === 'today' ? 'hour' : 'day';

  return (
    <div className="compliance-page">
      {/* Header */}
      <div className="compliance-header">
        <div className="compliance-title">
          <Link href="/portal" style={{ color: 'var(--text-dim)', textDecoration: 'none', fontSize: '0.85rem', marginRight: '0.5rem' }}>
            ← Portal
          </Link>
          PPE Compliance
        </div>

        {/* Period tabs (hidden on mobile, replaced by select) */}
        <div className="compliance-period-tabs">
          {PERIODS.map(p => (
            <button
              key={p.value}
              className={`compliance-period-tab${period === p.value ? ' active' : ''}`}
              onClick={() => setPeriod(p.value)}
            >
              {p.label}
            </button>
          ))}
        </div>

        {/* Mobile period select */}
        <select
          className="compliance-period-select"
          value={period}
          onChange={e => setPeriod(e.target.value as CompliancePeriod)}
        >
          {PERIODS.map(p => (
            <option key={p.value} value={p.value}>{p.label}</option>
          ))}
        </select>
      </div>

      {/* Controls row */}
      <div className="compliance-controls">
        <select
          className="compliance-site-select"
          value={siteID}
          onChange={e => setSiteID(e.target.value)}
        >
          <option value="">All sites</option>
          {sites.map(s => (
            <option key={s.id} value={s.id}>{s.name}</option>
          ))}
        </select>

        <button
          className="compliance-download-btn"
          onClick={handleDownload}
          disabled={pdfLoading || isLoading}
        >
          {pdfLoading ? 'Generating…' : '↓ Download PDF'}
        </button>
      </div>

      {/* Error */}
      {error && (
        <div className="compliance-empty" style={{ color: 'var(--accent)' }}>
          {error}
        </div>
      )}

      {/* Stat cards */}
      <ComplianceSummaryCards data={data} isLoading={isLoading} />

      {/* Violations over time */}
      <div className="compliance-section">
        <div className="compliance-section-title">Violations Over Time</div>
        <ComplianceViolationsChart
          buckets={data?.violations_over_time ?? []}
          isLoading={isLoading}
          truncUnit={truncUnit}
        />
      </div>

      {/* Top cameras */}
      <div className="compliance-section">
        <div className="compliance-section-title">Top Cameras by Violation Count</div>
        <ComplianceCamerasTable
          cameras={data?.top_cameras ?? []}
          isLoading={isLoading}
        />
      </div>

      {/* Recent findings */}
      <div className="compliance-section">
        <div className="compliance-section-title">Recent Reviewed Violations</div>
        <ComplianceFindingsTable
          findings={data?.recent_findings ?? []}
          isLoading={isLoading}
        />
      </div>
    </div>
  );
}

export default function CompliancePageWrapper() {
  return (
    <ThemeProvider>
      <CompliancePage />
    </ThemeProvider>
  );
}
