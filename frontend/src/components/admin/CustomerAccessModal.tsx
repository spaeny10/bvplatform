'use client';

import { useState, useEffect, useMemo } from 'react';
import { useCompanyUsers } from '@/hooks/useCustomers';
import { useSites } from '@/hooks/useSites';
import { getSiteUsers, assignUserToSite, unassignUserFromSite } from '@/lib/ironsight-api';
import type { SiteUserAssignment } from '@/types/ironsight';

interface Props {
  siteId: string;
  onClose: () => void;
  embedded?: boolean;
}

/** Role color map */
const ROLE_COLORS: Record<string, { bg: string; border: string; text: string }> = {
  admin: { bg: 'rgba(139,92,246,0.15)', border: 'rgba(139,92,246,0.3)', text: '#a78bfa' },
  safety_manager: { bg: 'rgba(0,212,255,0.1)', border: 'rgba(0,212,255,0.25)', text: '#E8732A' },
  supervisor: { bg: 'rgba(0,229,160,0.1)', border: 'rgba(0,229,160,0.25)', text: '#22C55E' },
  viewer: { bg: 'rgba(255,255,255,0.04)', border: 'rgba(255,255,255,0.08)', text: '#8891A5' },
};

export default function UserAssignmentModal({ siteId, onClose, embedded }: Props) {
  const { data: sites = [] } = useSites();
  const site = sites.find(s => s.id === siteId);
  const companyId = site?.company_id || null;
  const { data: companyUsers = [] } = useCompanyUsers(companyId);

  const [assignments, setAssignments] = useState<SiteUserAssignment[]>([]);
  const [loading, setLoading] = useState(true);

  // Load current site user assignments
  useEffect(() => {
    getSiteUsers(siteId).then(a => {
      setAssignments(a);
      setLoading(false);
    });
  }, [siteId]);

  const assignedUserIds = useMemo(() => new Set(assignments.map(a => a.user_id)), [assignments]);

  const handleToggle = async (userId: string) => {
    if (assignedUserIds.has(userId)) {
      await unassignUserFromSite(siteId, userId);
      setAssignments(prev => prev.filter(a => a.user_id !== userId));
    } else {
      const result = await assignUserToSite(siteId, userId);
      setAssignments(prev => [...prev, result]);
    }
  };

  const bodyContent = (
    <>
      {loading && (
        <div style={{ padding: 40, textAlign: 'center', color: '#4A5268' }}>Loading users…</div>
      )}

      {!loading && !companyId && (
        <div style={{ padding: 40, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
          This site has no company assigned. Assign a company first.
        </div>
      )}

      {!loading && companyId && (
        <>
          {/* Currently assigned */}
          {assignments.length > 0 && (
            <>
              <div style={{ padding: '12px 16px 6px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#4A5268' }}>
                Assigned ({assignments.length})
              </div>
              {assignments.map(a => {
                const rc = ROLE_COLORS[a.role] || ROLE_COLORS.viewer;
                return (
                  <div key={a.user_id} className="admin-camera-item">
                    <input
                      type="checkbox"
                      checked
                      onChange={() => handleToggle(a.user_id)}
                    />
                    <div className="admin-camera-info">
                      <div className="admin-camera-name">{a.user_name}</div>
                      <div className="admin-camera-meta">{a.user_email}</div>
                    </div>
                    <span style={{
                      fontSize: 9, padding: '2px 7px', borderRadius: 2,
                      fontWeight: 600, letterSpacing: 0.5, textTransform: 'uppercase' as const,
                      background: rc.bg, border: `1px solid ${rc.border}`, color: rc.text,
                    }}>
                      {a.role.replace('_', ' ')}
                    </span>
                  </div>
                );
              })}
            </>
          )}

          {/* Available company users */}
          {companyUsers.filter(u => !assignedUserIds.has(u.id)).length > 0 && (
            <>
              <div style={{ padding: '12px 16px 6px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#4A5268' }}>
                Available Company Users ({companyUsers.filter(u => !assignedUserIds.has(u.id)).length})
              </div>
              {companyUsers.filter(u => !assignedUserIds.has(u.id)).map(user => {
                const rc = ROLE_COLORS[user.role] || ROLE_COLORS.viewer;
                return (
                  <div key={user.id} className="admin-camera-item">
                    <input
                      type="checkbox"
                      checked={false}
                      onChange={() => handleToggle(user.id)}
                    />
                    <div className="admin-camera-info">
                      <div className="admin-camera-name">{user.name}</div>
                      <div className="admin-camera-meta">{user.email}</div>
                    </div>
                    <span style={{
                      fontSize: 9, padding: '2px 7px', borderRadius: 2,
                      fontWeight: 600, letterSpacing: 0.5, textTransform: 'uppercase' as const,
                      background: rc.bg, border: `1px solid ${rc.border}`, color: rc.text,
                    }}>
                      {user.role.replace('_', ' ')}
                    </span>
                  </div>
                );
              })}
            </>
          )}

          {companyUsers.length === 0 && (
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
              {site?.name || siteId} · {assignments.length} user{assignments.length !== 1 ? 's' : ''} assigned
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
