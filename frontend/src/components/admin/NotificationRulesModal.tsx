'use client';

import { useState, useEffect } from 'react';
import { getNotificationRules, deleteNotificationRule, createNotificationRule } from '@/lib/ironsight-api';
import type { NotificationRule, Severity } from '@/types/ironsight';

interface Props { siteId: string; onClose: () => void; embedded?: boolean; }

const CHANNEL_ICONS: Record<string, string> = { sms: '📱', email: '✉️', webhook: '🔗', push: '🔔' };
const SEVERITY_COLORS: Record<string, string> = { critical: '#EF4444', high: '#F97316', medium: '#E89B2A', low: '#3B82F6' };
const ALL_SEVERITIES: Severity[] = ['critical', 'high', 'medium', 'low'];
const ALL_CHANNELS = ['email', 'sms', 'webhook', 'push'] as const;

export default function NotificationRulesModal({ siteId, onClose, embedded }: Props) {
  const [rules, setRules] = useState<NotificationRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);

  // Create form state
  const [newName, setNewName] = useState('');
  const [newSeverities, setNewSeverities] = useState<Severity[]>(['critical', 'high']);
  const [newChannels, setNewChannels] = useState<typeof ALL_CHANNELS[number][]>(['email']);
  const [newSchedule, setNewSchedule] = useState<'immediate' | 'digest_hourly' | 'digest_daily'>('immediate');
  const [newRecipients, setNewRecipients] = useState([{ name: '', email: '', phone: '' }]);
  const [newWebhookUrl, setNewWebhookUrl] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    getNotificationRules(siteId).then(data => { setRules(data); setLoading(false); });
  }, [siteId]);

  const handleDelete = async (ruleId: string) => {
    await deleteNotificationRule(ruleId);
    setRules(prev => prev.filter(r => r.id !== ruleId));
  };

  const toggleSeverity = (s: Severity) => {
    setNewSeverities(prev => prev.includes(s) ? prev.filter(x => x !== s) : [...prev, s]);
  };

  const toggleChannel = (c: typeof ALL_CHANNELS[number]) => {
    setNewChannels(prev => prev.includes(c) ? prev.filter(x => x !== c) : [...prev, c]);
  };

  const addRecipient = () => setNewRecipients(prev => [...prev, { name: '', email: '', phone: '' }]);
  const removeRecipient = (idx: number) => setNewRecipients(prev => prev.filter((_, i) => i !== idx));
  const updateRecipient = (idx: number, field: string, val: string) => {
    setNewRecipients(prev => prev.map((r, i) => i === idx ? { ...r, [field]: val } : r));
  };

  const handleCreate = async () => {
    if (!newName.trim() || newSeverities.length === 0 || newChannels.length === 0) return;
    setSaving(true);
    try {
      const recipients = newRecipients
        .filter(r => r.name.trim())
        .map(r => ({
          name: r.name.trim(),
          email: r.email.trim() || undefined,
          phone: r.phone.trim() || undefined,
          webhook_url: newChannels.includes('webhook') ? newWebhookUrl.trim() || undefined : undefined,
        }));

      const rule = await createNotificationRule({
        site_id: siteId,
        name: newName.trim(),
        severity_filter: newSeverities,
        channels: newChannels,
        recipients,
        schedule: newSchedule,
        enabled: true,
      });
      setRules(prev => [...prev, rule]);
      resetForm();
    } catch { /* error display could go here */ }
    setSaving(false);
  };

  const resetForm = () => {
    setShowCreate(false);
    setNewName('');
    setNewSeverities(['critical', 'high']);
    setNewChannels(['email']);
    setNewSchedule('immediate');
    setNewRecipients([{ name: '', email: '', phone: '' }]);
    setNewWebhookUrl('');
  };

  const bodyContent = (
    <div>
      {/* Toolbar */}
      <div style={{ padding: '10px 16px', borderBottom: '1px solid rgba(255,255,255,0.04)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span style={{ fontSize: 11, color: '#4A5268' }}>{rules.length} rule{rules.length !== 1 ? 's' : ''}</span>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          style={{
            padding: '5px 12px', borderRadius: 4, fontSize: 11, fontWeight: 600,
            background: 'rgba(232,115,42,0.1)', border: '1px solid rgba(232,115,42,0.3)',
            color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
          }}
        >
          + Create Rule
        </button>
      </div>

      {/* ── Create Form ── */}
      {showCreate && (
        <div style={{ padding: '16px', borderBottom: '1px solid rgba(255,255,255,0.06)', background: 'rgba(232,115,42,0.03)' }}>
          <div style={{ fontSize: 12, fontWeight: 600, color: '#E8732A', marginBottom: 12 }}>New Alert Rule</div>

          {/* Name */}
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', marginBottom: 4 }}>Rule Name</div>
            <input
              className="admin-input"
              value={newName}
              onChange={e => setNewName(e.target.value)}
              placeholder="e.g. Critical alerts to security team"
              style={{ fontSize: 12, padding: '6px 10px', width: '100%', boxSizing: 'border-box' }}
            />
          </div>

          {/* Severity filter */}
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', marginBottom: 4 }}>Trigger on severities</div>
            <div style={{ display: 'flex', gap: 6 }}>
              {ALL_SEVERITIES.map(s => (
                <button
                  key={s}
                  type="button"
                  onClick={() => toggleSeverity(s)}
                  style={{
                    padding: '4px 10px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                    cursor: 'pointer', fontFamily: 'inherit', textTransform: 'capitalize',
                    background: newSeverities.includes(s) ? `${SEVERITY_COLORS[s]}18` : 'rgba(255,255,255,0.02)',
                    border: `1px solid ${newSeverities.includes(s) ? `${SEVERITY_COLORS[s]}50` : 'rgba(255,255,255,0.06)'}`,
                    color: newSeverities.includes(s) ? SEVERITY_COLORS[s] : '#4A5268',
                  }}
                >
                  {newSeverities.includes(s) ? '✓ ' : ''}{s}
                </button>
              ))}
            </div>
          </div>

          {/* Channels */}
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', marginBottom: 4 }}>Notification channels</div>
            <div style={{ display: 'flex', gap: 6 }}>
              {ALL_CHANNELS.map(c => (
                <button
                  key={c}
                  type="button"
                  onClick={() => toggleChannel(c)}
                  style={{
                    padding: '4px 10px', borderRadius: 4, fontSize: 11, fontWeight: 500,
                    cursor: 'pointer', fontFamily: 'inherit', textTransform: 'capitalize',
                    background: newChannels.includes(c) ? 'rgba(59,130,246,0.1)' : 'rgba(255,255,255,0.02)',
                    border: `1px solid ${newChannels.includes(c) ? 'rgba(59,130,246,0.4)' : 'rgba(255,255,255,0.06)'}`,
                    color: newChannels.includes(c) ? '#3B82F6' : '#4A5268',
                  }}
                >
                  {CHANNEL_ICONS[c]} {c}
                </button>
              ))}
            </div>
          </div>

          {/* Webhook URL (if webhook selected) */}
          {newChannels.includes('webhook') && (
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', marginBottom: 4 }}>Webhook URL</div>
              <input
                className="admin-input"
                value={newWebhookUrl}
                onChange={e => setNewWebhookUrl(e.target.value)}
                placeholder="https://hooks.example.com/alerts"
                type="url"
                style={{ fontSize: 12, padding: '6px 10px', width: '100%', boxSizing: 'border-box' }}
              />
            </div>
          )}

          {/* Delivery schedule */}
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', marginBottom: 4 }}>Delivery</div>
            <div style={{ display: 'flex', gap: 6 }}>
              {([
                { value: 'immediate', label: 'Immediate' },
                { value: 'digest_hourly', label: 'Hourly Digest' },
                { value: 'digest_daily', label: 'Daily Digest' },
              ] as const).map(opt => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setNewSchedule(opt.value)}
                  style={{
                    padding: '4px 10px', borderRadius: 4, fontSize: 11, fontWeight: 500,
                    cursor: 'pointer', fontFamily: 'inherit',
                    background: newSchedule === opt.value ? 'rgba(34,197,94,0.1)' : 'rgba(255,255,255,0.02)',
                    border: `1px solid ${newSchedule === opt.value ? 'rgba(34,197,94,0.4)' : 'rgba(255,255,255,0.06)'}`,
                    color: newSchedule === opt.value ? '#22C55E' : '#4A5268',
                  }}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>

          {/* Recipients */}
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', marginBottom: 4 }}>Recipients</div>
            {newRecipients.map((r, idx) => (
              <div key={idx} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr auto', gap: 6, marginBottom: 6 }}>
                <input className="admin-input" value={r.name} onChange={e => updateRecipient(idx, 'name', e.target.value)} placeholder="Name" style={{ fontSize: 11, padding: '4px 8px' }} />
                <input className="admin-input" value={r.email} onChange={e => updateRecipient(idx, 'email', e.target.value)} placeholder="Email" type="email" style={{ fontSize: 11, padding: '4px 8px' }} />
                <input className="admin-input" value={r.phone} onChange={e => updateRecipient(idx, 'phone', e.target.value)} placeholder="Phone" style={{ fontSize: 11, padding: '4px 8px' }} />
                {newRecipients.length > 1 && (
                  <button type="button" onClick={() => removeRecipient(idx)} style={{ background: 'none', border: 'none', color: '#EF4444', cursor: 'pointer', fontSize: 12, padding: '0 4px' }}>✕</button>
                )}
              </div>
            ))}
            <button
              type="button"
              onClick={addRecipient}
              style={{ fontSize: 10, color: '#8891A5', background: 'none', border: 'none', cursor: 'pointer', fontFamily: 'inherit', padding: 0 }}
            >
              + Add recipient
            </button>
          </div>

          {/* Actions */}
          <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
            <button type="button" onClick={resetForm} style={{ padding: '6px 14px', borderRadius: 4, fontSize: 11, background: 'none', border: '1px solid rgba(255,255,255,0.1)', color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit' }}>
              Cancel
            </button>
            <button
              type="button"
              onClick={handleCreate}
              disabled={!newName.trim() || newSeverities.length === 0 || newChannels.length === 0 || saving}
              style={{
                padding: '6px 14px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                background: 'rgba(232,115,42,0.1)', border: '1px solid rgba(232,115,42,0.4)',
                color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
              }}
            >
              {saving ? 'Creating...' : 'Create Rule'}
            </button>
          </div>
        </div>
      )}

      {/* ── Rule List ── */}
      {loading && <div style={{ padding: 30, textAlign: 'center', color: '#4A5268' }}>Loading…</div>}
      {rules.map(rule => (
        <div key={rule.id} style={{ padding: '12px 16px', borderBottom: '1px solid rgba(255,255,255,0.04)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: '#E4E8F0' }}>{rule.name}</div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span style={{
                fontSize: 8, padding: '2px 6px', borderRadius: 2, fontWeight: 600,
                background: rule.enabled ? 'rgba(0,229,160,0.1)' : 'rgba(255,255,255,0.04)',
                color: rule.enabled ? '#22C55E' : '#4A5268',
                border: `1px solid ${rule.enabled ? 'rgba(0,229,160,0.25)' : 'rgba(255,255,255,0.06)'}`,
              }}>{rule.enabled ? 'ACTIVE' : 'PAUSED'}</span>
              <button type="button" style={{ fontSize: 9, padding: '2px 6px', background: 'none', border: '1px solid rgba(239,68,68,0.2)', borderRadius: 3, color: '#EF4444', cursor: 'pointer', fontFamily: 'inherit' }} onClick={() => handleDelete(rule.id)}>✕</button>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 16, fontSize: 10 }}>
            <div>
              <span style={{ color: '#4A5268' }}>Severity: </span>
              {rule.severity_filter.map(s => (
                <span key={s} style={{ color: SEVERITY_COLORS[s], fontWeight: 600, marginRight: 4 }}>{s}</span>
              ))}
            </div>
            <div>
              <span style={{ color: '#4A5268' }}>Channels: </span>
              {rule.channels.map(c => <span key={c} style={{ marginRight: 4 }}>{CHANNEL_ICONS[c] || c}</span>)}
            </div>
            <div style={{ color: '#4A5268' }}>
              {rule.schedule === 'immediate' ? 'Immediate' : rule.schedule === 'digest_hourly' ? 'Hourly digest' : 'Daily digest'}
            </div>
          </div>
          {rule.recipients.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap' as const, gap: 4, marginTop: 6 }}>
              {rule.recipients.map((r, i) => (
                <span key={i} style={{ fontSize: 9, padding: '2px 6px', borderRadius: 2, background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.06)', color: '#8891A5' }}>
                  {r.name}{r.email ? ` · ${r.email}` : ''}{r.phone ? ` · ${r.phone}` : ''}
                </span>
              ))}
            </div>
          )}
        </div>
      ))}
      {!loading && rules.length === 0 && !showCreate && (
        <div style={{ padding: 30, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
          No alert rules configured. Create one to start routing notifications.
        </div>
      )}
    </div>
  );

  if (embedded) return bodyContent;

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal wide" onClick={e => e.stopPropagation()}>
        <div className="admin-modal-header">
          <div>
            <div className="admin-modal-title">Alert Rules</div>
            <div style={{ fontSize: 11, color: '#4A5268', marginTop: 2 }}>{siteId} · {rules.length} rule{rules.length !== 1 ? 's' : ''}</div>
          </div>
          <button className="admin-modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="admin-modal-body" style={{ padding: 0 }}>{bodyContent}</div>
        <div className="admin-modal-footer">
          <button className="admin-btn admin-btn-ghost" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  );
}
