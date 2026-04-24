'use client';

interface PPEStatusIconProps {
  present: boolean;
  item: string;   // "hard_hat" | "harness" | "hi_vis" | "boots" | "gloves"
  size?: number;
}

const ITEM_LABELS: Record<string, string> = {
  hard_hat: 'Hard Hat',
  harness: 'Harness',
  hi_vis: 'Hi-Vis',
  boots: 'Boots',
  gloves: 'Gloves',
};

const ITEM_ICONS: Record<string, string> = {
  hard_hat: '⛑',
  harness: '🔗',
  hi_vis: '🦺',
  boots: '👢',
  gloves: '🧤',
};

export default function PPEStatusIcon({ present, item, size = 18 }: PPEStatusIconProps) {
  const label = ITEM_LABELS[item] || item;
  const icon = ITEM_ICONS[item] || '•';

  return (
    <span
      title={`${label}: ${present ? 'OK' : 'MISSING'}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        fontSize: size * 0.6,
        color: present ? 'var(--ppe-ok, #40c080)' : 'var(--ppe-fail, #e05040)',
        fontWeight: 600,
      }}
    >
      <span style={{ fontSize: size * 0.8 }}>{icon}</span>
      <span style={{
        width: size * 0.65,
        height: size * 0.65,
        borderRadius: '50%',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize: size * 0.5,
        background: present ? 'rgba(64,192,128,0.15)' : 'rgba(224,80,64,0.15)',
        border: `1px solid ${present ? 'rgba(64,192,128,0.3)' : 'rgba(224,80,64,0.3)'}`,
        flexShrink: 0,
      }}>
        {present ? '✓' : '✗'}
      </span>
    </span>
  );
}
