'use client';

import Link from 'next/link';
import type { ComplianceFinding } from '@/types/ironsight';

interface Props {
  findings: ComplianceFinding[];
  isLoading: boolean;
}

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export default function ComplianceFindingsTable({ findings, isLoading }: Props) {
  if (isLoading) {
    return (
      <div>
        {[0, 1, 2, 4].map(i => <div key={i} className="compliance-skeleton-row" />)}
      </div>
    );
  }

  if (findings.length === 0) {
    return <div className="compliance-empty">No reviewed violations in this period.</div>;
  }

  return (
    <div className="compliance-table-wrap">
      <table className="compliance-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Camera</th>
            <th>Violation</th>
            <th>Confidence</th>
            <th>Review</th>
          </tr>
        </thead>
        <tbody>
          {findings.map(f => (
            <tr key={f.id}>
              <td data-label="Time">{relativeTime(f.created_at)}</td>
              <td data-label="Camera">{f.camera_name}</td>
              <td data-label="Violation">
                <span className="compliance-badge">{f.missing_label}</span>
              </td>
              <td data-label="Confidence">{(f.confidence * 100).toFixed(0)}%</td>
              <td data-label="Review">
                <Link href="/portal" style={{ color: 'var(--accent)', textDecoration: 'none', fontSize: '0.8rem' }}>
                  View queue →
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
