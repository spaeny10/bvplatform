'use client';

import { useEffect, useState } from 'react';
import { getSLAReport, slaReportCsvUrl, downloadAuthenticated, type SLAReportRow } from '@/lib/ironsight-api';

// Operator response-time report card. Pulls from
// GET /api/reports/sla and renders a per-operator (or per-day)
// table with avg/p50/p95 ack times and within/over SLA counts.
//
// UL 827B reviewers ask for this exactly: "show me the 95th
// percentile ack time for the last 30 days, broken out by
// operator." With this card open and a screenshot or CSV export,
// the answer is one click away.

const PRESETS: Array<{ key: string; label: string; days: number }> = [
  { key: '7d',  label: 'Last 7 days',  days: 7 },
  { key: '30d', label: 'Last 30 days', days: 30 },
  { key: '90d', label: 'Last 90 days', days: 90 },
];

function isoDaysAgo(days: number): string {
  const d = new Date();
  d.setUTCDate(d.getUTCDate() - days);
  return d.toISOString();
}

function fmtSec(n: number): string {
  if (!isFinite(n) || n === 0) return '—';
  if (n < 60) return `${n.toFixed(1)}s`;
  return `${Math.floor(n / 60)}m ${Math.round(n % 60)}s`;
}

export default function SLAReportCard() {
  const [days, setDays] = useState(30);
  const [group, setGroup] = useState<'operator' | 'day'>('operator');
  const [rows, setRows] = useState<SLAReportRow[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string>('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setErr('');
    getSLAReport({ from: isoDaysAgo(days), group })
      .then((res) => { if (!cancelled) setRows(res.rows ?? []); })
      .catch((e) => { if (!cancelled) setErr(e instanceof Error ? e.message : String(e)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [days, group]);

  const downloadCSV = async () => {
    const url = slaReportCsvUrl({ from: isoDaysAgo(days), group });
    try {
      const stamp = new Date().toISOString().slice(0, 10);
      await downloadAuthenticated(url, `sla_report_${stamp}_${group}_${days}d.csv`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="report-card">
      <div className="report-card-header">
        <div>
          <div className="report-card-title">Operator Response-Time SLA</div>
          <div className="report-card-subtitle">
            Time from alarm creation to operator acknowledgement. UL 827B reviewers
            consult this for the audited 95th-percentile metric.
          </div>
        </div>
        <button className="report-csv-btn" onClick={downloadCSV} disabled={loading}>
          ⬇ CSV
        </button>
      </div>

      <div className="report-controls">
        <div className="report-segmented">
          {PRESETS.map((p) => (
            <button
              key={p.key}
              className={`report-segment ${days === p.days ? 'active' : ''}`}
              onClick={() => setDays(p.days)}
            >
              {p.label}
            </button>
          ))}
        </div>
        <div className="report-segmented">
          <button
            className={`report-segment ${group === 'operator' ? 'active' : ''}`}
            onClick={() => setGroup('operator')}
          >
            By operator
          </button>
          <button
            className={`report-segment ${group === 'day' ? 'active' : ''}`}
            onClick={() => setGroup('day')}
          >
            By day
          </button>
        </div>
      </div>

      {err && <div className="report-error">⚠ {err}</div>}

      {loading && !rows ? (
        <div className="report-empty">Loading…</div>
      ) : !rows || rows.length === 0 ? (
        <div className="report-empty">No alarms acknowledged in this window.</div>
      ) : (
        <table className="report-table">
          <thead>
            <tr>
              <th>{group === 'operator' ? 'Operator' : 'Date'}</th>
              <th>Total</th>
              <th>Acked</th>
              <th>Within SLA</th>
              <th>Over SLA</th>
              <th>Avg</th>
              <th>P50</th>
              <th>P95</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const overPct = r.acked_alarms > 0 ? (r.over_sla / r.acked_alarms) * 100 : 0;
              const overWarn = overPct >= 10;
              return (
                <tr key={r.bucket}>
                  <td className="report-bucket">{r.bucket}</td>
                  <td>{r.total_alarms}</td>
                  <td>{r.acked_alarms}</td>
                  <td className="report-good">{r.within_sla}</td>
                  <td className={overWarn ? 'report-bad' : ''}>{r.over_sla}</td>
                  <td>{fmtSec(r.avg_ack_sec)}</td>
                  <td>{fmtSec(r.p50_ack_sec)}</td>
                  <td>{fmtSec(r.p95_ack_sec)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}
