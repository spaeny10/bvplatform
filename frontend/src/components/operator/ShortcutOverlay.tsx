'use client';

interface Props { onClose: () => void; }

const SHORTCUTS = [
  { key: 'Space', action: 'Acknowledge selected alert', group: 'Alerts' },
  { key: 'C', action: 'Claim selected alert', group: 'Alerts' },
  { key: 'R', action: 'Release alert ownership', group: 'Alerts' },
  { key: 'E', action: 'Escalate alert', group: 'Alerts' },
  { key: 'L', action: 'Lock/Unlock selected site', group: 'Sites' },
  { key: '↑ / ↓', action: 'Navigate site list', group: 'Sites' },
  { key: '1-6', action: 'Focus camera 1-6', group: 'Cameras' },
  { key: 'F', action: 'Fullscreen selected camera', group: 'Cameras' },
  { key: 'H', action: 'Open shift handoff', group: 'SOC' },
  { key: 'S', action: 'Open SOPs for locked site', group: 'SOC' },
  { key: 'M', action: 'Open site map', group: 'SOC' },
  { key: '?', action: 'Toggle this overlay', group: 'General' },
  { key: 'Esc', action: 'Close modal / deselect', group: 'General' },
];

const GROUPS = ['Alerts', 'Sites', 'Cameras', 'SOC', 'General'];

export default function ShortcutOverlay({ onClose }: Props) {
  return (
    <div style={{
      position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.8)', backdropFilter: 'blur(8px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 9999,
    }} onClick={onClose}>
      <div onClick={e => e.stopPropagation()} style={{
        background: '#0E1117', border: '1px solid rgba(139,92,246,0.2)', borderRadius: 10,
        padding: '24px 28px', width: 440, boxShadow: '0 20px 60px rgba(0,0,0,0.6)',
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <div style={{ fontSize: 16, fontWeight: 700, letterSpacing: 0.5 }}>⌨ Keyboard Shortcuts</div>
          <button onClick={onClose} style={{
            background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
            color: '#4A5268', borderRadius: 4, padding: '3px 8px', cursor: 'pointer', fontSize: 11,
          }}>ESC</button>
        </div>

        {GROUPS.map(group => (
          <div key={group} style={{ marginBottom: 14 }}>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268', marginBottom: 6 }}>
              {group}
            </div>
            {SHORTCUTS.filter(s => s.group === group).map(s => (
              <div key={s.key} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '4px 0' }}>
                <span style={{ fontSize: 12, color: '#8891A5' }}>{s.action}</span>
                <kbd style={{
                  padding: '2px 8px', fontSize: 10, fontWeight: 600,
                  background: 'rgba(255,255,255,0.06)', border: '1px solid rgba(255,255,255,0.1)',
                  borderRadius: 3, color: '#E4E8F0', fontFamily: "'JetBrains Mono', monospace",
                  minWidth: 28, textAlign: 'center',
                }}>{s.key}</kbd>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
