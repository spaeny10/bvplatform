'use client';

import { useEffect, useState } from 'react';
import {
  listIncidentShares,
  revokeEvidenceShareLink,
  createEvidenceShareLink,
  type EvidenceShareWithStats,
  type EvidenceShareRequest,
} from '@/lib/ironsight-api';

// Per-incident evidence-share manager. The supervisor pastes (or
// pre-fills via deep link) an incident id; the card shows every
// share token created against it — active, revoked, or expired —
// with the open-count denormalized in. Revoke button kills a token
// instantly; the public /share/{token} endpoint will 404 within
// the next request.
//
// Why per-incident vs global: shares are scoped to incidents (one
// per investigation), so a global list would conflate dozens of
// active investigations. A supervisor walks in with "what shares
// went out for INC-2026-0042" and gets a focused answer.

function fmtAbsolute(iso: string | null): string {
  if (!iso) return 'no expiry';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

function fmtRelative(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const diff = Date.now() - d.getTime();
  const min = Math.floor(diff / 60000);
  if (min < 1)  return 'just now';
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24)  return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

function statusBadge(s: EvidenceShareWithStats): { label: string; color: string } {
  if (s.revoked)         return { label: 'REVOKED', color: '#9CA3AF' };
  if (!s.active)         return { label: 'EXPIRED', color: '#E89F2A' };
  return { label: 'ACTIVE', color: '#84CC16' };
}

export default function EvidenceSharesCard() {
  const [incidentId, setIncidentId] = useState('');
  const [submittedId, setSubmittedId] = useState('');
  const [shares, setShares] = useState<EvidenceShareWithStats[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [createExpiry, setCreateExpiry] = useState<EvidenceShareRequest['expires_in']>('1w');
  const [creating, setCreating] = useState(false);
  const [newShare, setNewShare] = useState<{ url: string; expires_at: string | null } | null>(null);

  const refresh = async (id: string) => {
    if (!id) return;
    setLoading(true);
    setErr('');
    try {
      setShares(await listIncidentShares(id));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
      setShares([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (submittedId) refresh(submittedId);
  }, [submittedId]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setSubmittedId(incidentId.trim());
  };

  const handleCreate = async () => {
    if (!submittedId) return;
    setCreating(true);
    setErr('');
    setNewShare(null);
    try {
      const result = await createEvidenceShareLink({ incident_id: submittedId, expires_in: createExpiry });
      setNewShare({ url: result.url, expires_at: result.expires_at });
      await refresh(submittedId);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setCreating(false);
    }
  };

  const handleRevoke = async (token: string) => {
    if (!confirm('Revoke this share token? Public links will start returning 404 immediately.')) return;
    try {
      await revokeEvidenceShareLink(token);
      await refresh(submittedId);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="report-card">
      <div className="report-card-header">
        <div>
          <div className="report-card-title">Evidence Shares</div>
          <div className="report-card-subtitle">
            Public-share token lifecycle per incident. Includes revoked
            and expired tokens for full chain-of-custody review.
          </div>
        </div>
      </div>

      <form onSubmit={handleSubmit} className="report-controls">
        <input
          type="text"
          placeholder="Incident ID (e.g. INC-2026-0042)"
          value={incidentId}
          onChange={(e) => setIncidentId(e.target.value)}
          className="report-input"
        />
        <button type="submit" className="report-csv-btn" disabled={!incidentId.trim()}>
          Lookup
        </button>
      </form>

      {submittedId && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 0' }}>
          <span style={{ fontSize: 11, color: 'var(--report-text-dim, #9CA3AF)', flexShrink: 0 }}>New share for {submittedId}:</span>
          <select
            value={createExpiry}
            onChange={(e) => setCreateExpiry(e.target.value as EvidenceShareRequest['expires_in'])}
            className="report-input"
            style={{ flex: 1, maxWidth: 140 }}
          >
            <option value="1h">1 hour</option>
            <option value="1d">1 day</option>
            <option value="1w">1 week</option>
            <option value="1m">1 month</option>
            <option value="never">90 days (max)</option>
          </select>
          <button
            className="report-csv-btn"
            onClick={handleCreate}
            disabled={creating}
            style={{ background: 'rgba(132,204,22,0.15)', color: '#84CC16', border: '1px solid rgba(132,204,22,0.3)', flexShrink: 0 }}
          >
            {creating ? 'Creating…' : '+ Create Link'}
          </button>
        </div>
      )}

      {newShare && (
        <div style={{
          display: 'flex', alignItems: 'center', gap: 8, padding: '8px 10px',
          background: 'rgba(132,204,22,0.06)', border: '1px solid rgba(132,204,22,0.2)',
          borderRadius: 4, marginBottom: 8,
        }}>
          <span style={{ fontSize: 10, color: '#84CC16', fontWeight: 700, flexShrink: 0 }}>CREATED</span>
          <code style={{ fontSize: 10, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--report-text-primary, #E4E8F0)', fontFamily: 'monospace' }}>
            {typeof window !== 'undefined' ? window.location.origin : ''}{newShare.url}
          </code>
          <button
            className="report-csv-btn"
            onClick={() => navigator.clipboard.writeText(`${typeof window !== 'undefined' ? window.location.origin : ''}${newShare.url}`)}
            style={{ fontSize: 10, padding: '2px 8px', flexShrink: 0 }}
          >
            Copy
          </button>
          {newShare.expires_at && (
            <span style={{ fontSize: 9, color: 'var(--report-text-dim, #9CA3AF)', flexShrink: 0 }}>
              expires {fmtAbsolute(newShare.expires_at)}
            </span>
          )}
        </div>
      )}

      {err && <div className="report-error">⚠ {err}</div>}

      {!submittedId ? (
        <div className="report-empty">Enter an incident ID to view its share tokens.</div>
      ) : loading && !shares ? (
        <div className="report-empty">Loading…</div>
      ) : !shares || shares.length === 0 ? (
        <div className="report-empty">No share tokens have been created for {submittedId}.</div>
      ) : (
        <table className="report-table">
          <thead>
            <tr>
              <th>Token</th>
              <th>Status</th>
              <th>Created</th>
              <th>Expires</th>
              <th>Opens</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {shares.map((s) => {
              const badge = statusBadge(s);
              return (
                <tr key={s.token}>
                  <td className="report-bucket" title={s.token}>
                    {s.token.slice(0, 12)}…
                  </td>
                  <td>
                    <span style={{
                      padding: '2px 6px', borderRadius: 3, fontSize: 10, fontWeight: 700,
                      color: badge.color,
                      border: `1px solid ${badge.color}40`,
                    }}>
                      {badge.label}
                    </span>
                  </td>
                  <td>{fmtRelative(s.created_at)}</td>
                  <td>{fmtAbsolute(s.expires_at)}</td>
                  <td className={s.open_count > 0 ? 'report-good' : ''}>{s.open_count}</td>
                  <td>
                    {s.active && !s.revoked ? (
                      <button className="report-revoke-btn" onClick={() => handleRevoke(s.token)}>
                        Revoke
                      </button>
                    ) : (
                      <span className="report-disabled">—</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}
