'use client';

import { useEffect, useState } from 'react';
import { listUnverifiedSecurityEvents, verifySecurityEvent, type UnverifiedEvent } from '@/lib/ironsight-api';
import { useAuth } from '@/contexts/AuthContext';

// High-severity security events that have been dispositioned but not
// yet signed off by a second supervisor (the UL 827B four-eyes rule).
//
// Each row shows enough context for the supervisor to accept or reject
// the disposition: severity, type, AVS score, the disposing
// operator's callsign, and a one-click verify button. Self-verification
// is impossible — the backend rejects with 409 — but we also disable
// the button client-side when the disposing operator's user_id matches
// the authenticated supervisor, so the UI doesn't even tempt them.

function fmtRelative(ts: number): string {
  const diff = Date.now() - ts;
  const min = Math.floor(diff / 60000);
  if (min < 1)  return 'just now';
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24)  return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

const AVS_LABEL: Record<number, string> = {
  0: 'UNVERIFIED', 1: 'MINIMAL', 2: 'VERIFIED', 3: 'ELEVATED', 4: 'CRITICAL',
};

const AVS_COLORS: Record<number, string> = {
  0: '#9CA3AF', 1: '#E89F2A', 2: '#84CC16', 3: '#F47216', 4: '#EF4444',
};

export default function VerificationQueueCard() {
  const { user } = useAuth();
  const [events, setEvents] = useState<UnverifiedEvent[] | null>(null);
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const [err, setErr] = useState('');

  const refresh = async () => {
    try {
      const list = await listUnverifiedSecurityEvents();
      setEvents(list);
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 30_000); // poll every 30s — auditor watching live
    return () => clearInterval(t);
  }, []);

  const handleVerify = async (eventId: string) => {
    setPending((p) => ({ ...p, [eventId]: true }));
    try {
      await verifySecurityEvent(eventId);
      await refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPending((p) => {
        const n = { ...p };
        delete n[eventId];
        return n;
      });
    }
  };

  return (
    <div className="report-card">
      <div className="report-card-header">
        <div>
          <div className="report-card-title">
            Verification Queue
            {events && events.length > 0 && (
              <span className="report-pill">{events.length} pending</span>
            )}
          </div>
          <div className="report-card-subtitle">
            High-severity dispositions awaiting four-eyes sign-off.
            Required before law-enforcement escalation under UL 827B.
          </div>
        </div>
        <button className="report-csv-btn" onClick={refresh}>↻ Refresh</button>
      </div>

      {err && <div className="report-error">⚠ {err}</div>}

      {events === null ? (
        <div className="report-empty">Loading…</div>
      ) : events.length === 0 ? (
        <div className="report-empty">
          ✓ Queue clear. All high-severity dispositions are verified.
        </div>
      ) : (
        <table className="report-table">
          <thead>
            <tr>
              <th>Event ID</th>
              <th>Severity</th>
              <th>Type</th>
              <th>Disposition</th>
              <th>Disposed by</th>
              <th>AVS</th>
              <th>When</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {events.map((e) => {
              const isSelf = !!(e.disposed_by_user_id && user?.id === e.disposed_by_user_id);
              const score = e.avs_score ?? 0;
              return (
                <tr key={e.id}>
                  <td className="report-bucket">{e.id}</td>
                  <td>
                    <span className={`report-sev report-sev-${e.severity}`}>{e.severity}</span>
                  </td>
                  <td>{e.type || '—'}</td>
                  <td>{e.disposition_label || e.disposition_code}</td>
                  <td className="report-bucket">{e.operator_callsign || '—'}</td>
                  <td>
                    <span style={{
                      padding: '2px 6px', borderRadius: 3, fontSize: 10, fontWeight: 700,
                      color: AVS_COLORS[score],
                      border: `1px solid ${AVS_COLORS[score]}40`,
                    }}>
                      {score} {AVS_LABEL[score]}
                    </span>
                  </td>
                  <td>{fmtRelative(e.ts)}</td>
                  <td>
                    {isSelf ? (
                      <span className="report-disabled" title="Cannot verify your own disposition">
                        — own
                      </span>
                    ) : (
                      <button
                        className="report-verify-btn"
                        onClick={() => handleVerify(e.id)}
                        disabled={!!pending[e.id]}
                      >
                        {pending[e.id] ? '…' : '✓ Verify'}
                      </button>
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
