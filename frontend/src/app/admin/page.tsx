'use client';

import { useMemo, useState, useCallback, useEffect } from 'react';
import './admin.css';
import Link from 'next/link';
import type { Company, SiteSummary } from '@/types/ironsight';
import { useSites } from '@/hooks/useSites';
import { useCompanies } from '@/hooks/useCustomers';
import { useAdminStore } from '@/stores/admin-store';
import { createCompany } from '@/lib/ironsight-api';
import { BRAND } from '@/lib/branding';
import CreateSiteModal from '@/components/admin/CreateSiteModal';
import SiteConfigModal from '@/components/admin/SiteConfigModal';
import AuditLogPanel from '@/components/admin/AuditLogPanel';
import OperatorAnalyticsPanel from '@/components/admin/OperatorAnalyticsPanel';
import IntegrationHub from '@/components/admin/IntegrationHub';
import AuditLogExport from '@/components/admin/AuditLogExport';
import Logo from '@/components/shared/Logo';
import UserChip from '@/components/shared/UserChip';
import SettingsPage from '@/components/SettingsPage';
import HealthDashboard from '@/components/HealthDashboard';
import RecordingHealthCard from '@/components/RecordingHealthCard';
import { listCameras, Camera, listUsers, createUser, deleteUser, updateUserPassword, updateUserRole, updateUserProfile, UserPublic } from '@/lib/api';
import { useAuth } from '@/contexts/AuthContext';
import { ToastProvider, useToast } from '@/components/ToastProvider';
import { SkeletonRows } from '@/components/shared/Skeleton';

type Tab = 'sites' | 'operators' | 'users' | 'settings' | 'health' | 'audit' | 'integrations';

/**
 * Authenticated fetch for /api/v1 calls. Throws on non-2xx so the
 * caller's try/catch surfaces a user-visible error rather than the
 * action silently no-op-ing. The thrown Error includes the response
 * body when the server provided one (most error handlers do
 * http.Error which is plain text), so a toast can show "delete
 * failed: foreign key constraint…" instead of an opaque "something
 * went wrong."
 */
async function apiFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : null;
  const res = await fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init.headers,
    },
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(body || `${init.method || 'GET'} ${url} failed (${res.status})`);
  }
  return res;
}

/**
 * Toast-wrapped action runner. Used by admin handlers to bracket
 * their apiFetch calls with consistent success/error notifications.
 * Returns true on success, false on error — callers that want
 * post-action UX (e.g. close a modal only on success) can branch.
 */
function useApiAction(): (label: string, fn: () => Promise<void>) => Promise<boolean> {
  const toast = useToast();
  return async (label, fn) => {
    try {
      await fn();
      toast.push({ type: 'success', title: label, duration: 2500 });
      return true;
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      toast.push({ type: 'error', title: label + ' failed', body: msg, duration: 6000 });
      return false;
    }
  };
}

function CompanyCard({ company, sites, onConfigSite, onRefresh }: {
  company: Company;
  sites: SiteSummary[];
  onConfigSite: (siteId: string, siteName: string) => void;
  onRefresh: () => void;
}) {
  // ── Edit company ──
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

  const handleDeleteSite = async (siteId: string) => {
    if (!confirm(`Delete site ${siteId}? This will remove all its cameras and SOPs.`)) return;
    const ok = await runApi(`Site ${siteId} deleted`, async () => {
      await apiFetch(`/api/v1/sites/${siteId}`, { method: 'DELETE' });
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

export default function AdminPage() {
  const { user } = useAuth();
  const { data: sites = [], refetch: refetchSites } = useSites();
  const { data: companies = [], refetch: refetchCompanies } = useCompanies();
  const {
    showCreateSiteModal, editingSiteId,
    openCreateSite, closeModals,
  } = useAdminStore();

  const [activeTab, setActiveTab] = useState<Tab>('sites');
  const [configSiteId, setConfigSiteId] = useState<string | null>(null);
  const [configSiteName, setConfigSiteName] = useState('');

  // Cameras — loaded lazily when Settings or Health tab is first opened
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [camerasLoaded, setCamerasLoaded] = useState(false);

  const ensureCameras = useCallback(async () => {
    if (camerasLoaded) return;
    try {
      const data = await listCameras();
      setCameras(Array.isArray(data) ? data : []);
    } catch { /* non-fatal */ }
    setCamerasLoaded(true);
  }, [camerasLoaded]);

  const [showAuditExport, setShowAuditExport] = useState(false);

  // ── Create Company form state ──
  const [showCreateCompany, setShowCreateCompany] = useState(false);
  const [newCompanyName, setNewCompanyName] = useState('');
  const [newCompanyContact, setNewCompanyContact] = useState('');
  const [newCompanyEmail, setNewCompanyEmail] = useState('');
  const [newCompanyPlan, setNewCompanyPlan] = useState('professional');
  const [creatingCompany, setCreatingCompany] = useState(false);

  const handleCreateCompany = useCallback(async () => {
    if (!newCompanyName.trim()) return;
    setCreatingCompany(true);
    await createCompany({
      name: newCompanyName.trim(),
      contact_name: newCompanyContact.trim(),
      contact_email: newCompanyEmail.trim(),
      plan: newCompanyPlan as any,
      logo_url: undefined,
    });
    setCreatingCompany(false);
    setShowCreateCompany(false);
    setNewCompanyName('');
    setNewCompanyContact('');
    setNewCompanyEmail('');
    refetchCompanies();
  }, [newCompanyName, newCompanyContact, newCompanyEmail, newCompanyPlan, refetchCompanies]);

  // ── Operator management state ──
  const [operators, setOperators] = useState<Array<{ id: string; name: string; callsign: string; status: string; email?: string }>>([]);
  const [showCreateOperator, setShowCreateOperator] = useState(false);
  const [newOpName, setNewOpName] = useState('');
  const [newOpCallsign, setNewOpCallsign] = useState('');
  const [newOpEmail, setNewOpEmail] = useState('');
  const [newOpUsername, setNewOpUsername] = useState('');
  const [newOpPassword, setNewOpPassword] = useState('');
  const [creatingOp, setCreatingOp] = useState(false);
  const [opMsg, setOpMsg] = useState<{ ok: boolean; text: string } | null>(null);

  // Load operators from API
  useEffect(() => {
    apiFetch('/api/v1/operators')
      .then(r => r.json())
      .then(data => { if (Array.isArray(data)) setOperators(data); })
      .catch(() => {})
      .finally(() => {});
  }, []);

  const handleCreateOperator = useCallback(async () => {
    if (!newOpName.trim() || !newOpCallsign.trim()) return;
    setCreatingOp(true);
    setOpMsg(null);
    try {
      const res = await apiFetch('/api/v1/operators', {
        method: 'POST',
        body: JSON.stringify({
          name: newOpName.trim(),
          callsign: newOpCallsign.trim(),
          email: newOpEmail.trim(),
          username: newOpUsername.trim() || undefined,
          password: newOpPassword.trim() || undefined,
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      const op = await res.json();
      setOperators(prev => [...prev, op]);
      setShowCreateOperator(false);
      setNewOpName(''); setNewOpCallsign(''); setNewOpEmail('');
      setNewOpUsername(''); setNewOpPassword('');
      setOpMsg({ ok: true, text: `Operator ${op.callsign} created.` });
    } catch (e: any) {
      setOpMsg({ ok: false, text: e.message ?? 'Failed to create operator.' });
    }
    setCreatingOp(false);
  }, [newOpName, newOpCallsign, newOpEmail, newOpUsername, newOpPassword]);

  const totalCameras = sites.reduce((s, site) => s + site.cameras_total, 0);

  const sitesByCompany = useMemo(() => {
    const grouped: Record<string, typeof sites> = {};
    companies.forEach(c => { grouped[c.id] = []; });
    grouped['unassigned'] = [];
    sites.forEach(s => {
      const cid = s.company_id || 'unassigned';
      if (grouped[cid]) grouped[cid].push(s);
      else grouped['unassigned'].push(s);
    });
    return grouped;
  }, [sites, companies]);

  // ── User management state ──
  const [platformUsers, setPlatformUsers] = useState<UserPublic[]>([]);
  const [usersLoading, setUsersLoading] = useState(false);
  const [usersLoaded, setUsersLoaded] = useState(false);
  const [showCreateUser, setShowCreateUser] = useState(false);
  const [newUsername, setNewUsername] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [newDisplayName, setNewDisplayName] = useState('');
  const [newEmail, setNewEmail] = useState('');
  const [newPhone, setNewPhone] = useState('');
  const [newUserType, setNewUserType] = useState<'internal' | 'customer'>('internal');
  const [newUserRole, setNewUserRole] = useState('soc_operator');
  const [newUserIsAdmin, setNewUserIsAdmin] = useState(false);
  const [newUserOrgId, setNewUserOrgId] = useState('');
  const [creatingUser, setCreatingUser] = useState(false);

  // ── Edit profile state ──
  const [editingUserId, setEditingUserId] = useState<string | null>(null);
  const [editDisplayName, setEditDisplayName] = useState('');
  const [editEmail, setEditEmail] = useState('');
  const [editPhone, setEditPhone] = useState('');
  const [editOrgId, setEditOrgId] = useState('');
  const [savingProfile, setSavingProfile] = useState(false);
  const [editMsg, setEditMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [userMsg, setUserMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [pwdTargetId, setPwdTargetId] = useState<string | null>(null);
  const [pwdValue, setPwdValue] = useState('');
  const [pwdMsg, setPwdMsg] = useState<{ ok: boolean; text: string } | null>(null);

  const loadPlatformUsers = useCallback(async () => {
    setUsersLoading(true);
    try {
      const data = await listUsers();
      setPlatformUsers(Array.isArray(data) ? data : []);
    } catch { /* non-fatal */ }
    setUsersLoading(false);
    setUsersLoaded(true);
  }, []);

  const handleCreateUser = useCallback(async () => {
    if (!newUsername.trim() || !newPassword.trim()) {
      setUserMsg({ ok: false, text: 'Username and password are required.' });
      return;
    }
    setCreatingUser(true);
    setUserMsg(null);
    try {
      const effectiveRole = newUserType === 'internal'
        ? (newUserIsAdmin ? 'admin' : 'soc_operator')
        : newUserRole;
      await createUser({
        username: newUsername.trim(),
        password: newPassword.trim(),
        role: effectiveRole,
        display_name: newDisplayName.trim() || undefined,
        email: newEmail.trim() || undefined,
        phone: newPhone.trim() || undefined,
        organization_id: newUserType === 'customer' && newUserOrgId ? newUserOrgId : undefined,
      });
      setNewUsername(''); setNewPassword(''); setNewDisplayName('');
      setNewEmail(''); setNewPhone('');
      setNewUserType('internal'); setNewUserRole('soc_operator'); setNewUserIsAdmin(false); setNewUserOrgId('');
      setShowCreateUser(false);
      setUserMsg({ ok: true, text: 'User created.' });
      await loadPlatformUsers();
    } catch (e: any) {
      setUserMsg({ ok: false, text: e.message ?? 'Failed to create user.' });
    }
    setCreatingUser(false);
  }, [newUsername, newPassword, newDisplayName, newEmail, newPhone, newUserRole, loadPlatformUsers]);

  const handleDeleteUser = useCallback(async (id: string, username: string) => {
    if (!confirm(`Delete user "${username}"? This cannot be undone.`)) return;
    try {
      await deleteUser(id);
      setPlatformUsers(prev => prev.filter(u => u.id !== id));
    } catch (e: any) { alert('Delete failed: ' + e.message); }
  }, []);

  const handleRoleChange = useCallback(async (id: string, role: string) => {
    try {
      await updateUserRole(id, role);
      setPlatformUsers(prev => prev.map(u => u.id === id ? { ...u, role } : u));
    } catch (e: any) { alert('Role change failed: ' + e.message); }
  }, []);

  const handleChangePassword = useCallback(async () => {
    if (!pwdTargetId || !pwdValue.trim()) return;
    setPwdMsg(null);
    try {
      await updateUserPassword(pwdTargetId, pwdValue.trim());
      setPwdTargetId(null);
      setPwdValue('');
      setPwdMsg({ ok: true, text: 'Password updated.' });
    } catch (e: any) {
      setPwdMsg({ ok: false, text: e.message ?? 'Failed.' });
    }
  }, [pwdTargetId, pwdValue]);

  const openEditProfile = useCallback((u: UserPublic) => {
    setEditingUserId(u.id);
    setEditDisplayName(u.display_name || '');
    setEditEmail(u.email || '');
    setEditPhone(u.phone || '');
    setEditOrgId(u.organization_id || '');
    setEditMsg(null);
  }, []);

  const handleSaveProfile = useCallback(async () => {
    if (!editingUserId) return;
    setSavingProfile(true);
    setEditMsg(null);
    try {
      const updated = await updateUserProfile(editingUserId, {
        display_name: editDisplayName.trim() || undefined,
        email: editEmail.trim() || undefined,
        phone: editPhone.trim() || undefined,
        organization_id: editOrgId || undefined,
      });
      setPlatformUsers(prev => prev.map(u => u.id === editingUserId ? updated : u));
      setEditingUserId(null);
      setEditMsg({ ok: true, text: 'Profile updated.' });
    } catch (e: any) {
      setEditMsg({ ok: false, text: e.message ?? 'Failed to save.' });
    }
    setSavingProfile(false);
  }, [editingUserId, editDisplayName, editEmail, editPhone, editOrgId]);

  const handleTabChange = useCallback((tab: Tab) => {
    setActiveTab(tab);
    if (tab === 'settings' || tab === 'health') ensureCameras();
    if ((tab === 'users' || tab === 'operators') && !usersLoaded) loadPlatformUsers();
  }, [ensureCameras, usersLoaded, loadPlatformUsers]);

  const tabs: { key: Tab; label: string; count?: number }[] = [
    { key: 'sites',        label: 'Sites & Customers', count: sites.length },
    { key: 'operators',    label: 'Operators',          count: operators.length },
    { key: 'users',        label: 'Users',              count: usersLoaded ? platformUsers.length : undefined },
    { key: 'settings',     label: 'NVR Settings' },
    { key: 'health',       label: 'Health' },
    { key: 'audit',        label: 'Audit Trail' },
    { key: 'integrations', label: 'Integrations' },
  ];

  const statusColors: Record<string, string> = {
    available: 'var(--accent-green, #22C55E)',
    engaged: 'var(--accent-red, #EF4444)',
    wrap_up: 'var(--accent-amber, #E89B2A)',
    away: 'var(--text-muted, #4A5268)',
  };

  return (
    <div className="admin-shell">
      {/* ── Top Bar ── */}
      <div className="admin-topbar">
        <div className="admin-brand">
          <Logo height={20} />
          <span style={{ fontWeight: 400, color: 'var(--text-secondary, #8891A5)', fontSize: 14, marginLeft: 4 }}>Admin</span>
        </div>
        <div className="admin-nav">
          <Link href="/operator" className="admin-nav-item">SOC Monitor</Link>
          <Link href="/portal" className="admin-nav-item">Portal</Link>
          <Link href="/reports" className="admin-nav-item">Reports</Link>
          <span className="admin-nav-item active">Admin</span>
          <Link href="/" className="admin-nav-item">NVR</Link>
        </div>
        <UserChip />
      </div>

      {/* ── Stats Bar ── */}
      <div style={{
        display: 'flex', gap: 24, padding: '14px 24px',
        borderBottom: '1px solid var(--border, rgba(255,255,255,0.07))',
        background: 'var(--bg-secondary, #0E1117)', flexShrink: 0,
      }}>
        {[
          { label: 'Companies', value: companies.length, color: 'var(--accent-orange)' },
          { label: 'Sites', value: sites.length, color: 'var(--accent-blue)' },
          { label: 'Cameras', value: totalCameras, color: 'var(--accent-green)' },
          { label: 'Operators', value: operators.length, color: 'var(--accent-purple)' },
        ].map(stat => (
          <div key={stat.label} style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
            <span style={{ fontSize: 22, fontWeight: 700, color: stat.color, fontFamily: "'JetBrains Mono', monospace" }}>
              {stat.value}
            </span>
            <span style={{ fontSize: 10, color: 'var(--text-muted)', letterSpacing: 1, textTransform: 'uppercase', fontWeight: 600 }}>
              {stat.label}
            </span>
          </div>
        ))}
      </div>

      {/* ── Tab Bar ── */}
      <div style={{
        display: 'flex', gap: 2, padding: '0 24px',
        borderBottom: '1px solid var(--border, rgba(255,255,255,0.07))',
        background: 'var(--bg-secondary, #0E1117)', flexShrink: 0,
      }}>
        {tabs.map(tab => (
          <button
            key={tab.key}
            onClick={() => handleTabChange(tab.key)}
            style={{
              padding: '10px 16px', fontSize: 12, fontWeight: 600,
              color: activeTab === tab.key ? 'var(--accent-orange)' : 'var(--text-muted)',
              background: 'none', border: 'none', cursor: 'pointer',
              borderBottom: activeTab === tab.key ? '2px solid var(--accent-orange)' : '2px solid transparent',
              fontFamily: 'inherit', letterSpacing: 0.5,
              display: 'flex', alignItems: 'center', gap: 6, marginBottom: -1,
            }}
          >
            {tab.label}
            {tab.count !== undefined && (
              <span style={{
                fontSize: 9, padding: '1px 6px', borderRadius: 8,
                background: activeTab === tab.key ? 'rgba(232,115,42,0.15)' : 'rgba(255,255,255,0.04)',
                color: activeTab === tab.key ? 'var(--accent-orange)' : 'var(--text-muted)',
              }}>{tab.count}</span>
            )}
          </button>
        ))}
      </div>

      {/* ── Tab Content ── */}
      <div style={{ flex: 1, overflowY: 'auto', scrollbarWidth: 'thin' }}>

        {/* ══════ SITES & CUSTOMERS TAB ══════ */}
        {activeTab === 'sites' && (() => {
          // Search and filter state is inline here to avoid lifting to parent
          return <SitesAndCustomersTab
            sites={sites}
            companies={companies}
            sitesByCompany={sitesByCompany}
            onCreateSite={openCreateSite}
            onConfigSite={(id: string, name: string) => { setConfigSiteId(id); setConfigSiteName(name); }}
            onRefresh={() => { refetchSites(); refetchCompanies(); }}
            showCreateCompany={showCreateCompany}
            setShowCreateCompany={setShowCreateCompany}
            newCompanyName={newCompanyName} setNewCompanyName={setNewCompanyName}
            newCompanyPlan={newCompanyPlan} setNewCompanyPlan={setNewCompanyPlan}
            newCompanyContact={newCompanyContact} setNewCompanyContact={setNewCompanyContact}
            newCompanyEmail={newCompanyEmail} setNewCompanyEmail={setNewCompanyEmail}
            creatingCompany={creatingCompany} handleCreateCompany={handleCreateCompany}
          />;
        })()}

        {/* Keep old code for reference — company create form is now inside SitesAndCustomersTab */}
        {false && activeTab === 'sites' && (
          <div style={{ padding: 24 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 }}>
              <div>
                <div style={{ fontSize: 18, fontWeight: 700 }}>Companies & Sites</div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                  Manage customer organizations and their monitored sites
                </div>
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="admin-btn admin-btn-ghost" onClick={() => setShowCreateCompany(true)}>+ Add Company</button>
                <button className="admin-btn admin-btn-primary" onClick={openCreateSite}>+ Create Site</button>
              </div>
            </div>

            {/* Create Company inline form */}
            {showCreateCompany && (
              <div className="admin-card" style={{ marginBottom: 16, border: '1px solid rgba(232,115,42,0.2)' }}>
                <div className="admin-card-header" style={{ background: 'rgba(232,115,42,0.05)' }}>
                  <div className="admin-card-title">New Company</div>
                  <button className="admin-modal-close" onClick={() => setShowCreateCompany(false)}>x</button>
                </div>
                <div style={{ padding: 18, display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                  <div>
                    <label className="admin-label">Company Name *</label>
                    <input className="admin-input" value={newCompanyName} onChange={e => setNewCompanyName(e.target.value)} placeholder="e.g. Turner Construction" />
                  </div>
                  <div>
                    <label className="admin-label">Plan</label>
                    <select className="admin-input" value={newCompanyPlan} onChange={e => setNewCompanyPlan(e.target.value)}>
                      <option value="starter">Starter</option>
                      <option value="professional">Professional</option>
                      <option value="enterprise">Enterprise</option>
                    </select>
                  </div>
                  <div>
                    <label className="admin-label">Contact Name</label>
                    <input className="admin-input" value={newCompanyContact} onChange={e => setNewCompanyContact(e.target.value)} placeholder="e.g. John Vance" />
                  </div>
                  <div>
                    <label className="admin-label">Contact Email</label>
                    <input className="admin-input" value={newCompanyEmail} onChange={e => setNewCompanyEmail(e.target.value)} placeholder="e.g. jvance@turner.com" />
                  </div>
                </div>
                <div style={{ padding: '0 18px 18px', display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
                  <button className="admin-btn admin-btn-ghost" onClick={() => setShowCreateCompany(false)}>Cancel</button>
                  <button className="admin-btn admin-btn-primary" onClick={handleCreateCompany} disabled={!newCompanyName.trim() || creatingCompany}>
                    {creatingCompany ? 'Creating...' : 'Create Company'}
                  </button>
                </div>
              </div>
            )}

            {/* Company cards */}
            {companies.map(company => (
              <CompanyCard
                key={company.id}
                company={company}
                sites={sitesByCompany[company.id] || []}
                onConfigSite={(id: string, name: string) => { setConfigSiteId(id); setConfigSiteName(name); }}
                onRefresh={() => { refetchCompanies(); refetchSites(); }}
              />
            ))}
          </div>
        )}

        {/* ══════ OPERATORS TAB ══════ */}
        {activeTab === 'operators' && (() => {
          // Internal users with SOC/admin roles auto-populate as operators
          const SOC_ROLES = ['soc_operator', 'soc_supervisor', 'admin'];
          const userOperators = platformUsers.filter(u => !u.organization_id && SOC_ROLES.includes(u.role));
          const callsignFor = (u: UserPublic) => u.username.toUpperCase().slice(0, 8);
          // Standalone operator records not linked to any user account
          const linkedEmails = new Set(userOperators.map(u => u.email).filter(Boolean));
          const standaloneOps = operators.filter(op => !op.email || !linkedEmails.has(op.email));

          return (
          <div style={{ padding: 24 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 }}>
              <div>
                <div style={{ fontSize: 18, fontWeight: 700 }}>SOC Operators</div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                  {userOperators.length} from user accounts · {standaloneOps.length} standalone records
                </div>
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="admin-btn admin-btn-ghost" onClick={() => handleTabChange('users')}>Manage in Users →</button>
                <button className="admin-btn admin-btn-primary" onClick={() => setShowCreateOperator(true)}>+ Standalone Operator</button>
              </div>
            </div>

            {!showCreateOperator && opMsg && (
              <div style={{ marginBottom: 12, fontSize: 11, color: opMsg.ok ? 'var(--accent-green)' : 'var(--accent-red)' }}>{opMsg.text}</div>
            )}

            {showCreateOperator && (
              <div className="admin-card" style={{ marginBottom: 16, border: '1px solid rgba(232,115,42,0.2)' }}>
                <div className="admin-card-header" style={{ background: 'rgba(232,115,42,0.05)' }}>
                  <div className="admin-card-title">New Standalone Operator</div>
                  <button className="admin-modal-close" onClick={() => setShowCreateOperator(false)}>x</button>
                </div>
                <div style={{ padding: 18, display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12 }}>
                  <div><label className="admin-label">Full Name *</label><input className="admin-input" value={newOpName} onChange={e => setNewOpName(e.target.value)} placeholder="e.g. Marcus Chen" /></div>
                  <div><label className="admin-label">Callsign *</label><input className="admin-input" value={newOpCallsign} onChange={e => setNewOpCallsign(e.target.value)} placeholder="e.g. OP-4" /></div>
                  <div><label className="admin-label">Email</label><input className="admin-input" value={newOpEmail} onChange={e => setNewOpEmail(e.target.value)} placeholder="e.g. marcus@ironsight.ai" /></div>
                </div>
                {opMsg && showCreateOperator && <div style={{ padding: '0 18px 8px', fontSize: 11, color: opMsg.ok ? 'var(--accent-green)' : 'var(--accent-red)' }}>{opMsg.text}</div>}
                <div style={{ padding: '0 18px 18px', display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
                  <button className="admin-btn admin-btn-ghost" onClick={() => setShowCreateOperator(false)}>Cancel</button>
                  <button className="admin-btn admin-btn-primary" onClick={handleCreateOperator} disabled={!newOpName.trim() || !newOpCallsign.trim() || creatingOp}>{creatingOp ? 'Creating...' : 'Create'}</button>
                </div>
              </div>
            )}

            {/* From user accounts */}
            <div className="admin-card" style={{ marginBottom: 16 }}>
              <div className="admin-card-header">
                <div className="admin-card-title">From User Accounts</div>
                <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>auto-populated from internal staff with SOC roles</span>
              </div>
              {(usersLoading || !usersLoaded) ? (
                <div style={{ padding: 24, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>Loading…</div>
              ) : userOperators.length === 0 ? (
                <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>
                  No internal users with SOC or Admin roles — <button className="admin-btn admin-btn-ghost" style={{ fontSize: 11, padding: '2px 8px' }} onClick={() => handleTabChange('users')}>Add in Users tab</button>
                </div>
              ) : userOperators.map(u => {
                const cs = callsignFor(u);
                const isAdmin = u.role === 'admin';
                return (
                  <div key={u.id} style={{ padding: '14px 18px', borderBottom: '1px solid rgba(255,255,255,0.04)', display: 'flex', alignItems: 'center', gap: 14 }}>
                    <div style={{
                      width: 36, height: 36, borderRadius: '50%', flexShrink: 0,
                      background: 'rgba(245,158,11,0.1)', border: '2px solid rgba(245,158,11,0.3)',
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      fontSize: 12, fontWeight: 700, color: '#f59e0b', fontFamily: "'JetBrains Mono', monospace",
                    }}>
                      {cs.slice(-2)}
                    </div>
                    <div style={{ flex: 1 }}>
                      <div style={{ fontSize: 13, fontWeight: 600, display: 'flex', alignItems: 'center', gap: 6 }}>
                        {u.display_name || u.username}
                        {isAdmin && <span style={{ fontSize: 8, padding: '1px 6px', borderRadius: 4, background: 'rgba(239,68,68,0.1)', color: '#ef9b8b', border: '1px solid rgba(239,68,68,0.2)', fontWeight: 700, letterSpacing: 0.5 }}>ADMIN</span>}
                      </div>
                      <div style={{ fontSize: 10, color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>
                        {cs}{u.email ? ` · ${u.email}` : ''}
                      </div>
                    </div>
                    <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                      <span style={{ fontSize: 9, padding: '2px 8px', borderRadius: 10, fontWeight: 600, background: 'rgba(245,158,11,0.1)', color: '#f59e0b', border: '1px solid rgba(245,158,11,0.2)', letterSpacing: 0.5, textTransform: 'uppercase' }}>
                        {u.role === 'soc_supervisor' ? 'SUPERVISOR' : 'OPERATOR'}
                      </span>
                      <button className="admin-action-btn" title="Edit in Users tab" onClick={() => { handleTabChange('users'); }}>✏️</button>
                    </div>
                  </div>
                );
              })}
            </div>

            {/* Standalone operator records */}
            {standaloneOps.length > 0 && (
              <div className="admin-card">
                <div className="admin-card-header">
                  <div className="admin-card-title">Standalone Records</div>
                  <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>operators without a linked user account</span>
                </div>
                {standaloneOps.map(op => (
                  <div key={op.id} style={{ padding: '14px 18px', borderBottom: '1px solid rgba(255,255,255,0.04)', display: 'flex', alignItems: 'center', gap: 14 }}>
                    <div style={{
                      width: 36, height: 36, borderRadius: '50%',
                      background: `${statusColors[op.status] || '#4A5268'}15`,
                      border: `2px solid ${statusColors[op.status] || '#4A5268'}40`,
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      fontSize: 12, fontWeight: 700, color: statusColors[op.status] || '#4A5268',
                      fontFamily: "'JetBrains Mono', monospace",
                    }}>
                      {op.callsign.slice(-2)}
                    </div>
                    <div style={{ flex: 1 }}>
                      <div style={{ fontSize: 13, fontWeight: 600 }}>{op.name}</div>
                      <div style={{ fontSize: 10, color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>
                        {op.callsign}{op.email ? ` · ${op.email}` : ''}
                      </div>
                    </div>
                    <div style={{ padding: '3px 10px', borderRadius: 10, fontSize: 10, fontWeight: 600, background: `${statusColors[op.status] || '#4A5268'}15`, color: statusColors[op.status] || '#4A5268', border: `1px solid ${statusColors[op.status] || '#4A5268'}30`, letterSpacing: 0.5, textTransform: 'uppercase' }}>
                      {op.status}
                    </div>
                  </div>
                ))}
              </div>
            )}

            {/* Operator analytics below */}
            <div style={{ marginTop: 20 }}>
              <OperatorAnalyticsPanel />
            </div>
          </div>
          );
        })()}

        {/* ══════ USERS TAB ══════ */}
        {activeTab === 'users' && (() => {
          const PLATFORM_ROLES = [
            { value: 'admin',          label: 'Admin',          color: '#ef4444' },
            { value: 'soc_operator',   label: 'SOC Operator',   color: '#f59e0b' },
            { value: 'soc_supervisor', label: 'SOC Supervisor', color: '#f97316' },
            { value: 'site_manager',   label: 'Site Manager',   color: '#3b82f6' },
            { value: 'customer',       label: 'Customer',       color: '#8b5cf6' },
            { value: 'viewer',         label: 'Viewer',         color: '#22c55e' },
          ];
          const INTERNAL_ROLES = PLATFORM_ROLES.filter(r => ['admin', 'soc_operator', 'soc_supervisor'].includes(r.value));
          const CUSTOMER_ROLES = PLATFORM_ROLES.filter(r => ['site_manager', 'customer', 'viewer'].includes(r.value));

          const roleColor = (r: string) => PLATFORM_ROLES.find(x => x.value === r)?.color ?? '#888';
          const roleLabel = (r: string) => PLATFORM_ROLES.find(x => x.value === r)?.label ?? r;
          const isInternal = (u: UserPublic) => !u.organization_id;

          const internalUsers = platformUsers.filter(isInternal);
          const customerUsers = platformUsers.filter(u => !isInternal(u));

          return (
            <div style={{ padding: 24 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 }}>
                <div>
                  <div style={{ fontSize: 18, fontWeight: 700 }}>Platform Users</div>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                    {internalUsers.length} internal staff · {customerUsers.length} customer accounts
                  </div>
                </div>
                <button className="admin-btn admin-btn-primary" onClick={() => { setShowCreateUser(true); setUserMsg(null); }}>
                  + Add User
                </button>
              </div>

              {/* Create user form */}
              {showCreateUser && (
                <div className="admin-card" style={{ marginBottom: 16, border: '1px solid rgba(232,115,42,0.2)' }}>
                  <div className="admin-card-header" style={{ background: 'rgba(232,115,42,0.05)' }}>
                    <div className="admin-card-title">New User</div>
                    <button className="admin-modal-close" onClick={() => setShowCreateUser(false)}>x</button>
                  </div>

                  {/* User type toggle */}
                  <div style={{ padding: '14px 18px 0', display: 'flex', gap: 8 }}>
                    {([
                      { key: 'internal', label: '🛡 Internal Staff', hint: `${BRAND.name} employees and operators` },
                      { key: 'customer', label: '🏢 Customer User', hint: 'Belongs to a client company' },
                    ] as const).map(opt => (
                      <button
                        key={opt.key}
                        onClick={() => {
                          setNewUserType(opt.key);
                          setNewUserRole(opt.key === 'internal' ? 'admin' : 'viewer');
                          setNewUserOrgId('');
                        }}
                        style={{
                          flex: 1, padding: '10px 14px', borderRadius: 6, cursor: 'pointer',
                          fontFamily: 'inherit', textAlign: 'left',
                          background: newUserType === opt.key ? 'rgba(232,115,42,0.08)' : 'rgba(255,255,255,0.02)',
                          border: `1px solid ${newUserType === opt.key ? 'rgba(232,115,42,0.35)' : 'rgba(255,255,255,0.07)'}`,
                        }}
                      >
                        <div style={{ fontSize: 12, fontWeight: 600, color: newUserType === opt.key ? 'var(--accent-orange)' : 'var(--text-secondary)' }}>
                          {opt.label}
                        </div>
                        <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>{opt.hint}</div>
                      </button>
                    ))}
                  </div>

                  <div style={{ padding: 18, display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12 }}>
                    <div>
                      <label className="admin-label">Username *</label>
                      <input className="admin-input" value={newUsername} onChange={e => setNewUsername(e.target.value)} placeholder="e.g. jsmith" />
                    </div>
                    <div>
                      <label className="admin-label">Password *</label>
                      <input className="admin-input" type="password" value={newPassword} onChange={e => setNewPassword(e.target.value)} placeholder="Min 8 characters" />
                    </div>
                    {newUserType === 'internal' ? (
                      <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'flex-end' }}>
                        <label className="admin-label">Role</label>
                        <div style={{
                          padding: '8px 12px', borderRadius: 5, border: '1px solid rgba(255,255,255,0.08)',
                          background: 'rgba(255,255,255,0.02)', display: 'flex', flexDirection: 'column', gap: 8,
                        }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                            <span style={{ fontSize: 10, fontWeight: 700, padding: '2px 8px', borderRadius: 10, background: 'rgba(245,158,11,0.12)', color: '#f59e0b', border: '1px solid rgba(245,158,11,0.25)' }}>SOC Operator</span>
                            <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>default</span>
                          </div>
                          <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', userSelect: 'none' }}>
                            <input
                              type="checkbox"
                              checked={newUserIsAdmin}
                              onChange={e => setNewUserIsAdmin(e.target.checked)}
                              style={{ accentColor: '#ef4444', width: 14, height: 14 }}
                            />
                            <span style={{ fontSize: 10, color: newUserIsAdmin ? '#ef4444' : 'var(--text-secondary)', fontWeight: newUserIsAdmin ? 600 : 400 }}>
                              Admin Access
                            </span>
                          </label>
                        </div>
                      </div>
                    ) : (
                      <div>
                        <label className="admin-label">Role</label>
                        <select
                          className="admin-input"
                          value={newUserRole}
                          onChange={e => setNewUserRole(e.target.value)}
                          style={{ color: roleColor(newUserRole) }}
                        >
                          {CUSTOMER_ROLES.map(r => (
                            <option key={r.value} value={r.value}>{r.label}</option>
                          ))}
                        </select>
                      </div>
                    )}
                    <div>
                      <label className="admin-label">Display Name</label>
                      <input className="admin-input" value={newDisplayName} onChange={e => setNewDisplayName(e.target.value)} placeholder="e.g. John Smith" />
                    </div>
                    <div>
                      <label className="admin-label">Email</label>
                      <input className="admin-input" value={newEmail} onChange={e => setNewEmail(e.target.value)} placeholder={newUserType === 'internal' ? 'e.g. jsmith@jetstreamsys.com' : 'e.g. jsmith@customer.com'} />
                    </div>
                    {newUserType === 'customer' ? (
                      <div>
                        <label className="admin-label">Company *</label>
                        <select className="admin-input" value={newUserOrgId} onChange={e => setNewUserOrgId(e.target.value)}>
                          <option value="">Select company…</option>
                          {companies.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
                        </select>
                      </div>
                    ) : (
                      <div>
                        <label className="admin-label">Phone</label>
                        <input className="admin-input" value={newPhone} onChange={e => setNewPhone(e.target.value)} placeholder="e.g. 555-0100" />
                      </div>
                    )}
                  </div>

                  {newUserType === 'internal' && (
                    <div style={{ margin: '0 18px 12px', padding: '8px 12px', borderRadius: 5, background: 'rgba(239,68,68,0.05)', border: '1px solid rgba(239,68,68,0.15)', fontSize: 10, color: '#ef9b8b' }}>
                      Admin and SOC Operator roles give full platform access. Assign only to {BRAND.name} staff.
                    </div>
                  )}

                  {userMsg && (
                    <div style={{ padding: '0 18px 10px', fontSize: 11, color: userMsg.ok ? 'var(--accent-green)' : 'var(--accent-red)' }}>
                      {userMsg.text}
                    </div>
                  )}
                  <div style={{ padding: '0 18px 18px', display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
                    <button className="admin-btn admin-btn-ghost" onClick={() => setShowCreateUser(false)}>Cancel</button>
                    <button
                      className="admin-btn admin-btn-primary"
                      onClick={handleCreateUser}
                      disabled={!newUsername.trim() || !newPassword.trim() || (newUserType === 'customer' && !newUserOrgId) || creatingUser}
                    >
                      {creatingUser ? 'Creating...' : 'Create User'}
                    </button>
                  </div>
                </div>
              )}

              {/* Feedback outside form */}
              {!showCreateUser && userMsg && (
                <div style={{ marginBottom: 12, fontSize: 11, color: userMsg.ok ? 'var(--accent-green)' : 'var(--accent-red)' }}>
                  {userMsg.text}
                </div>
              )}

              {/* Password change inline form */}
              {pwdTargetId && (
                <div className="admin-card" style={{ marginBottom: 16, border: '1px solid rgba(59,130,246,0.2)' }}>
                  <div className="admin-card-header" style={{ background: 'rgba(59,130,246,0.04)' }}>
                    <div className="admin-card-title">
                      Change password for <strong>{platformUsers.find(u => u.id === pwdTargetId)?.username}</strong>
                    </div>
                    <button className="admin-modal-close" onClick={() => { setPwdTargetId(null); setPwdMsg(null); }}>x</button>
                  </div>
                  <div style={{ padding: '12px 18px', display: 'flex', gap: 8, alignItems: 'center' }}>
                    <input
                      className="admin-input" type="password"
                      placeholder="New password" value={pwdValue}
                      onChange={e => setPwdValue(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && handleChangePassword()}
                      autoFocus style={{ flex: 1 }}
                    />
                    <button className="admin-btn admin-btn-primary" onClick={handleChangePassword} disabled={!pwdValue.trim()}>
                      Update
                    </button>
                  </div>
                  {pwdMsg && (
                    <div style={{ padding: '0 18px 10px', fontSize: 11, color: pwdMsg.ok ? 'var(--accent-green)' : 'var(--accent-red)' }}>
                      {pwdMsg.text}
                    </div>
                  )}
                </div>
              )}

              {/* User rows helper */}
              {(() => {
                const UserRow = ({ u, roleSet }: { u: UserPublic; roleSet: typeof PLATFORM_ROLES }) => {
                  const isEditing = editingUserId === u.id;
                  return (
                  <>
                  <div style={{
                    padding: '12px 18px', borderBottom: isEditing ? 'none' : '1px solid rgba(255,255,255,0.04)',
                    display: 'flex', alignItems: 'center', gap: 14,
                  }}>
                    {/* Avatar */}
                    <div style={{
                      width: 36, height: 36, borderRadius: '50%', flexShrink: 0,
                      background: `${roleColor(u.role)}15`,
                      border: `2px solid ${roleColor(u.role)}35`,
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      fontSize: 12, fontWeight: 700, color: roleColor(u.role),
                      fontFamily: "'JetBrains Mono', monospace",
                    }}>
                      {(u.display_name || u.username).slice(0, 2).toUpperCase()}
                    </div>

                    {/* Identity */}
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontSize: 13, fontWeight: 600, display: 'flex', alignItems: 'center', gap: 6 }}>
                        {u.display_name || u.username}
                        {u.id === user?.id && (
                          <span style={{ fontSize: 9, padding: '1px 5px', borderRadius: 4, background: 'rgba(232,115,42,0.15)', color: 'var(--accent-orange)', fontWeight: 700 }}>YOU</span>
                        )}
                        {isInternal(u) && (
                          <span style={{ fontSize: 8, padding: '1px 6px', borderRadius: 4, background: 'rgba(239,68,68,0.1)', color: '#ef9b8b', fontWeight: 700, border: '1px solid rgba(239,68,68,0.2)', letterSpacing: 0.5 }}>STAFF</span>
                        )}
                      </div>
                      <div style={{ fontSize: 10, color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>
                        {u.username}{u.email ? ` · ${u.email}` : ''}{u.phone ? ` · ${u.phone}` : ''}
                        {u.organization_id && (
                          <span style={{ marginLeft: 4, color: '#8b5cf6' }}>· {companies.find(c => c.id === u.organization_id)?.name ?? u.organization_id}</span>
                        )}
                      </div>
                    </div>

                    {/* Role */}
                    <div style={{ flexShrink: 0 }}>
                      {user?.role === 'admin' && u.id !== user?.id ? (
                        <select
                          className="admin-input"
                          value={u.role}
                          onChange={e => handleRoleChange(u.id, e.target.value)}
                          style={{ fontSize: 10, padding: '3px 8px', color: roleColor(u.role) }}
                        >
                          {roleSet.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}
                        </select>
                      ) : (
                        <span style={{
                          fontSize: 9, padding: '2px 8px', borderRadius: 10, fontWeight: 600,
                          background: `${roleColor(u.role)}12`,
                          color: roleColor(u.role),
                          border: `1px solid ${roleColor(u.role)}25`,
                          letterSpacing: 0.5, textTransform: 'uppercase', whiteSpace: 'nowrap',
                        }}>
                          {roleLabel(u.role)}
                        </span>
                      )}
                    </div>

                    {/* Created */}
                    <div style={{ fontSize: 10, color: 'var(--text-muted)', flexShrink: 0, minWidth: 70, textAlign: 'right' }}>
                      {new Date(u.created_at).toLocaleDateString()}
                    </div>

                    {/* Actions */}
                    <div style={{ display: 'flex', gap: 4, flexShrink: 0 }}>
                      <button
                        className="admin-action-btn"
                        onClick={() => isEditing ? setEditingUserId(null) : openEditProfile(u)}
                        title="Edit profile"
                        style={isEditing ? { color: 'var(--accent-orange)' } : {}}
                      >✏️</button>
                      <button
                        className="admin-action-btn"
                        onClick={() => { setPwdTargetId(u.id); setPwdValue(''); setPwdMsg(null); }}
                        title="Change password"
                      >🔑</button>
                      {user?.role === 'admin' && u.id !== user?.id && (
                        <button
                          className="admin-action-btn"
                          onClick={() => handleDeleteUser(u.id, u.username)}
                          title="Delete user"
                          style={{ color: 'var(--accent-red)' }}
                        >🗑</button>
                      )}
                    </div>
                  </div>

                  {/* Inline edit profile form */}
                  {isEditing && (
                    <div style={{ padding: '12px 18px 16px', borderBottom: '1px solid rgba(255,255,255,0.04)', background: 'rgba(232,115,42,0.03)' }}>
                      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 10, marginBottom: 10 }}>
                        <div>
                          <label className="admin-label">Display Name</label>
                          <input className="admin-input" value={editDisplayName} onChange={e => setEditDisplayName(e.target.value)} placeholder={u.username} />
                        </div>
                        <div>
                          <label className="admin-label">Email</label>
                          <input className="admin-input" value={editEmail} onChange={e => setEditEmail(e.target.value)} placeholder="email@example.com" />
                        </div>
                        <div>
                          <label className="admin-label">Phone</label>
                          <input className="admin-input" value={editPhone} onChange={e => setEditPhone(e.target.value)} placeholder="555-0100" />
                        </div>
                      </div>
                      {editMsg && (
                        <div style={{ marginBottom: 8, fontSize: 11, color: editMsg.ok ? 'var(--accent-green)' : 'var(--accent-red)' }}>
                          {editMsg.text}
                        </div>
                      )}
                      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                        <button className="admin-btn admin-btn-ghost" style={{ fontSize: 11 }} onClick={() => setEditingUserId(null)}>Cancel</button>
                        <button className="admin-btn admin-btn-primary" style={{ fontSize: 11 }} onClick={handleSaveProfile} disabled={savingProfile}>
                          {savingProfile ? 'Saving…' : 'Save Profile'}
                        </button>
                      </div>
                    </div>
                  )}
                  </>
                  );
                };

                return (
                  <>
                    {/* ── Internal Staff ── */}
                    <div className="admin-card" style={{ marginBottom: 16 }}>
                      <div className="admin-card-header">
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <div className="admin-card-title">{BRAND.name} Staff</div>
                          <span style={{ fontSize: 8, padding: '1px 6px', borderRadius: 4, background: 'rgba(239,68,68,0.1)', color: '#ef9b8b', fontWeight: 700, border: '1px solid rgba(239,68,68,0.2)', letterSpacing: 0.5 }}>INTERNAL</span>
                        </div>
                        <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>
                          {internalUsers.length} account{internalUsers.length !== 1 ? 's' : ''} · admin & operator access
                        </span>
                      </div>
                      {usersLoading ? (
                        <div style={{ padding: 16 }}>
                          <SkeletonRows count={4} height={32} gap={6} />
                        </div>
                      ) : internalUsers.length === 0 ? (
                        <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>No internal staff accounts</div>
                      ) : (
                        internalUsers.map(u => <UserRow key={u.id} u={u} roleSet={INTERNAL_ROLES} />)
                      )}
                    </div>

                    {/* ── Customer Accounts ── */}
                    <div className="admin-card">
                      <div className="admin-card-header">
                        <div className="admin-card-title">Customer Accounts</div>
                        <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>
                          {customerUsers.length} account{customerUsers.length !== 1 ? 's' : ''} · portal access only
                        </span>
                      </div>
                      {usersLoading ? (
                        <div style={{ padding: 24, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>Loading...</div>
                      ) : customerUsers.length === 0 ? (
                        <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>No customer accounts</div>
                      ) : (
                        customerUsers.map(u => <UserRow key={u.id} u={u} roleSet={CUSTOMER_ROLES} />)
                      )}
                    </div>
                  </>
                );
              })()}
            </div>
          );
        })()}

        {/* ══════ NVR SETTINGS TAB ══════ */}
        {activeTab === 'settings' && (
          <ToastProvider>
            <SettingsPage
              currentUserId={user?.id ?? ''}
              currentUserRole={user?.role ?? 'viewer'}
              cameras={cameras}
              onRefresh={ensureCameras}
            />
          </ToastProvider>
        )}

        {/* ══════ HEALTH TAB ══════ */}
        {activeTab === 'health' && (
          <div style={{ padding: '24px 24px 40px' }}>
            <div style={{ fontSize: 16, fontWeight: 700, marginBottom: 4 }}>System Health</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 20 }}>
              Camera connectivity, recording status, and server metrics
            </div>
            <div style={{ marginBottom: 24 }}>
              <RecordingHealthCard />
            </div>
            <HealthDashboard cameras={cameras} />
          </div>
        )}

        {/* ══════ AUDIT TAB ══════ */}
        {activeTab === 'audit' && (
          <div style={{ padding: 24 }}>
            <AuditLogPanel />
            <div style={{ marginTop: 16 }}>
              <button onClick={() => setShowAuditExport(true)} className="admin-btn admin-btn-primary">Export Audit Trail</button>
            </div>
          </div>
        )}

        {/* ══════ INTEGRATIONS TAB ══════ */}
        {activeTab === 'integrations' && (
          <div style={{ padding: 24 }}>
            <IntegrationHub />
          </div>
        )}
      </div>

      {/* ── Modals ── */}
      {showCreateSiteModal && <CreateSiteModal onClose={() => { closeModals(); refetchSites(); }} editSiteId={editingSiteId} />}
      {configSiteId && <SiteConfigModal siteId={configSiteId} siteName={configSiteName} onClose={() => { setConfigSiteId(null); refetchSites(); refetchCompanies(); }} onDeleted={() => { refetchSites(); refetchCompanies(); }} />}
      {showAuditExport && <AuditLogExport onClose={() => setShowAuditExport(false)} />}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// Sites & Customers Tab — searchable, filterable, paginated
// ═══════════════════════════════════════════════════════════════

const SITES_PAGE_SIZE = 25;

function SitesAndCustomersTab({ sites, companies, sitesByCompany, onCreateSite, onConfigSite, onRefresh,
  showCreateCompany, setShowCreateCompany, newCompanyName, setNewCompanyName, newCompanyPlan, setNewCompanyPlan,
  newCompanyContact, setNewCompanyContact, newCompanyEmail, setNewCompanyEmail, creatingCompany, handleCreateCompany,
}: {
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
}) {
  const [search, setSearch] = useState('');
  const [companyFilter, setCompanyFilter] = useState<string>('all');
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [viewMode, setViewMode] = useState<'sites' | 'companies'>('sites');
  const [page, setPage] = useState(0);

  // Company lookup
  const companyMap = useMemo(() => {
    const m: Record<string, Company> = {};
    for (const c of companies) m[c.id] = c;
    return m;
  }, [companies]);

  // Filtered & searched sites
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
