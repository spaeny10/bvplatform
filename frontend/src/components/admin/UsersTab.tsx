'use client';

// Extracted from app/admin/page.tsx (P1-B-11 session 14). The Users tab
// is large (~400 lines of JSX) and owned a deep state machine —
// 21 useState slots and 6 handlers, none of which the rest of the admin
// page touched. The state moves into this child via the session-7 pattern;
// only `platformUsers` + `setPlatformUsers` + `usersLoading` + `companies`
// + the current `user` cross the boundary as props (because the operators
// tab in the same parent also reads platformUsers).

import { useCallback, useState } from 'react';
import type { Company } from '@/types/ironsight';
import {
    UserPublic,
    createUser, deleteUser, updateUserPassword, updateUserRole, updateUserProfile,
} from '@/lib/api';
import { apiFetch } from '@/lib/admin-api-helpers';
import { BRAND } from '@/lib/branding';
import { SkeletonRows } from '@/components/shared/Skeleton';

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

interface Props {
    user: { id?: string; role?: string } | null;
    companies: Company[];
    platformUsers: UserPublic[];
    setPlatformUsers: React.Dispatch<React.SetStateAction<UserPublic[]>>;
    usersLoading: boolean;
    loadPlatformUsers: () => Promise<void>;
}

export default function UsersTab({
    user, companies, platformUsers, setPlatformUsers, usersLoading, loadPlatformUsers,
}: Props) {
    // ── Create-user form ──
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
    const [userMsg, setUserMsg] = useState<{ ok: boolean; text: string } | null>(null);

    // ── Edit profile inline form ──
    const [editingUserId, setEditingUserId] = useState<string | null>(null);
    const [editDisplayName, setEditDisplayName] = useState('');
    const [editEmail, setEditEmail] = useState('');
    const [editPhone, setEditPhone] = useState('');
    const [editOrgId, setEditOrgId] = useState('');
    const [savingProfile, setSavingProfile] = useState(false);
    const [editMsg, setEditMsg] = useState<{ ok: boolean; text: string } | null>(null);

    // ── Password change inline form ──
    const [pwdTargetId, setPwdTargetId] = useState<string | null>(null);
    const [pwdValue, setPwdValue] = useState('');
    const [pwdMsg, setPwdMsg] = useState<{ ok: boolean; text: string } | null>(null);

    // ── Customer company filter ──
    const [customerCompanyFilter, setCustomerCompanyFilter] = useState('');

    const internalUsers = platformUsers.filter(isInternal);
    const customerUsers = platformUsers.filter(u => !isInternal(u));

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
        } catch (e) {
            setUserMsg({ ok: false, text: e instanceof Error ? e.message : 'Failed to create user.' });
        }
        setCreatingUser(false);
    }, [newUsername, newPassword, newDisplayName, newEmail, newPhone, newUserType, newUserRole, newUserIsAdmin, newUserOrgId, loadPlatformUsers]);

    const handleDeleteUser = useCallback(async (id: string, username: string) => {
        if (!confirm(`Delete user "${username}"? This cannot be undone.`)) return;
        try {
            await deleteUser(id);
            setPlatformUsers(prev => prev.filter(u => u.id !== id));
        } catch (e) { alert('Delete failed: ' + (e instanceof Error ? e.message : String(e))); }
    }, [setPlatformUsers]);

    const handleRoleChange = useCallback(async (id: string, role: string) => {
        try {
            await updateUserRole(id, role);
            setPlatformUsers(prev => prev.map(u => u.id === id ? { ...u, role } : u));
        } catch (e) { alert('Role change failed: ' + (e instanceof Error ? e.message : String(e))); }
    }, [setPlatformUsers]);

    const handleChangePassword = useCallback(async () => {
        if (!pwdTargetId || !pwdValue.trim()) return;
        setPwdMsg(null);
        try {
            await updateUserPassword(pwdTargetId, pwdValue.trim());
            setPwdTargetId(null);
            setPwdValue('');
            setPwdMsg({ ok: true, text: 'Password updated.' });
        } catch (e) {
            setPwdMsg({ ok: false, text: e instanceof Error ? e.message : 'Failed.' });
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
        } catch (e) {
            setEditMsg({ ok: false, text: e instanceof Error ? e.message : 'Failed to save.' });
        }
        setSavingProfile(false);
    }, [editingUserId, editDisplayName, editEmail, editPhone, editOrgId, setPlatformUsers]);

    const UserRow = ({ u, roleSet }: { u: UserPublic; roleSet: typeof PLATFORM_ROLES }) => {
        const isEditing = editingUserId === u.id;
        return (
            <>
                <div style={{
                    padding: '12px 18px', borderBottom: isEditing ? 'none' : '1px solid rgba(255,255,255,0.04)',
                    display: 'flex', alignItems: 'center', gap: 14,
                }}>
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

                    <div style={{ fontSize: 10, color: 'var(--text-muted)', flexShrink: 0, minWidth: 70, textAlign: 'right' }}>
                        {new Date(u.created_at).toLocaleDateString()}
                    </div>

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
                                onClick={async () => {
                                    if (!confirm(`Reset MFA for ${u.username}? Their authenticator app will be unlinked and they can re-enroll on next login.`)) return;
                                    try {
                                        await apiFetch(`/api/v1/users/${u.id}/mfa/reset`, { method: 'POST' });
                                        setUserMsg({ ok: true, text: `MFA reset for ${u.username}.` });
                                    } catch (e) {
                                        setUserMsg({ ok: false, text: e instanceof Error ? e.message : 'MFA reset failed.' });
                                    }
                                }}
                                title="Reset MFA"
                                style={{ color: '#f59e0b' }}
                            >🔐</button>
                        )}
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

            {showCreateUser && (
                <div className="admin-card" style={{ marginBottom: 16, border: '1px solid rgba(232,115,42,0.2)' }}>
                    <div className="admin-card-header" style={{ background: 'rgba(232,115,42,0.05)' }}>
                        <div className="admin-card-title">New User</div>
                        <button className="admin-modal-close" onClick={() => setShowCreateUser(false)}>x</button>
                    </div>

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

            {!showCreateUser && userMsg && (
                <div style={{ marginBottom: 12, fontSize: 11, color: userMsg.ok ? 'var(--accent-green)' : 'var(--accent-red)' }}>
                    {userMsg.text}
                </div>
            )}

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
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                        <select
                            className="admin-input"
                            value={customerCompanyFilter}
                            onChange={e => setCustomerCompanyFilter(e.target.value)}
                            style={{ fontSize: 10, padding: '3px 8px', minWidth: 130 }}
                        >
                            <option value="">All companies</option>
                            {companies.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
                        </select>
                        <span style={{ fontSize: 10, color: 'var(--text-muted)', flexShrink: 0 }}>
                            {customerUsers.filter(u => !customerCompanyFilter || u.organization_id === customerCompanyFilter).length} account{customerUsers.filter(u => !customerCompanyFilter || u.organization_id === customerCompanyFilter).length !== 1 ? 's' : ''} · portal access only
                        </span>
                    </div>
                </div>
                {usersLoading ? (
                    <div style={{ padding: 24, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>Loading...</div>
                ) : customerUsers.length === 0 ? (
                    <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>No customer accounts</div>
                ) : (
                    customerUsers
                        .filter(u => !customerCompanyFilter || u.organization_id === customerCompanyFilter)
                        .map(u => <UserRow key={u.id} u={u} roleSet={CUSTOMER_ROLES} />)
                )}
            </div>
        </div>
    );
}
