'use client';

// Extracted from app/admin/page.tsx (P1-B-11 session 2). Renders one
// company row in the Sites & Customers tab — header with edit/delete
// controls + an inline edit form + the list of the company's sites.
// CRUD calls go through the shared apiFetch/useApiAction helpers.

import { useState } from 'react';
import type { Company, SiteSummary } from '@/types/ironsight';
import { apiFetch, useApiAction } from '@/lib/admin-api-helpers';

interface Props {
  company: Company;
  sites: SiteSummary[];
  onConfigSite: (siteId: string, siteName: string) => void;
  onRefresh: () => void;
}

export default function CompanyCard({ company, sites, onConfigSite, onRefresh }: Props) {
  const [editingCompany, setEditingCompany] = useState(false);
  const [ecName, setEcName] = useState(company.name);
  const [ecContact, setEcContact] = useState(company.contact_name);
  const [ecEmail, setEcEmail] = useState(company.contact_email);
  const [ecPlan, setEcPlan] = useState<string>(company.plan);
  const runApi = useApiAction();

  const handleSaveCompany = async () => {
    const ok = await runApi('Company saved', async () => {
      await apiFetch(`/api/v1/companies/${company.id}`, {
        method: 'PUT',
        body: JSON.stringify({ name: ecName, plan: ecPlan, contact_name: ecContact, contact_email: ecEmail }),
      });
    });
    if (ok) {
      setEditingCompany(false);
      onRefresh();
    }
  };

  const handleDeleteCompany = async () => {
    if (!confirm(`Delete "${company.name}" and all its data?`)) return;
    const ok = await runApi('Company deleted', async () => {
      await apiFetch(`/api/v1/companies/${company.id}`, { method: 'DELETE' });
    });
    if (ok) onRefresh();
  };

  const inp = { padding: '6px 10px', fontSize: 12 } as const;

  return (
    <div className="admin-card" style={{ marginBottom: 16 }}>
      {/* ── Header ── */}
      <div className="admin-card-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ width: 32, height: 32, borderRadius: 6, background: 'rgba(232,115,42,0.1)', border: '1px solid rgba(232,115,42,0.2)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 16 }}>🏢</div>
          <div>
            <div className="admin-card-title">{company.name}</div>
            <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 1 }}>
              {company.contact_name && `${company.contact_name} · `}{company.contact_email} · {sites.length} site{sites.length !== 1 ? 's' : ''}
            </div>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <button className="admin-action-btn" onClick={() => setEditingCompany(true)} title="Edit company">✏️</button>
          <button className="admin-action-btn" onClick={handleDeleteCompany} title="Delete company" style={{ color: 'var(--accent-red)' }}>🗑</button>
          <span className={`admin-plan-badge ${company.plan}`}>{company.plan}</span>
        </div>
      </div>

      {/* ── Edit company form ── */}
      {editingCompany && (
        <div style={{ padding: '12px 18px', background: 'rgba(232,115,42,0.03)', borderBottom: '1px solid rgba(255,255,255,0.04)' }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 8 }}>
            <div><label className="admin-label">Company Name</label><input className="admin-input" value={ecName} onChange={e => setEcName(e.target.value)} style={inp} /></div>
            <div><label className="admin-label">Plan</label><select className="admin-input" value={ecPlan} onChange={e => setEcPlan(e.target.value)} style={inp}><option value="starter">Starter</option><option value="professional">Professional</option><option value="enterprise">Enterprise</option></select></div>
            <div><label className="admin-label">Contact Name</label><input className="admin-input" value={ecContact} onChange={e => setEcContact(e.target.value)} style={inp} /></div>
            <div><label className="admin-label">Contact Email</label><input className="admin-input" value={ecEmail} onChange={e => setEcEmail(e.target.value)} style={inp} /></div>
          </div>
          <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
            <button className="admin-btn admin-btn-ghost" onClick={() => setEditingCompany(false)} style={{ padding: '4px 12px', fontSize: 10 }}>Cancel</button>
            <button className="admin-btn admin-btn-primary" onClick={handleSaveCompany} style={{ padding: '4px 12px', fontSize: 10 }}>Save</button>
          </div>
        </div>
      )}

      {/* ── Sites ── */}
      {sites.map(site => (
        <div key={site.id} style={{ padding: '12px 18px', borderBottom: '1px solid rgba(255,255,255,0.04)', display: 'flex', alignItems: 'center', gap: 16 }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span className={`admin-status-dot ${site.status}`} />
              <span style={{ fontWeight: 600, fontSize: 13 }}>{site.name}</span>
              <span style={{ fontSize: 10, color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}>{site.id}</span>
            </div>
          </div>
          <div style={{ textAlign: 'center', minWidth: 60 }}>
            <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--accent-green)', fontFamily: "'JetBrains Mono', monospace" }}>
              {site.cameras_online}<span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>/{site.cameras_total}</span>
            </div>
            <div style={{ fontSize: 8, color: 'var(--text-muted)', letterSpacing: 1, textTransform: 'uppercase' }}>Cameras</div>
          </div>
          <button
            className="admin-btn admin-btn-ghost"
            onClick={() => onConfigSite(site.id, site.name)}
            style={{ padding: '4px 12px', fontSize: 10, display: 'flex', alignItems: 'center', gap: 4 }}
          >
            Manage
          </button>
        </div>
      ))}
      {sites.length === 0 && <div style={{ padding: '14px 18px', color: 'var(--text-muted)', fontSize: 12 }}>No sites yet</div>}
    </div>
  );
}
