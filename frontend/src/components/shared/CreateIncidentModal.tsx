'use client';

import { useState } from 'react';
import type { Severity } from '@/types/ironsight';

interface Props {
  onClose: () => void;
  prefill?: {
    site_id?: string;
    site_name?: string;
    camera_id?: string;
    camera_name?: string;
    severity?: Severity;
    type?: string;
    description?: string;
    alert_id?: string;
  };
}

const SEVERITY_OPTIONS: { value: Severity; label: string; color: string }[] = [
  { value: 'critical', label: 'Critical', color: '#EF4444' },
  { value: 'high', label: 'High', color: '#EF4444' },
  { value: 'medium', label: 'Medium', color: '#E89B2A' },
  { value: 'low', label: 'Low', color: '#E8732A' },
];

const TYPE_OPTIONS = [
  'no_hard_hat', 'no_harness', 'no_hi_vis', 'zone_breach',
  'vehicle_hazard', 'slip_fall', 'unauthorized_access', 'other',
];

export default function CreateIncidentModal({ onClose, prefill }: Props) {
  const [title, setTitle] = useState(prefill?.description?.slice(0, 80) || '');
  const [severity, setSeverity] = useState<Severity>(prefill?.severity || 'medium');
  const [type, setType] = useState(prefill?.type || 'other');
  const [notes, setNotes] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  const handleSubmit = async () => {
    if (!title.trim()) return;
    setSubmitting(true);
    // In production, this would POST to /api/v1/incidents
    await new Promise(r => setTimeout(r, 600));
    setSubmitting(false);
    setSubmitted(true);
    setTimeout(onClose, 1200);
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 10000,
      background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }} onClick={onClose}>
      <div
        style={{
          background: 'linear-gradient(180deg, #0E1117 0%, #080c10 100%)',
          border: '1px solid rgba(255,255,255,0.08)',
          borderRadius: 12, padding: 0, width: 460, maxHeight: '80vh',
          boxShadow: '0 16px 64px rgba(0,0,0,0.6)',
          animation: 'cam-fullscreen-enter 0.2s ease-out',
          overflow: 'hidden',
        }}
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div style={{
          padding: '16px 20px', borderBottom: '1px solid rgba(255,255,255,0.06)',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: '#E4E8F0' }}>
            📌 Create Incident
          </div>
          <button onClick={onClose} style={{
            background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
            borderRadius: 4, padding: '4px 8px', color: '#4A5268', cursor: 'pointer', fontSize: 11,
          }}>✕</button>
        </div>

        {submitted ? (
          <div style={{ padding: 40, textAlign: 'center' }}>
            <div style={{ fontSize: 40, marginBottom: 12 }}>✅</div>
            <div style={{ fontSize: 14, fontWeight: 600, color: '#22C55E' }}>Incident Created</div>
            <div style={{ fontSize: 11, color: '#4A5268', marginTop: 4 }}>
              Incident has been logged and assigned for review.
            </div>
          </div>
        ) : (
          <div style={{ padding: '16px 20px', overflowY: 'auto', maxHeight: 'calc(80vh - 130px)' }}>
            {/* Prefill context */}
            {prefill?.site_name && (
              <div style={{
                padding: '8px 12px', borderRadius: 4, marginBottom: 14,
                background: 'rgba(0,212,255,0.04)', border: '1px solid rgba(0,212,255,0.1)',
                display: 'flex', gap: 8, flexWrap: 'wrap', fontSize: 10, color: '#8891A5',
              }}>
                {prefill.site_name && <span>📍 {prefill.site_name}</span>}
                {prefill.camera_name && <span>📷 {prefill.camera_name}</span>}
                {prefill.alert_id && <span>🔗 Alert {prefill.alert_id}</span>}
              </div>
            )}

            {/* Title */}
            <div style={{ marginBottom: 14 }}>
              <label style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', letterSpacing: 0.5, display: 'block', marginBottom: 4 }}>
                Title *
              </label>
              <input
                value={title}
                onChange={e => setTitle(e.target.value)}
                placeholder="Brief description of the incident..."
                style={{
                  width: '100%', padding: '8px 12px', borderRadius: 4, fontSize: 12,
                  background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
                  color: '#E4E8F0', outline: 'none', fontFamily: 'inherit',
                  boxSizing: 'border-box',
                }}
              />
            </div>

            {/* Severity */}
            <div style={{ marginBottom: 14 }}>
              <label style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', letterSpacing: 0.5, display: 'block', marginBottom: 4 }}>
                Severity
              </label>
              <div style={{ display: 'flex', gap: 6 }}>
                {SEVERITY_OPTIONS.map(opt => (
                  <button
                    key={opt.value}
                    onClick={() => setSeverity(opt.value)}
                    style={{
                      flex: 1, padding: '6px 0', borderRadius: 4, fontSize: 10, fontWeight: 600,
                      background: severity === opt.value ? `${opt.color}15` : 'rgba(255,255,255,0.02)',
                      border: `1px solid ${severity === opt.value ? `${opt.color}40` : 'rgba(255,255,255,0.06)'}`,
                      color: severity === opt.value ? opt.color : '#4A5268',
                      cursor: 'pointer', fontFamily: 'inherit',
                    }}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Type */}
            <div style={{ marginBottom: 14 }}>
              <label style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', letterSpacing: 0.5, display: 'block', marginBottom: 4 }}>
                Incident Type
              </label>
              <select
                value={type}
                onChange={e => setType(e.target.value)}
                style={{
                  width: '100%', padding: '8px 12px', borderRadius: 4, fontSize: 12,
                  background: '#101820', border: '1px solid rgba(255,255,255,0.08)',
                  color: '#E4E8F0', outline: 'none', fontFamily: 'inherit',
                }}
              >
                {TYPE_OPTIONS.map(t => (
                  <option key={t} value={t}>{t.replace(/_/g, ' ')}</option>
                ))}
              </select>
            </div>

            {/* Notes */}
            <div style={{ marginBottom: 14 }}>
              <label style={{ fontSize: 10, fontWeight: 600, color: '#8891A5', letterSpacing: 0.5, display: 'block', marginBottom: 4 }}>
                Notes
              </label>
              <textarea
                value={notes}
                onChange={e => setNotes(e.target.value)}
                placeholder="Additional context, observations..."
                rows={3}
                style={{
                  width: '100%', padding: '8px 12px', borderRadius: 4, fontSize: 12,
                  background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
                  color: '#E4E8F0', outline: 'none', fontFamily: 'inherit', resize: 'vertical',
                  boxSizing: 'border-box',
                }}
              />
            </div>
          </div>
        )}

        {/* Footer */}
        {!submitted && (
          <div style={{
            padding: '12px 20px', borderTop: '1px solid rgba(255,255,255,0.06)',
            display: 'flex', gap: 8, justifyContent: 'flex-end',
          }}>
            <button onClick={onClose} style={{
              padding: '8px 16px', borderRadius: 4, fontSize: 11, fontWeight: 600,
              background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
              color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
            }}>Cancel</button>
            <button
              onClick={handleSubmit}
              disabled={!title.trim() || submitting}
              style={{
                padding: '8px 20px', borderRadius: 4, fontSize: 11, fontWeight: 700,
                background: 'rgba(0,212,255,0.1)', border: '1px solid rgba(0,212,255,0.3)',
                color: '#E8732A', cursor: title.trim() ? 'pointer' : 'not-allowed',
                fontFamily: 'inherit', opacity: title.trim() ? 1 : 0.5,
              }}
            >
              {submitting ? 'Creating...' : '📌 Create Incident'}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
