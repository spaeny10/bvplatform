'use client';

import type { ComplianceSummary } from '@/types/ironsight';

interface Props {
  data: ComplianceSummary | null;
  isLoading: boolean;
}

export default function ComplianceSummaryCards({ data, isLoading }: Props) {
  if (isLoading) {
    return (
      <div className="compliance-cards">
        {[0, 1, 2].map(i => (
          <div key={i} className="compliance-skeleton-card" />
        ))}
      </div>
    );
  }

  const rateDisplay = data?.compliance_rate != null
    ? `${data.compliance_rate.toFixed(1)}%`
    : 'Insufficient data';

  const hoursDisplay = data?.person_hours_available
    ? (data.person_hours != null ? `${data.person_hours.toFixed(1)} h` : '—')
    : 'Unavailable';

  const hoursSub = data?.person_hours_available
    ? 'Occupancy tracking active'
    : 'Occupancy tracking not yet active';

  return (
    <div className="compliance-cards">
      <div className="compliance-card compliance-card--accent">
        <div className="compliance-card-label">Total Violations</div>
        <div className="compliance-card-value">{data?.total_violations ?? '—'}</div>
        <div className="compliance-card-sub">
          {data != null ? `${data.pending_count} pending review` : ''}
        </div>
      </div>
      <div className="compliance-card compliance-card--green">
        <div className="compliance-card-label">Compliance Rate</div>
        <div className="compliance-card-value">{rateDisplay}</div>
        <div className="compliance-card-sub">
          {data != null && data.total_reviewed > 0
            ? `${data.total_reviewed} reviewed`
            : 'No reviewed findings in period'}
        </div>
      </div>
      <div className="compliance-card compliance-card--blue">
        <div className="compliance-card-label">Person-Hours</div>
        <div className="compliance-card-value">{hoursDisplay}</div>
        <div className="compliance-card-sub">{hoursSub}</div>
      </div>
    </div>
  );
}
