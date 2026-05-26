'use client';

import type { ComplianceCameraSummary } from '@/types/ironsight';

interface Props {
  cameras: ComplianceCameraSummary[];
  isLoading: boolean;
}

export default function ComplianceCamerasTable({ cameras, isLoading }: Props) {
  if (isLoading) {
    return (
      <div>
        {[0, 1, 2].map(i => <div key={i} className="compliance-skeleton-row" />)}
      </div>
    );
  }

  if (cameras.length === 0) {
    return <div className="compliance-empty">No camera data for this period.</div>;
  }

  const maxViolations = Math.max(...cameras.map(c => c.violation_count), 1);

  return (
    <div className="compliance-table-wrap">
      <table className="compliance-table">
        <thead>
          <tr>
            <th>Camera</th>
            <th>Violations</th>
            <th>% of Total</th>
            <th>Share</th>
          </tr>
        </thead>
        <tbody>
          {cameras.map(cam => (
            <tr key={cam.camera_id}>
              <td data-label="Camera">{cam.camera_name}</td>
              <td data-label="Violations">
                <span className="compliance-badge">{cam.violation_count}</span>
              </td>
              <td data-label="% of Total">{cam.pct_of_total.toFixed(1)}%</td>
              <td data-label="Share">
                <div style={{
                  width: '100%',
                  maxWidth: 80,
                  height: 6,
                  background: 'var(--surface-2, #f0ece6)',
                  borderRadius: 3,
                  overflow: 'hidden',
                }}>
                  <div style={{
                    width: `${(cam.violation_count / maxViolations) * 100}%`,
                    height: '100%',
                    background: 'var(--accent, #c84b2f)',
                    borderRadius: 3,
                  }} />
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
