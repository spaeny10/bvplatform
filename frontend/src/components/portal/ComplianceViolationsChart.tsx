'use client';

import type { ComplianceTimeBucket } from '@/types/ironsight';

interface Props {
  buckets: ComplianceTimeBucket[];
  isLoading: boolean;
  truncUnit?: string; // 'hour' | 'day'
}

export default function ComplianceViolationsChart({ buckets, isLoading, truncUnit }: Props) {
  if (isLoading) {
    return <div className="compliance-skeleton-chart" />;
  }

  if (buckets.length === 0) {
    return <div className="compliance-empty">No violation data for this period.</div>;
  }

  const maxCount = Math.max(...buckets.map(b => b.count), 1);
  const svgW = 700;
  const svgH = 160;
  const padL = 32;
  const padB = 28;
  const padT = 8;
  const chartW = svgW - padL - 8;
  const chartH = svgH - padB - padT;

  const barW = chartW / buckets.length;
  const gap = barW * 0.18;

  // Format bucket label based on truncation unit.
  function label(iso: string): string {
    const d = new Date(iso);
    if (truncUnit === 'hour') {
      return d.getUTCHours().toString().padStart(2, '0') + ':00';
    }
    return `${(d.getUTCMonth() + 1).toString().padStart(2, '0')}/${d.getUTCDate().toString().padStart(2, '0')}`;
  }

  // Y-axis grid lines at 0, 50%, 100%.
  const gridLines = [0, 0.5, 1].map(pct => {
    const y = padT + chartH * (1 - pct);
    const val = Math.round(maxCount * pct);
    return { y, val };
  });

  return (
    <div className="compliance-chart-wrap">
      <svg
        viewBox={`0 0 ${svgW} ${svgH}`}
        width="100%"
        style={{ display: 'block', minWidth: 280, maxWidth: '100%' }}
        aria-label="Violations over time bar chart"
      >
        {/* Grid lines */}
        {gridLines.map((g, i) => (
          <g key={i}>
            <line
              x1={padL} y1={g.y}
              x2={svgW - 8} y2={g.y}
              stroke="#d8d0c4" strokeWidth={0.5}
            />
            <text
              x={padL - 4} y={g.y + 4}
              textAnchor="end"
              fontSize={9}
              fill="#a09080"
            >{g.val}</text>
          </g>
        ))}

        {/* Bars */}
        {buckets.map((b, i) => {
          const bh = Math.max(2, (b.count / maxCount) * chartH);
          const bx = padL + i * barW + gap / 2;
          const by = padT + chartH - bh;
          const bw = barW - gap;
          const lbl = label(b.bucket);
          return (
            <g key={i}>
              <rect
                x={bx} y={by}
                width={bw} height={bh}
                fill="var(--accent, #c84b2f)"
                rx={2}
              >
                <title>{lbl}: {b.count} violation{b.count !== 1 ? 's' : ''}</title>
              </rect>
              {/* Axis label — only render every Nth to avoid crowding */}
              {(buckets.length <= 14 || i % Math.ceil(buckets.length / 14) === 0) && (
                <text
                  x={bx + bw / 2} y={svgH - 4}
                  textAnchor="middle"
                  fontSize={8}
                  fill="#a09080"
                >{lbl}</text>
              )}
            </g>
          );
        })}
      </svg>
    </div>
  );
}
