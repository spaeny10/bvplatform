'use client';

import { useState, useEffect } from 'react';
import { getIntegrations, toggleIntegration, deleteIntegration } from '@/lib/ironsight-api';
import type { Integration } from '@/types/ironsight';

const TYPE_META: Record<string, { icon: string; color: string }> = {
  webhook: { icon: '🔗', color: '#E8732A' },
  slack: { icon: '💬', color: '#E01E5A' },
  teams: { icon: '🟦', color: '#6264A7' },
  api_key: { icon: '🔑', color: '#E89B2A' },
};

export default function IntegrationHub() {
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getIntegrations().then(data => { setIntegrations(data); setLoading(false); });
  }, []);

  const handleToggle = async (id: string, active: boolean) => {
    setIntegrations(prev => prev.map(i => i.id === id ? { ...i, active } : i));
    await toggleIntegration(id, active);
  };

  const handleDelete = async (id: string) => {
    await deleteIntegration(id);
    setIntegrations(prev => prev.filter(i => i.id !== id));
  };

  return (
    <div className="admin-card" style={{ marginTop: 16 }}>
      <div className="admin-card-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 16 }}>🔌</span>
          <div>
            <div className="admin-card-title">Integrations</div>
            <div style={{ fontSize: 10, color: '#4A5268', marginTop: 1 }}>{integrations.filter(i => i.active).length} active</div>
          </div>
        </div>
      </div>

      {loading && <div style={{ padding: 30, textAlign: 'center', color: '#4A5268' }}>Loading…</div>}

      {!loading && integrations.map(integ => {
        const meta = TYPE_META[integ.type] || { icon: '•', color: '#8891A5' };
        return (
          <div key={integ.id} style={{
            display: 'flex', alignItems: 'center', gap: 12, padding: '10px 16px',
            borderBottom: '1px solid rgba(255,255,255,0.04)',
            opacity: integ.active ? 1 : 0.4,
          }}>
            <span style={{ fontSize: 20 }}>{meta.icon}</span>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: '#E4E8F0' }}>{integ.name}</div>
              <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>
                {integ.type.toUpperCase()} · {integ.site_ids.length || 'All'} sites
                {integ.last_triggered && ` · Last: ${new Date(integ.last_triggered).toLocaleString()}`}
              </div>
            </div>
            <span style={{
              fontSize: 8, padding: '2px 8px', borderRadius: 10, fontWeight: 600,
              textTransform: 'uppercase' as const, letterSpacing: 0.5,
              background: `${meta.color}15`, color: meta.color, border: `1px solid ${meta.color}30`,
            }}>{integ.type}</span>
            <label style={{ cursor: 'pointer', display: 'flex', alignItems: 'center' }}>
              <input type="checkbox" checked={integ.active} onChange={e => handleToggle(integ.id, e.target.checked)} style={{ accentColor: '#8b5cf6', width: 14, height: 14 }} />
            </label>
            <button className="admin-btn admin-btn-danger" style={{ fontSize: 9, padding: '2px 6px' }} onClick={() => handleDelete(integ.id)}>✕</button>
          </div>
        );
      })}
    </div>
  );
}
