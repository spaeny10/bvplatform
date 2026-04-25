'use client';

import { useEffect, useRef, useState } from 'react';
import { fetchJSON } from '@/lib/ironsight-api';
import { useAuth } from '@/contexts/AuthContext';

// Supervisor-side support inbox. Lives in the /reports page as the
// "Support" tab. Mirrors the customer SupportWidget but globally-
// scoped — supervisors see every org's open tickets, click in to
// reply. Same polling pattern (30s) keeps the queue current.

type Status = 'open' | 'answered' | 'closed';

interface Ticket {
  id: number;
  organization_id: string;
  site_id: string;
  created_by_name: string;
  subject: string;
  status: Status;
  created_at: string;
  last_message_at: string;
  last_message_by: 'customer' | 'soc';
  message_count: number;
}

interface Message {
  id: number;
  ticket_id: number;
  author_id: string;
  author_name: string;
  author_role: string;
  body: string;
  created_at: string;
}

interface ThreadResponse {
  ticket: Ticket;
  messages: Message[];
}

const POLL_MS = 30_000;

function fmtRelative(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const diff = Date.now() - d.getTime();
  if (diff < 60_000) return 'just now';
  const min = Math.floor(diff / 60_000);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

export default function SupportTicketsCard() {
  const { user } = useAuth();
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [thread, setThread] = useState<ThreadResponse | null>(null);
  const [filter, setFilter] = useState<'open' | 'answered' | 'all'>('open');
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  const reload = async () => {
    try {
      const list = await fetchJSON<Ticket[]>(`/api/support/tickets?status=${filter === 'all' ? '' : filter}`);
      setTickets(list ?? []);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    reload();
    const t = setInterval(reload, POLL_MS);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter]);

  // Live-poll the active thread.
  useEffect(() => {
    if (!thread) return;
    const tid = thread.ticket.id;
    let cancelled = false;
    const t = setInterval(async () => {
      try {
        const data = await fetchJSON<ThreadResponse>(`/api/support/tickets/${tid}`);
        if (!cancelled) setThread(data);
      } catch {
        // Ignore polling errors; the operator can still send.
      }
    }, POLL_MS);
    return () => { cancelled = true; clearInterval(t); };
  }, [thread?.ticket.id]);

  const openThread = async (id: number) => {
    setErr('');
    try {
      const data = await fetchJSON<ThreadResponse>(`/api/support/tickets/${id}`);
      setThread(data);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  const sendReply = async () => {
    if (!thread || !draft.trim()) return;
    setBusy(true);
    setErr('');
    try {
      await fetchJSON(`/api/support/tickets/${thread.ticket.id}/messages`, {
        method: 'POST',
        body: JSON.stringify({ body: draft.trim() }),
      });
      setDraft('');
      const data = await fetchJSON<ThreadResponse>(`/api/support/tickets/${thread.ticket.id}`);
      setThread(data);
      await reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const closeTicket = async () => {
    if (!thread) return;
    if (!confirm('Close this ticket? The customer can re-open it by replying.')) return;
    try {
      await fetchJSON(`/api/support/tickets/${thread.ticket.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ status: 'closed' }),
      });
      setThread(null);
      await reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="report-card">
      <div className="report-card-header">
        <div>
          <div className="report-card-title">
            Customer Support
            {tickets.filter(t => t.status === 'open').length > 0 && (
              <span className="report-pill">
                {tickets.filter(t => t.status === 'open').length} awaiting reply
              </span>
            )}
          </div>
          <div className="report-card-subtitle">
            Tickets opened by customers across all organizations. Reply
            here to keep the audit trail attached to each thread.
          </div>
        </div>
        <button className="report-csv-btn" onClick={reload}>↻ Refresh</button>
      </div>

      <div className="report-controls">
        <div className="report-segmented">
          <button className={`report-segment ${filter === 'open' ? 'active' : ''}`} onClick={() => setFilter('open')}>Open</button>
          <button className={`report-segment ${filter === 'answered' ? 'active' : ''}`} onClick={() => setFilter('answered')}>Answered</button>
          <button className={`report-segment ${filter === 'all' ? 'active' : ''}`} onClick={() => setFilter('all')}>All</button>
        </div>
      </div>

      {err && <div className="report-error">⚠ {err}</div>}

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1.5fr)', gap: 12, alignItems: 'flex-start' }}>
        {/* Ticket list */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, maxHeight: 540, overflowY: 'auto' }}>
          {tickets.length === 0 ? (
            <div className="report-empty">No tickets in this view.</div>
          ) : (
            tickets.map((t) => {
              const active = thread?.ticket.id === t.id;
              const waiting = t.last_message_by === 'customer' && t.status === 'open';
              return (
                <button
                  key={t.id}
                  onClick={() => openThread(t.id)}
                  style={{
                    textAlign: 'left',
                    padding: 10,
                    background: active
                      ? 'rgba(232,115,42,0.10)'
                      : waiting ? 'rgba(232,115,42,0.04)' : 'var(--sg-surface-2, rgba(255,255,255,0.02))',
                    border: `1px solid ${active ? 'rgba(232,115,42,0.40)' : waiting ? 'rgba(232,115,42,0.20)' : 'var(--sg-border-subtle, rgba(255,255,255,0.06))'}`,
                    borderRadius: 6,
                    cursor: 'pointer', fontFamily: 'inherit',
                    color: 'var(--sg-text-primary, #E4E8F0)',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 6, alignItems: 'center' }}>
                    <div style={{ fontSize: 12, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>
                      {t.subject}
                    </div>
                    <span style={{
                      fontSize: 9, fontWeight: 700, letterSpacing: 0.4,
                      padding: '2px 6px', borderRadius: 3,
                      background: t.status === 'open' ? 'rgba(232,115,42,0.15)' : t.status === 'answered' ? 'rgba(132,204,22,0.10)' : 'rgba(75,85,99,0.20)',
                      color: t.status === 'open' ? '#E8732A' : t.status === 'answered' ? '#84CC16' : '#9CA3AF',
                      whiteSpace: 'nowrap', flexShrink: 0,
                    }}>{t.status.toUpperCase()}</span>
                  </div>
                  <div style={{ fontSize: 10, color: 'var(--sg-text-dim, #9CA3AF)', marginTop: 4, display: 'flex', justifyContent: 'space-between', gap: 6 }}>
                    <span className="report-bucket">{t.organization_id}{t.site_id ? ` · ${t.site_id}` : ''}</span>
                    <span>{fmtRelative(t.last_message_at)}</span>
                  </div>
                  <div style={{ fontSize: 10, color: 'var(--sg-text-dim, #6B7280)', marginTop: 2 }}>
                    {t.created_by_name} · {t.message_count} message{t.message_count === 1 ? '' : 's'}
                  </div>
                </button>
              );
            })
          )}
        </div>

        {/* Thread pane */}
        {thread ? (
          <ThreadPane
            thread={thread}
            draft={draft} setDraft={setDraft}
            busy={busy} onSend={sendReply}
            onClose={closeTicket}
            meId={user?.id}
          />
        ) : (
          <div className="report-empty" style={{ minHeight: 200 }}>
            Select a ticket from the list to read and reply.
          </div>
        )}
      </div>
    </div>
  );
}

function ThreadPane({
  thread, draft, setDraft, busy, onSend, onClose, meId,
}: {
  thread: ThreadResponse;
  draft: string; setDraft: (v: string) => void;
  busy: boolean; onSend: () => void; onClose: () => void;
  meId?: string;
}) {
  const endRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [thread.messages.length]);

  return (
    <div style={{
      display: 'flex', flexDirection: 'column', gap: 8,
      maxHeight: 540,
      padding: 12,
      background: 'var(--sg-surface-2, rgba(255,255,255,0.02))',
      border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
      borderRadius: 8,
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 10, paddingBottom: 8, borderBottom: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))' }}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 13, fontWeight: 700 }}>{thread.ticket.subject}</div>
          <div style={{ fontSize: 11, color: 'var(--sg-text-dim, #9CA3AF)', marginTop: 2 }}>
            {thread.ticket.organization_id}
            {thread.ticket.site_id && ` · ${thread.ticket.site_id}`}
            {' · opened by '}{thread.ticket.created_by_name}
          </div>
        </div>
        {thread.ticket.status !== 'closed' && (
          <button
            onClick={onClose}
            style={{
              padding: '4px 10px', fontSize: 11, fontWeight: 600,
              background: 'rgba(75,85,99,0.20)',
              border: '1px solid rgba(75,85,99,0.30)',
              borderRadius: 4,
              color: 'var(--sg-text-dim, #B0B8C8)',
              cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            Close
          </button>
        )}
      </div>

      <div style={{ flex: 1, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 8 }}>
        {thread.messages.map((m) => {
          const mine = m.author_id === meId;
          const soc = m.author_role === 'admin' || m.author_role === 'soc_supervisor';
          return (
            <div
              key={m.id}
              style={{
                alignSelf: mine ? 'flex-end' : 'flex-start',
                maxWidth: '85%',
                padding: '8px 10px',
                background: mine ? 'rgba(232,115,42,0.12)' : soc ? 'rgba(132,204,22,0.08)' : 'var(--sg-surface-2, rgba(255,255,255,0.04))',
                border: `1px solid ${mine ? 'rgba(232,115,42,0.30)' : soc ? 'rgba(132,204,22,0.20)' : 'var(--sg-border-subtle, rgba(255,255,255,0.08))'}`,
                borderRadius: 8,
              }}
            >
              <div style={{ fontSize: 10, color: 'var(--sg-text-dim, #9CA3AF)', marginBottom: 3, display: 'flex', justifyContent: 'space-between', gap: 6 }}>
                <span>{soc ? `🛡 ${m.author_name || 'SOC'}` : (m.author_name || 'Customer')}</span>
                <span>{fmtRelative(m.created_at)}</span>
              </div>
              <div style={{ fontSize: 12, lineHeight: 1.5, whiteSpace: 'pre-wrap' }}>
                {m.body}
              </div>
            </div>
          );
        })}
        <div ref={endRef} />
      </div>

      {thread.ticket.status !== 'closed' && (
        <div style={{ display: 'flex', gap: 6, marginTop: 4 }}>
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="Reply to the customer…"
            rows={3}
            maxLength={8000}
            style={{
              flex: 1, padding: '8px 10px', fontSize: 12,
              background: 'var(--sg-surface-1, rgba(255,255,255,0.04))',
              border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.10))',
              borderRadius: 4,
              color: 'var(--sg-text-primary, #E4E8F0)',
              fontFamily: 'inherit', resize: 'none',
            }}
          />
          <button
            onClick={onSend}
            disabled={busy || !draft.trim()}
            style={{
              padding: '0 14px', fontSize: 12, fontWeight: 600,
              background: 'var(--brand-primary, #E8732A)', color: '#fff',
              border: 'none', borderRadius: 4,
              cursor: busy ? 'wait' : 'pointer',
              opacity: busy || !draft.trim() ? 0.6 : 1,
              fontFamily: 'inherit',
            }}
          >
            Send
          </button>
        </div>
      )}
    </div>
  );
}
