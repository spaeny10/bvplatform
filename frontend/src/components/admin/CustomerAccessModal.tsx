'use client';

import { useState, useEffect, useMemo, useCallback } from 'react';
import { useSites } from '@/hooks/useSites';
import { listUsers, updateUserProfile, type UserPublic } from '@/lib/api';

interface Props {
  siteId: string;
  onClose: () => void;
  embedded?: boolean;
}

/** Role color map — platform roles from the unified users table. */
const ROLE_COLORS: Record<string, { bg: string; border: string; text: string }> = {
  admin: { bg: 'rgba(139,92,246,0.15)', border: 'rgba(139,92,246,0.3)', text: '#a78bfa' },
  soc_operator: { bg: 'rgba(0,212,255,0.1)', border: 'rgba(0,212,255,0.25)', text: '#E8732A' },
  soc_supervisor: { bg: 'rgba(0,212,255,0.1)', border: 'rgba(0,212,255,0.25)', text: '#E8732A' },
  site_manager: { bg: 'rgba(0,229,160,0.1)', border: 'rgba(0,229,160,0.25)', text: '#22C55E' },
  customer: { bg: 'rgba(255,255,255,0.04)', border: 'rgba(255,255,255,0.08)', text: '#8891A5' },
  viewer: { bg: 'rgba(255,255,255,0.04)', border: 'rgba(255,255,255,0.08)', text: '#8891A5' },
};

// F-04: site-user assignment used to call GET/POST/DELETE
// /api/v1/sites/{id}/users — routes that never existed server-side, so
// the tab hung at "Loading users…" forever and every toggle silently
// failed. The real access-scoping mechanism is users.assigned_site_ids
// (what AuthorizedCameraIDs / canAccessSiteByID actually read), edited
// via PATCH /api/users/{id}. This modal now lists platform users for
// the site's company and toggles the site in/out of each user's
// assigned_site_ids.
export default function UserAssignmentModal({ siteId, onClose, embedded }: Props) {
  const { data: sites = [] } = useSites();
  const site = sites.find(s => s.id === siteId);
  const companyId = site?.company_id || null;

  const [users, setUsers] = useState<UserPublic[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [savingUserId, setSavingUserId] = useState<string | null>(null);

  // Load all platform users once; filtering to the site's company
  // happens client-side because GET /api/users has no org filter.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    listUsers()
      .then(u => {
        if (cancelled) return;
        setUsers(u);
        setLoading(false);
      })
      .catch(e => {
        if (cancelled) return;
        setLoadError(e instanceof Error ? e.message : String(e));
        setLoading(false);
      });
    return () => { cancelled = true; };
  }, [siteId]);

  // Users relevant to this site: everyone in the owning company, plus
  // anyone already assigned to the site (so a stale cross-org
  // assignment can still be removed here).
  const relevantUsers = useMemo(
    () => users.filter(u =>
      (companyId && u.organization_id === companyId) ||
      (u.assigned_site_ids ?? []).includes(siteId),
    ),
    [users, companyId, siteId],
  );
  const assignedUsers = relevantUsers.filter(u => (u.assigned_site_ids ?? []).includes(siteId));
  const availableUsers = relevantUsers.filter(u => !(u.assigned_site_ids ?? []).includes(siteId));

  const handleToggle = useCallback(async (user: UserPublic) => {
    const current = user.assigned_site_ids ?? [];
    const next = current.includes(siteId)
      ? current.filter(id => id !== siteId)
      : [...current, siteId];
    setSavingUserId(user.id);
    setSaveError(null);
    try {
      // organization_id must be echoed back: the backend PATCH writes
      // it unconditionally (no COALESCE), so omitting it would null
      // the user's org membership.
      const updated = await updateUserProfile(user.id, {
        organization_id: user.organization_id ?? '',
        assigned_site_ids: next,
      });
      setUsers(prev => prev.map(u => (u.id === updated.id ? updated : u)));
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : String(e));
    } finally {
      setSavingUserId(null);
    }
  }, [siteId]);

  const renderUserRow = (user: UserPublic, assigned: boolean) => {
    const rc = ROLE_COLORS[user.role] || ROLE_COLORS.viewer;
    return (
      <div key={user.id} className="admin-camera-item">
        <input
          type="checkbox"
          checked={assigned}
          disabled={savingUserId !== null}
          onChange={() => handleToggle(user)}
        />
        <div className="admin-camera-info">
          <div className="admin-camera-name">{user.display_name || user.username}</div>
          <div className="admin-camera-meta">{user.email || user.username}</div>
        </div>
        <span style={{
          fontSize: 9, padding: '2px 7px', borderRadius: 2,
          fontWeight: 600, letterSpacing: 0.5, textTransform: 'uppercase' as const,
          background: rc.bg, border: `1px solid ${rc.border}`, color: rc.text,
        }}>
          {user.role.replace(/_/g, ' ')}
        </span>
      </div>
    );
  };

  const bodyContent = (
    <>
      {loading && (
        <div style={{ padding: 40, textAlign: 'center', color: '#4A5268' }}>Loading users…</div>
      )}

      {!loading && loadError && (
        <div style={{ padding: 40, textAlign: 'center', color: '#e87060', fontSize: 12 }}>
          Failed to load users: {loadError}
        </div>
      )}

      {!loading && !loadError && !companyId && assignedUsers.length === 0 && (
        <div style={{ padding: 40, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
          This site has no company assigned. Assign a company first.
        </div>
      )}

      {!loading && !loadError && (companyId || assignedUsers.length > 0) && (
        <>
          {saveError && (
            <div style={{
              margin: '10px 16px 0', padding: '8px 12px', borderRadius: 4, fontSize: 11,
              background: 'rgba(192,49,26,0.08)', border: '1px solid rgba(192,49,26,0.25)', color: '#e87060',
            }}>
              Failed to update assignment: {saveError}
            </div>
          )}

          {/* Currently assigned */}
          {assignedUsers.length > 0 && (
            <>
              <div style={{ padding: '12px 16px 6px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#4A5268' }}>
                Assigned ({assignedUsers.length})
              </div>
              {assignedUsers.map(u => renderUserRow(u, true))}
            </>
          )}

          {/* Available company users */}
          {availableUsers.length > 0 && (
            <>
              <div style={{ padding: '12px 16px 6px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#4A5268' }}>
                Available Company Users ({availableUsers.length})
              </div>
              {availableUsers.map(u => renderUserRow(u, false))}
            </>
          )}

          {relevantUsers.length === 0 && (
            <div style={{ padding: 40, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
              No users found for this company. Add users to the company first.
            </div>
          )}
        </>
      )}
    </>
  );

  if (embedded) return bodyContent;

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal wide" onClick={e => e.stopPropagation()}>
        <div className="admin-modal-header">
          <div>
            <div className="admin-modal-title">Site Users</div>
            <div style={{ fontSize: 11, color: '#4A5268', marginTop: 2 }}>
              {site?.name || siteId} · {assignedUsers.length} user{assignedUsers.length !== 1 ? 's' : ''} assigned
            </div>
          </div>
          <button className="admin-modal-close" onClick={onClose}>✕</button>
        </div>

        <div className="admin-modal-body" style={{ padding: 0 }}>
          {bodyContent}
        </div>

        <div className="admin-modal-footer">
          <button className="admin-btn admin-btn-ghost" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  );
}
