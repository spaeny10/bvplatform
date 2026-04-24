'use client';

import type { PPEBreakdown } from '@/types/ironsight';
import { getComplianceLevel } from '@/lib/format';

interface PPECompliancePanelProps {
  breakdown: PPEBreakdown;
}

const PPE_ITEMS = [
  { key: 'hard_hat', label: 'Hard Hat', icon: '⛑' },
  { key: 'harness', label: 'Harness', icon: '🔗' },
  { key: 'hi_vis', label: 'Hi-Vis Vest', icon: '🦺' },
  { key: 'boots', label: 'Safety Boots', icon: '👢' },
  { key: 'gloves', label: 'Gloves', icon: '🧤' },
] as const;

const LEVEL_COLORS = {
  green: 'var(--sg-accent-green, #22C55E)',
  amber: 'var(--sg-accent-yellow, #E89B2A)',
  red: 'var(--sg-accent-red, #EF4444)',
};

export default function PPECompliancePanel({ breakdown }: PPECompliancePanelProps) {
  return (
    <div className="op-sidebar-section">
      <div className="op-sidebar-label">PPE Compliance</div>
      {PPE_ITEMS.map(({ key, label, icon }) => {
        const value = breakdown[key as keyof PPEBreakdown] ?? 0;
        const level = getComplianceLevel(value);
        const color = LEVEL_COLORS[level];
        return (
          <div key={key} style={{ marginBottom: 8 }}>
            <div className="op-bar-label">
              <span>{icon} {label}</span>
              <span style={{ color, fontFamily: "'JetBrains Mono', monospace", fontWeight: 600, fontSize: 10 }}>
                {Math.round(value)}%
              </span>
            </div>
            <div className="op-bar-track">
              <div
                className="op-bar-fill"
                style={{ width: `${value}%`, background: color }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}
