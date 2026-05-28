'use client';

// Extracted from app/admin/page.tsx (P1-B-11 session 2). The Sites &
// Customers tab — searchable, filterable, paginated. Composes
// CompanyCard for the "companies" view mode.

import { useEffect, useMemo, useState } from 'react';
import type { Company, SiteSummary } from '@/types/ironsight';
import CompanyCard from './CompanyCard';

const SITES_PAGE_SIZE = 25;

interface Props {
  sites: SiteSummary[];
  companies: Company[];
  sitesByCompany: Record<string, SiteSummary[]>;
  onCreateSite: () => void;
  onConfigSite: (id: string, name: string) => void;
  onRefresh: () => void;
  showCreateCompany: boolean; setShowCreateCompany: (v: boolean) => void;
  newCompanyName: string; setNewCompanyName: (v: string) => void;
  newCompanyPlan: string; setNewCompanyPlan: (v: string) => void;
  newCompanyContact: string; setNewCompanyContact: (v: string) => void;
  newCompanyEmail: string; setNewCompanyEmail: (v: string) => void;
  creatingCompany: boolean; handleCreateCompany: () => void;
}

export default function SitesAndCustomersTab({
  sites, companies, sitesByCompany, onCreateSite, onConfigSite, onRefresh,
  showCreateCompany, setShowCreateCompany, newCompanyName, setNewCompanyName,
  newCompanyPlan, setNewCompanyPlan, newCompanyContact, setNewCompanyContact,
  newCompanyEmail, setNewCompanyEmail, creatingCompany, handleCreateCompany,
}: Props) {
  const [search, setSearch] = useState('');
  const [companyFilter, setCompanyFilter] = useState<string>('all');
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [viewMode, setViewMode] = useState<'sites' | 'companies'>('sites');
  const [page, setPage] = useState(0);

  const companyMap = useMemo(() => {
    const m: Record<string, Company> = {};
    for (const c of companies) m[c.id] = c;
    return m;
  }, [companies]);

  const filteredSites = useMemo(() => {
    let result = sites;
    if (search.trim()) {
      const q = search.toLowerCase();
      result = result.filter(s =>
        s.name.toLowerCase().includes(q) ||
        s.id.toLowerCase().includes(q) ||
        (companyMap[s.company_id || '']?.name || '').toLowerCase().includes(q)
      );
    }
    if (companyFilter !== 'all') {
      result = result.filter(s => s.company_id === companyFilter);
    }
    if (statusFilter !== 'all') {
      result = result.filter(s => (s.status as string) === statusFilter);
    }
    return result;
  }, [sites, search, companyFilter, statusFilter, companyMap]);

  const totalPages = Math.ceil(filteredSites.length / SITES_PAGE_SIZE);
  const pagedSites = filteredSites.slice(page * SITES_PAGE_SIZE, (page + 1) * SITES_PAGE_SIZE);

  // Reset page when filters change
  useEffect(() => { setPage(0); }, [search, companyFilter, statusFilter]);

  const statusCounts = useMemo(() => {
    const counts: Record<string, number> = { active: 0, idle: 0, critical: 0, archived: 0 };
    for (const s of sites) counts[s.status] = (counts[s.status] || 0) + 1;
    return counts;
  }, [sites]);

  return (
    <div style={{ padding: 24 }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <div>
          <div style={{ fontSize: 18, fontWeight: 700 }}>Companies & Sites</div>
          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
            {companies.length} companies · {sites.length} sites
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="admin-btn admin-btn-ghost" onClick={() => setShowCreateCompany(true)}>+ Add Company</button>
          <button className="admin-btn admin-btn-primary" onClick={onCreateSite}>+ Create Site</button>
        </div>
      </div>

      {/* Create Company form */}
      {showCreateCompany && (
        <div className="admin-card" style={{ marginBottom: 16, border: '1px solid rgba(232,115,42,0.2)' }}>
          <div className="admin-card-header" style={{ background: 'rgba(232,115,42,0.05)' }}>
            <div className="admin-card-title">New Company</div>
            <button className="admin-modal-close" onClick={() => setShowCreateCompany(false)}>x</button>
          </div>
          <div style={{ padding: 18, display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <div><label className="admin-label">Company Name *</label><input className="admin-input" value={newCompanyName} onChange={e => setNewCompanyName(e.target.value)} placeholder="e.g. Turner Construction" /></div>
            <div><label className="admin-label">Plan</label><select className="admin-input" value={newCompanyPlan} onChange={e => setNewCompanyPlan(e.target.value)}><option value="starter">Starter</option><option value="professional">Professional</option><option value="enterprise">Enterprise</option></select></div>
            <div><label className="admin-label">Contact Name</label><input className="admin-input" value={newCompanyContact} onChange={e => setNewCompanyContact(e.target.value)} placeholder="Primary contact" /></div>
            <div><label className="admin-label">Contact Email</label><input className="admin-input" value={newCompanyEmail} onChange={e => setNewCompanyEmail(e.target.value)} placeholder="email@company.com" type="email" /></div>
          </div>
          <div style={{ padding: '0 18px 14px', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <button className="admin-btn admin-btn-ghost" onClick={() => setShowCreateCompany(false)}>Cancel</button>
            <button className="admin-btn admin-btn-primary" onClick={handleCreateCompany} disabled={!newCompanyName.trim() || creatingCompany}>
              {creatingCompany ? 'Creating...' : 'Create Company'}
            </button>
          </div>
        </div>
      )}

      {/* Search + Filters bar */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center' }}>
        <input
          className="admin-input"
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="Search sites, companies, IDs..."
          style={{ flex: 1, minWidth: 200, padding: '7px 12px', fontSize: 12 }}
        />
        <select
          className="admin-input"
          value={companyFilter}
          onChange={e => setCompanyFilter(e.target.value)}
          style={{ padding: '7px 10px', fontSize: 11, cursor: 'pointer', minWidth: 160 }}
        >
          <option value="all">All Companies ({companies.length})</option>
          {companies.map(c => (
            <option key={c.id} value={c.id}>{c.name} ({(sitesByCompany[c.id] || []).length})</option>
          ))}
        </select>
        <select
          className="admin-input"
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
          style={{ padding: '7px 10px', fontSize: 11, cursor: 'pointer', minWidth: 130 }}
        >
          <option value="all">All Status</option>
          <option value="active">Active ({statusCounts.active})</option>
          <option value="critical">Critical ({statusCounts.critical})</option>
          <option value="idle">Idle ({statusCounts.idle})</option>
          <option value="archived">Archived ({statusCounts.archived || 0})</option>
        </select>
        {/* View toggle */}
        <div style={{ display: 'flex', border: '1px solid rgba(255,255,255,0.08)', borderRadius: 4, overflow: 'hidden' }}>
          {(['sites', 'companies'] as const).map(v => (
            <button
              key={v}
              type="button"
              onClick={() => setViewMode(v)}
              style={{
                padding: '6px 12px', fontSize: 11, fontWeight: 500, cursor: 'pointer',
                background: viewMode === v ? 'rgba(255,255,255,0.06)' : 'transparent',
                color: viewMode === v ? '#E4E8F0' : '#4A5268',
                border: 'none', fontFamily: 'inherit', textTransform: 'capitalize',
              }}
            >
              {v}
            </button>
          ))}
        </div>
      </div>

      {/* ── Sites Table View ── */}
      {viewMode === 'sites' && (
        <>
          <div style={{ background: 'var(--bg-card, #151921)', border: '1px solid rgba(255,255,255,0.06)', borderRadius: 8, overflow: 'hidden' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
                  {['Status', 'Site', 'Company', 'Cameras', 'Incidents', 'Tier', ''].map(h => (
                    <th key={h} style={{ padding: '10px 14px', textAlign: 'left', fontSize: 9, fontWeight: 600, letterSpacing: 1.2, textTransform: 'uppercase', color: '#4A5268', background: 'rgba(255,255,255,0.02)' }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {pagedSites.map(site => {
                  const company = companyMap[site.company_id || ''];
                  const mode = (site.feature_mode ?? 'security_and_safety') as string;
                  const st = site.status as string;
                  const statusColor = st === 'critical' ? '#EF4444' : st === 'active' ? '#22C55E' : st === 'archived' ? '#4A5268' : '#E89B2A';
                  return (
                    <tr key={site.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.04)', cursor: 'pointer', transition: 'background 0.1s' }}
                      onClick={() => onConfigSite(site.id, site.name)}
                      onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}
                      onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                    >
                      <td style={{ padding: '10px 14px' }}>
                        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 10, fontWeight: 600, color: statusColor }}>
                          <span style={{ width: 6, height: 6, borderRadius: '50%', background: statusColor }} />
                          {site.status}
                        </span>
                      </td>
                      <td style={{ padding: '10px 14px' }}>
                        <div style={{ fontSize: 12, fontWeight: 600, color: '#E4E8F0' }}>{site.name}</div>
                        <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>{site.id}</div>
                      </td>
                      <td style={{ padding: '10px 14px', fontSize: 11, color: '#8891A5' }}>
                        {company?.name || '—'}
                      </td>
                      <td style={{ padding: '10px 14px', fontSize: 12, fontFamily: "'JetBrains Mono', monospace" }}>
                        <span style={{ color: site.cameras_online === site.cameras_total ? '#22C55E' : '#E89B2A' }}>{site.cameras_online}</span>
                        <span style={{ color: '#4A5268' }}>/{site.cameras_total}</span>
                      </td>
                      <td style={{ padding: '10px 14px' }}>
                        {site.open_incidents > 0
                          ? <span style={{ fontSize: 11, fontWeight: 600, color: '#EF4444' }}>{site.open_incidents}</span>
                          : <span style={{ fontSize: 11, color: '#22C55E' }}>0</span>}
                      </td>
                      <td style={{ padding: '10px 14px' }}>
                        <span style={{
                          fontSize: 8, fontWeight: 700, padding: '2px 6px', borderRadius: 3, letterSpacing: 0.3,
                          background: mode === 'security_only' ? 'rgba(59,130,246,0.1)' : 'rgba(168,85,247,0.1)',
                          color: mode === 'security_only' ? '#3B82F6' : '#a855f7',
                          border: `1px solid ${mode === 'security_only' ? 'rgba(59,130,246,0.25)' : 'rgba(168,85,247,0.25)'}`,
                        }}>
                          {mode === 'security_only' ? 'SEC' : 'SEC+SAFETY'}
                        </span>
                      </td>
                      <td style={{ padding: '10px 14px' }}>
                        <button
                          className="admin-btn admin-btn-ghost"
                          onClick={e => { e.stopPropagation(); onConfigSite(site.id, site.name); }}
                          style={{ padding: '3px 10px', fontSize: 10 }}
                        >
                          Manage
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {filteredSites.length === 0 && (
              <div style={{ padding: 30, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
                {search ? `No sites matching "${search}"` : 'No sites configured yet'}
              </div>
            )}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 12, fontSize: 11, color: '#4A5268' }}>
              <span>Showing {page * SITES_PAGE_SIZE + 1}–{Math.min((page + 1) * SITES_PAGE_SIZE, filteredSites.length)} of {filteredSites.length}</span>
              <div style={{ display: 'flex', gap: 4 }}>
                <button
                  type="button"
                  disabled={page === 0}
                  onClick={() => setPage(p => p - 1)}
                  style={{ padding: '4px 10px', borderRadius: 4, fontSize: 11, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: page === 0 ? '#2a3040' : '#8891A5', cursor: page === 0 ? 'default' : 'pointer', fontFamily: 'inherit' }}
                >
                  Previous
                </button>
                <span style={{ padding: '4px 10px', fontSize: 11 }}>Page {page + 1} of {totalPages}</span>
                <button
                  type="button"
                  disabled={page >= totalPages - 1}
                  onClick={() => setPage(p => p + 1)}
                  style={{ padding: '4px 10px', borderRadius: 4, fontSize: 11, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: page >= totalPages - 1 ? '#2a3040' : '#8891A5', cursor: page >= totalPages - 1 ? 'default' : 'pointer', fontFamily: 'inherit' }}
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {/* ── Companies View ── */}
      {viewMode === 'companies' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {companies.filter(c => {
            if (search.trim()) {
              const q = search.toLowerCase();
              return c.name.toLowerCase().includes(q) || c.contact_email?.toLowerCase().includes(q);
            }
            return true;
          }).map(company => {
            const companySites = sitesByCompany[company.id] || [];
            return (
              <CompanyCard
                key={company.id}
                company={company}
                sites={companySites}
                onConfigSite={onConfigSite}
                onRefresh={onRefresh}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
