'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { fetchJSON } from '@/lib/ironsight-api';
import { BRAND } from '@/lib/branding';

// Self-service customer contact list editor for one site. Lives at
// /portal/sites/{siteId}/contacts and is the page a site_manager
// reaches when they say "let me update who answers when an alarm
// fires." Distinct from the SOC-side SOP/call-tree editor in /admin
// — that one belongs to operators, this one belongs to the customer.
//
// Shape per row: name, role (e.g. "Site Foreman"), phone, email,
// notify_on_alarm flag, optional notes. The backend stores it as a
// JSONB array on the sites row.

interface Contact {
  name: string;
  role: string;
  phone: string;
  email: string;
  notify_on_alarm: boolean;
  notes?: string;
}

const EMPTY_CONTACT: Contact = {
  name: '', role: '', phone: '', email: '', notify_on_alarm: true, notes: '',
};

export default function SiteContactsPage() {
  const params = useParams();
  const siteId = String(params?.id ?? '');
  const [contacts, setContacts] = useState<Contact[] | null>(null);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState(0);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (!siteId) return;
    fetchJSON<Contact[]>(`/api/v1/sites/${encodeURIComponent(siteId)}/contacts`)
      .then((rows) => setContacts(rows ?? []))
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)));
  }, [siteId]);

  const save = async (next: Contact[]) => {
    setSaving(true);
    setErr('');
    try {
      await fetchJSON(`/api/v1/sites/${encodeURIComponent(siteId)}/contacts`, {
        method: 'PUT',
        body: JSON.stringify(next),
      });
      setSavedAt(Date.now());
      setContacts(next);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const updateRow = (idx: number, patch: Partial<Contact>) => {
    if (!contacts) return;
    const next = contacts.map((c, i) => (i === idx ? { ...c, ...patch } : c));
    setContacts(next);
  };

  const removeRow = (idx: number) => {
    if (!contacts) return;
    const next = contacts.filter((_, i) => i !== idx);
    save(next);
  };

  const addRow = () => {
    const next = [...(contacts ?? []), { ...EMPTY_CONTACT }];
    setContacts(next);
  };

  return (
    <div style={{
      minHeight: '100vh',
      background: 'var(--sg-bg-base, #0c1015)',
      color: 'var(--sg-text-primary, #E4E8F0)',
      padding: 24,
      fontFamily: "var(--font-family, 'Inter', sans-serif)",
    }}>
      <div style={{ maxWidth: 880, margin: '0 auto' }}>
        <div style={{ marginBottom: 8, fontSize: 12 }}>
          <Link
            href={`/portal/sites/${encodeURIComponent(siteId)}`}
            style={{ color: 'var(--sg-text-dim, #9CA3AF)', textDecoration: 'none' }}
          >
            ← Back to site
          </Link>
        </div>
        <h1 style={{ fontSize: 22, fontWeight: 700, margin: '0 0 4px' }}>
          On-site contacts
        </h1>
        <p style={{ fontSize: 13, color: 'var(--sg-text-dim, #9CA3AF)', margin: '0 0 24px', lineHeight: 1.5 }}>
          People the {BRAND.name} SOC should know about for this site —
          foreman, project manager, security, off-hours emergency. Edits save
          automatically when you click <strong>Save changes</strong>.
        </p>

        {err && (
          <div style={{
            padding: '10px 14px', marginBottom: 16,
            background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.25)',
            borderRadius: 4, color: '#EF9B8B', fontSize: 12,
          }}>⚠ {err}</div>
        )}

        {savedAt > 0 && Date.now() - savedAt < 3000 && (
          <div style={{
            padding: '8px 14px', marginBottom: 16,
            background: 'rgba(132,204,22,0.08)', border: '1px solid rgba(132,204,22,0.25)',
            borderRadius: 4, color: '#A3E635', fontSize: 12,
          }}>✓ Saved</div>
        )}

        {contacts === null ? (
          <div style={{ padding: 32, textAlign: 'center', color: 'var(--sg-text-dim, #9CA3AF)' }}>
            Loading…
          </div>
        ) : (
          <>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginBottom: 16 }}>
              {contacts.length === 0 && (
                <div style={{
                  padding: 32, textAlign: 'center',
                  background: 'var(--sg-surface-1, rgba(255,255,255,0.02))',
                  border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
                  borderRadius: 8,
                  color: 'var(--sg-text-dim, #9CA3AF)', fontSize: 13,
                }}>
                  No contacts on file yet. Add your first below.
                </div>
              )}
              {contacts.map((c, idx) => (
                <ContactCard
                  key={idx}
                  contact={c}
                  onChange={(patch) => updateRow(idx, patch)}
                  onRemove={() => removeRow(idx)}
                />
              ))}
            </div>

            <div style={{ display: 'flex', gap: 8 }}>
              <button onClick={addRow} style={{ ...btn(false), flex: '0 0 auto' }}>
                + Add contact
              </button>
              <button
                onClick={() => save(contacts)}
                disabled={saving}
                style={{ ...btn(true), flex: '1 1 auto', opacity: saving ? 0.6 : 1 }}
              >
                {saving ? 'Saving…' : 'Save changes'}
              </button>
            </div>
          </>
        )}

        <div style={{
          marginTop: 32, paddingTop: 16,
          borderTop: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
          fontSize: 11, color: 'var(--sg-text-dim, #6B7280)', lineHeight: 1.5,
        }}>
          The "Notify on alarm" toggle is a hint to the SOC operator — when
          on, they'll attempt to reach this contact during disposition. To
          control your <em>own</em> account's notification channels (your
          email and phone), use{' '}
          <Link href="/portal/notifications" style={{ color: 'var(--brand-primary, #E8732A)' }}>
            Notification preferences
          </Link>.
        </div>
      </div>
    </div>
  );
}

function ContactCard({
  contact, onChange, onRemove,
}: {
  contact: Contact;
  onChange: (patch: Partial<Contact>) => void;
  onRemove: () => void;
}) {
  return (
    <div style={{
      padding: 14,
      background: 'var(--sg-surface-1, rgba(255,255,255,0.02))',
      border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
      borderRadius: 8,
    }}>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10 }}>
        <Input label="Name" value={contact.name} onChange={(v) => onChange({ name: v })} />
        <Input label="Role" value={contact.role} onChange={(v) => onChange({ role: v })} placeholder="e.g. Site Foreman" />
        <Input label="Phone" value={contact.phone} onChange={(v) => onChange({ phone: v })} placeholder="+1 555 123 4567" />
        <Input label="Email" value={contact.email} onChange={(v) => onChange({ email: v })} placeholder="name@example.com" />
      </div>
      <div style={{ marginBottom: 10 }}>
        <Input label="Notes" value={contact.notes ?? ''} onChange={(v) => onChange({ notes: v })}
               placeholder="On-call hours, gate code, languages spoken…" />
      </div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--sg-text-dim, #B0B8C8)', cursor: 'pointer' }}>
          <input
            type="checkbox"
            checked={contact.notify_on_alarm}
            onChange={(e) => onChange({ notify_on_alarm: e.target.checked })}
          />
          Notify on alarm
        </label>
        <button onClick={onRemove} style={{
          padding: '4px 10px', fontSize: 11,
          background: 'rgba(239,68,68,0.10)',
          border: '1px solid rgba(239,68,68,0.30)',
          borderRadius: 4, color: '#EF4444',
          cursor: 'pointer', fontFamily: 'inherit',
        }}>
          Remove
        </button>
      </div>
    </div>
  );
}

function Input({
  label, value, onChange, placeholder,
}: { label: string; value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontSize: 10, fontWeight: 600, color: 'var(--sg-text-dim, #9CA3AF)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      <input
        type="text"
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        style={{
          padding: '6px 10px', fontSize: 12,
          background: 'var(--sg-surface-2, rgba(255,255,255,0.04))',
          border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.10))',
          borderRadius: 4,
          color: 'var(--sg-text-primary, #E4E8F0)',
          fontFamily: 'inherit',
        }}
      />
    </label>
  );
}

function btn(primary: boolean): React.CSSProperties {
  return primary
    ? {
        padding: '8px 16px', fontSize: 13, fontWeight: 600,
        background: 'var(--brand-primary, #E8732A)', color: '#fff',
        border: 'none', borderRadius: 4, cursor: 'pointer', fontFamily: 'inherit',
      }
    : {
        padding: '8px 16px', fontSize: 13, fontWeight: 600,
        background: 'var(--sg-surface-2, rgba(255,255,255,0.05))',
        color: 'var(--sg-text-primary, #E4E8F0)',
        border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.10))',
        borderRadius: 4, cursor: 'pointer', fontFamily: 'inherit',
      };
}
