'use client';

import { useEffect, useRef, useState } from 'react';
import { fetchJSON } from '@/lib/ironsight-api';
import { useAuth } from '@/contexts/AuthContext';

// Floating "Send the SOC a message" widget mounted on every portal
// page. Click the bottom-right bubble → slide-out panel with the
// customer's existing tickets + an inline editor for new messages.
//
// Design notes:
//   - Bubble badge shows count of tickets with last_message_by='soc'
//     since the customer last opened the panel — the equivalent of
//     "unread replies waiting for you."
//   - Polling every 30s while the panel is open keeps the thread
//     live without a websocket — the SOC's reply lands in <1 minute
//     even if the customer never refreshes.
//   - Auto-mounted via the portal layout, so it appears on every
//     portal page and on /portal/sites, /portal/incidents,
//     /portal/notifications, /portal/history.

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

const READ_KEY = 'ironsight_support_seen_at';
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

export default function SupportWidget() {
  const { user, isAuthenticated } = useAuth();
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<'list' | 'compose' | { thread: number }>('list');
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [thread, setThread] = useState<ThreadResponse | null>(null);
  const [draft, setDraft] = useState('');
  const [subject, setSubject] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  // soc_operator is the one role that should not see the widget at all.
  // Operators interact with customers via the alarm flow, not via
  // this customer-facing surface.
  const hideForRole = !isAuthenticated || user?.role === 'soc_operator';

  const reload = async () => {
    try {
      const list = await fetchJSON<Ticket[]>('/api/support/tickets');
      setTickets(list ?? []);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    if (hideForRole) return;
    reload();
    const t = setInterval(reload, POLL_MS);
    return () => clearInterval(t);
  }, [hideForRole]);

  // Live-poll the active thread so the SOC's reply lands without
  // requiring a manual refresh. We also re-pull tickets on each tick
  // so the badge / status pills stay current.
  useEffect(() => {
    if (typeof view !== 'object') return;
    const ticketId = view.thread;
    let cancelled = false;
    const load = async () => {
      try {
        const data = await fetchJSON<ThreadResponse>(`/api/support/tickets/${ticketId}`);
        if (!cancelled) setThread(data);
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : String(e));
      }
    };
    load();
    const t = setInterval(load, POLL_MS);
    return () => { cancelled = true; clearInterval(t); };
  }, [view]);

  const seenAt = parseInt(typeof window !== 'undefined' ? (localStorage.getItem(READ_KEY) ?? '0') : '0', 10);
  const unread = tickets.filter(t => t.last_message_by === 'soc' && new Date(t.last_message_at).getTime() > seenAt).length;

  const handleOpen = () => {
    setOpen(true);
    if (typeof window !== 'undefined') {
      localStorage.setItem(READ_KEY, String(Date.now()));
    }
  };

  const handleSendNew = async () => {
    if (!subject.trim() || !draft.trim()) return;
    setBusy(true);
    setErr('');
    try {
      const t = await fetchJSON<Ticket>('/api/support/tickets', {
        method: 'POST',
        body: JSON.stringify({ subject: subject.trim(), body: draft.trim() }),
      });
      setSubject('');
      setDraft('');
      await reload();
      setView({ thread: t.id });
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleUpdateStatus = async (ticketId: number, status: 'open' | 'closed') => {
    setBusy(true);
    setErr('');
    try {
      await fetchJSON(`/api/support/tickets/${ticketId}`, {
        method: 'PATCH',
        body: JSON.stringify({ status }),
      });
      const data = await fetchJSON<ThreadResponse>(`/api/support/tickets/${ticketId}`);
      setThread(data);
      await reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleReply = async () => {
    if (typeof view !== 'object' || !draft.trim()) return;
    setBusy(true);
    setErr('');
    try {
      await fetchJSON(`/api/support/tickets/${view.thread}/messages`, {
        method: 'POST',
        body: JSON.stringify({ body: draft.trim() }),
      });
      setDraft('');
      // Re-pull the thread immediately rather than wait for the
      // 30s poll — the customer just hit send, the message should
      // appear right away.
      const data = await fetchJSON<ThreadResponse>(`/api/support/tickets/${view.thread}`);
      setThread(data);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  if (hideForRole) return null;

  return (
    <>
      {!open && (
        <button
          onClick={handleOpen}
          aria-label="Open support chat"
          style={{
            position: 'fixed', right: 20, bottom: 80,
            width: 56, height: 56, borderRadius: 28,
            background: 'var(--brand-primary, #E8732A)', color: '#fff',
            border: 'none', cursor: 'pointer',
            boxShadow: '0 6px 20px rgba(0,0,0,0.3)',
            fontSize: 24,
            zIndex: 90,
            transition: 'transform 0.15s',
          }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.transform = 'scale(1.05)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.transform = 'scale(1)'; }}
        >
          💬
          {unread > 0 && (
            <span style={{
              position: 'absolute', top: 0, right: 0,
              minWidth: 20, height: 20, padding: '0 6px',
              borderRadius: 10,
              background: '#dc2626', color: '#fff',
              fontSize: 11, fontWeight: 700,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              border: '2px solid var(--sg-bg-base, #0c1015)',
            }}>{unread}</span>
          )}
        </button>
      )}

      {open && (
        <div
          role="dialog"
          aria-label="SOC support"
          style={{
            position: 'fixed',
            right: 20, bottom: 20,
            width: 'calc(100vw - 40px)', maxWidth: 380,
            height: 'calc(100vh - 100px)', maxHeight: 560,
            background: 'var(--sg-surface-1, #181c22)',
            border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.10))',
            borderRadius: 12,
            boxShadow: '0 12px 40px rgba(0,0,0,0.5)',
            color: 'var(--sg-text-primary, #E4E8F0)',
            fontFamily: "var(--font-family, 'Inter', sans-serif)",
            zIndex: 95,
            display: 'flex', flexDirection: 'column',
            overflow: 'hidden',
          }}
        >
          <Header
            title={
              view === 'list' ? 'SOC support' :
              view === 'compose' ? 'New message' :
              thread?.ticket.subject ?? 'Conversation'
            }
            onBack={view !== 'list' ? () => { setView('list'); setThread(null); setDraft(''); } : undefined}
            onClose={() => setOpen(false)}
          />

          <div style={{ flex: 1, overflowY: 'auto', padding: 12 }}>
            {err && (
              <div style={{
                padding: '8px 12px', marginBottom: 10,
                background: 'rgba(239,68,68,0.10)',
                border: '1px solid rgba(239,68,68,0.25)',
                borderRadius: 6, color: '#EF9B8B', fontSize: 11,
              }}>⚠ {err}</div>
            )}

            {view === 'list' && (
              <TicketList tickets={tickets} onPick={(id) => setView({ thread: id })} />
            )}

            {view === 'compose' && (
              <ComposeForm
                subject={subject} setSubject={setSubject}
                draft={draft} setDraft={setDraft}
                busy={busy} onSend={handleSendNew}
              />
            )}

            {typeof view === 'object' && thread && (
              <Thread
                thread={thread}
                draft={draft} setDraft={setDraft}
                busy={busy} onSend={handleReply}
                meId={user?.id}
                onUpdateStatus={(status) => handleUpdateStatus(thread.ticket.id, status)}
              />
            )}
          </div>

          {view === 'list' && (
            <button
              onClick={() => setView('compose')}
              style={{
                margin: 12,
                padding: '10px 16px',
                background: 'var(--brand-primary, #E8732A)', color: '#fff',
                border: 'none', borderRadius: 6,
                fontSize: 13, fontWeight: 600,
                cursor: 'pointer', fontFamily: 'inherit',
              }}
            >
              + New message
            </button>
          )}
        </div>
      )}
    </>
  );
}

function Header({ title, onBack, onClose }: { title: string; onBack?: () => void; onClose: () => void }) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 10,
      padding: '12px 14px',
      borderBottom: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.08))',
      background: 'var(--sg-surface-2, rgba(255,255,255,0.02))',
    }}>
      {onBack && (
        <button onClick={onBack} aria-label="Back" style={iconBtnStyle}>←</button>
      )}
      <div style={{ flex: 1, fontSize: 13, fontWeight: 700, letterSpacing: 0.3, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {title}
      </div>
      <button onClick={onClose} aria-label="Close" style={iconBtnStyle}>×</button>
    </div>
  );
}

const iconBtnStyle: React.CSSProperties = {
  width: 28, height: 28,
  background: 'transparent',
  border: '1px solid rgba(255,255,255,0.10)',
  borderRadius: 4,
  color: 'var(--sg-text-dim, #B0B8C8)',
  cursor: 'pointer', fontFamily: 'inherit',
  fontSize: 16, lineHeight: 1,
};

function TicketList({ tickets, onPick }: { tickets: Ticket[]; onPick: (id: number) => void }) {
  if (tickets.length === 0) {
    return (
      <div style={{ padding: 20, textAlign: 'center', color: 'var(--sg-text-dim, #9CA3AF)', fontSize: 12, lineHeight: 1.6 }}>
        Nothing here yet. Use the button below to send the SOC a message —
        questions, schedule changes, anything.
      </div>
    );
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      {tickets.map((t) => {
        const waiting = t.last_message_by === 'soc';
        return (
          <button
            key={t.id}
            onClick={() => onPick(t.id)}
            style={{
              textAlign: 'left',
              padding: 10,
              background: waiting ? 'rgba(232,115,42,0.08)' : 'var(--sg-surface-2, rgba(255,255,255,0.02))',
              border: `1px solid ${waiting ? 'rgba(232,115,42,0.30)' : 'var(--sg-border-subtle, rgba(255,255,255,0.06))'}`,
              borderRadius: 6,
              cursor: 'pointer',
              fontFamily: 'inherit',
              color: 'var(--sg-text-primary, #E4E8F0)',
            }}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
              <div style={{ fontSize: 12, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>
                {t.subject}
              </div>
              <StatusPill status={t.status} />
            </div>
            <div style={{ fontSize: 10, color: 'var(--sg-text-dim, #9CA3AF)', marginTop: 4, display: 'flex', justifyContent: 'space-between', gap: 6 }}>
              <span>{t.message_count} message{t.message_count === 1 ? '' : 's'}</span>
              <span>{fmtRelative(t.last_message_at)}</span>
            </div>
          </button>
        );
      })}
    </div>
  );
}

function StatusPill({ status }: { status: Status }) {
  const map: Record<Status, { label: string; bg: string; fg: string }> = {
    open:     { label: 'AWAITING SOC', bg: 'rgba(232,115,42,0.12)', fg: '#E8732A' },
    answered: { label: 'NEW REPLY',    bg: 'rgba(132,204,22,0.12)', fg: '#84CC16' },
    closed:   { label: 'CLOSED',       bg: 'rgba(75,85,99,0.20)',   fg: '#9CA3AF' },
  };
  const m = map[status];
  return (
    <span style={{
      fontSize: 9, fontWeight: 700, letterSpacing: 0.4,
      padding: '2px 6px', borderRadius: 3,
      background: m.bg, color: m.fg,
      whiteSpace: 'nowrap', flexShrink: 0,
    }}>{m.label}</span>
  );
}

function ComposeForm({
  subject, setSubject, draft, setDraft, busy, onSend,
}: {
  subject: string; setSubject: (v: string) => void;
  draft: string; setDraft: (v: string) => void;
  busy: boolean; onSend: () => void;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <span style={fieldLabelStyle}>SUBJECT</span>
        <input
          type="text"
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          placeholder="What's this about?"
          maxLength={200}
          style={fieldInputStyle}
        />
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <span style={fieldLabelStyle}>MESSAGE</span>
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Tell us what you need…"
          rows={6}
          maxLength={8000}
          style={{ ...fieldInputStyle, resize: 'vertical', fontFamily: 'inherit' }}
        />
      </label>
      <button
        onClick={onSend}
        disabled={busy || !subject.trim() || !draft.trim()}
        style={{
          padding: '10px', fontSize: 13, fontWeight: 600,
          background: 'var(--brand-primary, #E8732A)', color: '#fff',
          border: 'none', borderRadius: 6,
          cursor: busy ? 'wait' : 'pointer',
          opacity: busy || !subject.trim() || !draft.trim() ? 0.6 : 1,
          fontFamily: 'inherit',
        }}
      >
        {busy ? 'Sending…' : 'Send to SOC'}
      </button>
    </div>
  );
}

function Thread({
  thread, draft, setDraft, busy, onSend, meId, onUpdateStatus,
}: {
  thread: ThreadResponse; draft: string; setDraft: (v: string) => void;
  busy: boolean; onSend: () => void;
  meId?: string;
  onUpdateStatus: (status: 'open' | 'closed') => void;
}) {
  const endRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [thread.messages.length]);

  const isClosed = thread.ticket.status === 'closed';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 2 }}>
        <button
          onClick={() => onUpdateStatus(isClosed ? 'open' : 'closed')}
          disabled={busy}
          style={{
            fontSize: 10, fontWeight: 600,
            padding: '3px 10px', borderRadius: 4,
            border: `1px solid ${isClosed ? 'rgba(132,204,22,0.30)' : 'rgba(156,163,175,0.30)'}`,
            background: isClosed ? 'rgba(132,204,22,0.08)' : 'rgba(75,85,99,0.12)',
            color: isClosed ? '#84CC16' : '#9CA3AF',
            cursor: busy ? 'wait' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          {isClosed ? 'Reopen' : 'Close ticket'}
        </button>
      </div>
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
              background: mine
                ? 'rgba(232,115,42,0.14)'
                : soc ? 'rgba(132,204,22,0.10)' : 'var(--sg-surface-2, rgba(255,255,255,0.04))',
              border: `1px solid ${mine ? 'rgba(232,115,42,0.30)' : soc ? 'rgba(132,204,22,0.20)' : 'var(--sg-border-subtle, rgba(255,255,255,0.08))'}`,
              borderRadius: 8,
              borderTopRightRadius: mine ? 2 : 8,
              borderTopLeftRadius: mine ? 8 : 2,
            }}
          >
            <div style={{ fontSize: 10, color: 'var(--sg-text-dim, #9CA3AF)', marginBottom: 3, display: 'flex', justifyContent: 'space-between', gap: 6 }}>
              <span>{soc ? `🛡 ${m.author_name || 'SOC'}` : (m.author_name || 'You')}</span>
              <span>{fmtRelative(m.created_at)}</span>
            </div>
            <div style={{ fontSize: 12, lineHeight: 1.5, whiteSpace: 'pre-wrap' }}>
              {m.body}
            </div>
          </div>
        );
      })}
      <div ref={endRef} />
      <div style={{ display: 'flex', gap: 6, marginTop: 4 }}>
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Reply…"
          rows={2}
          maxLength={8000}
          style={{ ...fieldInputStyle, resize: 'none', flex: 1, fontFamily: 'inherit' }}
        />
        <button
          onClick={onSend}
          disabled={busy || !draft.trim()}
          style={{
            padding: '0 14px', fontSize: 12, fontWeight: 600,
            background: 'var(--brand-primary, #E8732A)', color: '#fff',
            border: 'none', borderRadius: 6,
            cursor: busy ? 'wait' : 'pointer',
            opacity: busy || !draft.trim() ? 0.6 : 1,
            fontFamily: 'inherit',
          }}
        >
          Send
        </button>
      </div>
    </div>
  );
}

const fieldLabelStyle: React.CSSProperties = {
  fontSize: 9, fontWeight: 700, letterSpacing: 0.5,
  textTransform: 'uppercase',
  color: 'var(--sg-text-dim, #9CA3AF)',
};
const fieldInputStyle: React.CSSProperties = {
  padding: '8px 10px', fontSize: 12,
  background: 'var(--sg-surface-2, rgba(255,255,255,0.04))',
  border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.10))',
  borderRadius: 4,
  color: 'var(--sg-text-primary, #E4E8F0)',
};
