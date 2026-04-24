'use client';

import { useState } from 'react';
import { CameraSection, SpeakerSection } from './AssignCameraModal';
import CreateSiteModal from './CreateSiteModal';
import SiteSOPModal from './SiteSOPModal';
import SiteMapModal from './SiteMapModal';
import NotificationRulesModal from './NotificationRulesModal';
import SiteScheduleModal from './SiteScheduleModal';
import UserAssignmentModal from './CustomerAccessModal';
import SiteRecordingPanel from './SiteRecordingPanel';

type ConfigTab = 'details' | 'cameras' | 'speakers' | 'recording' | 'sops' | 'users' | 'schedule' | 'notifications' | 'map' | 'danger';

interface NavItem {
  key: ConfigTab;
  label: string;
  section: string;
  desc: string;
}

const NAV: NavItem[] = [
  { key: 'details',       label: 'Site Details',         section: 'General',    desc: 'Name, address, coordinates, monitoring tier, and company assignment.' },
  { key: 'cameras',       label: 'Cameras & Devices',   section: 'Devices',    desc: 'Assign NVR cameras to this site. Add new cameras via Admin → NVR Settings.' },
  { key: 'speakers',      label: 'Speakers',             section: 'Devices',    desc: 'Assign speakers for talk-down capability at this site.' },
  { key: 'recording',     label: 'Recording & Retention', section: 'Devices',   desc: 'Recording mode, buffers, triggers and retention for every camera on this site.' },
  { key: 'sops',          label: 'Standard Procedures',  section: 'Operations', desc: 'Standard operating procedures for SOC operators monitoring this site.' },
  { key: 'schedule',      label: 'Monitoring Schedule',  section: 'Operations', desc: 'Define when the SOC actively monitors this site.' },
  { key: 'notifications', label: 'Alert Rules',          section: 'Operations', desc: 'Configure alert routing — who gets notified and how.' },
  { key: 'users',         label: 'User Access',          section: 'Access',     desc: 'Control which customer users can view this site in the portal.' },
  { key: 'map',           label: 'Site Map',             section: 'Access',     desc: 'Upload or configure the site floor plan with camera positions.' },
  { key: 'danger',        label: 'Archive & Delete',     section: 'Danger',     desc: 'Archive this site to disable monitoring, or permanently delete all data.' },
];

function apiFetch(url: string, opts: RequestInit = {}) {
  const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : '';
  return fetch(url, {
    ...opts,
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...opts.headers },
  });
}

interface Props {
  siteId: string;
  siteName: string;
  initialTab?: ConfigTab;
  onClose: () => void;
  onDeleted?: () => void;
}

export default function SiteConfigModal({ siteId, siteName, initialTab = 'details', onClose, onDeleted }: Props) {
  const [tab, setTab] = useState<ConfigTab>(initialTab);

  // Group nav items by section
  const sections: { label: string; items: NavItem[] }[] = [];
  for (const item of NAV) {
    let section = sections.find(s => s.label === item.section);
    if (!section) { section = { label: item.section, items: [] }; sections.push(section); }
    section.items.push(item);
  }

  const noop = () => {};
  const currentNav = NAV.find(n => n.key === tab);
  const isDanger = tab === 'danger';

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div
        onClick={e => e.stopPropagation()}
        style={{
          background: '#0E1117',
          border: '1px solid rgba(255,255,255,0.08)',
          borderRadius: 12,
          width: '90vw', maxWidth: 900,
          height: '80vh', maxHeight: 700,
          display: 'grid',
          gridTemplateColumns: '200px 1fr',
          overflow: 'hidden',
          boxShadow: '0 24px 64px rgba(0,0,0,0.6)',
        }}
      >
        {/* ── Left sidebar ── */}
        <div style={{
          background: '#0A0C10',
          borderRight: '1px solid rgba(255,255,255,0.06)',
          display: 'flex', flexDirection: 'column',
          padding: '20px 0',
          overflow: 'hidden',
        }}>
          <div style={{ padding: '0 16px 16px', borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
            <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268', marginBottom: 4 }}>
              Site Manager
            </div>
            <div style={{ fontSize: 14, fontWeight: 700, color: '#E4E8F0', lineHeight: 1.3 }}>
              {siteName}
            </div>
            <div style={{ fontSize: 10, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", marginTop: 4 }}>
              {siteId}
            </div>
          </div>

          <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
            {sections.map(section => (
              <div key={section.label}>
                <div style={{
                  fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase',
                  color: section.label === 'Danger' ? 'rgba(239,68,68,0.5)' : '#4A5268',
                  padding: '12px 16px 4px',
                }}>
                  {section.label}
                </div>
                {section.items.map(item => {
                  const isActive = tab === item.key;
                  const isDangerItem = item.section === 'Danger';
                  return (
                    <button
                      key={item.key}
                      type="button"
                      onClick={() => setTab(item.key)}
                      style={{
                        display: 'block', width: '100%', textAlign: 'left',
                        padding: '8px 16px', fontSize: 12, fontWeight: 500,
                        background: isActive
                          ? isDangerItem ? 'rgba(239,68,68,0.08)' : 'rgba(232,115,42,0.08)'
                          : 'transparent',
                        color: isActive
                          ? isDangerItem ? '#EF4444' : '#E8732A'
                          : isDangerItem ? 'rgba(239,68,68,0.5)' : '#8891A5',
                        border: 'none', cursor: 'pointer', fontFamily: 'inherit',
                        borderLeft: isActive
                          ? `2px solid ${isDangerItem ? '#EF4444' : '#E8732A'}`
                          : '2px solid transparent',
                        transition: 'all 0.1s',
                      }}
                    >
                      {item.label}
                    </button>
                  );
                })}
              </div>
            ))}
          </div>

          <div style={{ padding: '12px 16px', borderTop: '1px solid rgba(255,255,255,0.06)' }}>
            <button
              type="button"
              onClick={onClose}
              style={{
                width: '100%', padding: '8px 0', borderRadius: 6,
                fontSize: 11, fontWeight: 600,
                background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
                color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
              }}
            >
              Done
            </button>
          </div>
        </div>

        {/* ── Right content ── */}
        <div style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{
            padding: '16px 24px 14px',
            borderBottom: `1px solid ${isDanger ? 'rgba(239,68,68,0.15)' : 'rgba(255,255,255,0.06)'}`,
            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            flexShrink: 0,
            background: isDanger ? 'rgba(239,68,68,0.03)' : 'transparent',
          }}>
            <div>
              <div style={{ fontSize: 15, fontWeight: 600, color: isDanger ? '#EF4444' : '#E4E8F0' }}>
                {currentNav?.label}
              </div>
              <div style={{ fontSize: 11, color: isDanger ? 'rgba(239,68,68,0.6)' : '#4A5268', marginTop: 2 }}>
                {currentNav?.desc}
              </div>
            </div>
            <button
              type="button"
              onClick={onClose}
              style={{
                background: 'none', border: 'none', cursor: 'pointer',
                color: '#4A5268', fontSize: 18, padding: '4px 8px', lineHeight: 1,
              }}
            >
              ✕
            </button>
          </div>

          <div style={{ flex: 1, overflowY: 'auto' }}>
            {tab === 'details' && <CreateSiteModal editSiteId={siteId} onClose={noop} embedded />}
            {tab === 'cameras' && <CameraSection siteId={siteId} />}
            {tab === 'speakers' && <SpeakerSection siteId={siteId} />}
            {tab === 'recording' && <SiteRecordingPanel siteId={siteId} siteName={siteName} />}
            {tab === 'sops' && <SiteSOPModal siteId={siteId} onClose={noop} embedded />}
            {tab === 'schedule' && <SiteScheduleModal siteId={siteId} onClose={noop} embedded />}
            {tab === 'notifications' && <NotificationRulesModal siteId={siteId} onClose={noop} embedded />}
            {tab === 'users' && <UserAssignmentModal siteId={siteId} onClose={noop} embedded />}
            {tab === 'map' && <SiteMapModal siteId={siteId} onClose={noop} embedded />}
            {tab === 'danger' && (
              <DangerZone siteId={siteId} siteName={siteName} onArchived={onClose} onDeleted={() => { onDeleted?.(); onClose(); }} />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════
// Danger Zone — Archive & Delete
// ═══════════════════════════════════════════════════════════════

function DangerZone({ siteId, siteName, onArchived, onDeleted }: {
  siteId: string;
  siteName: string;
  onArchived: () => void;
  onDeleted: () => void;
}) {
  const [archiving, setArchiving] = useState(false);
  const [archived, setArchived] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState('');

  const handleArchive = async () => {
    setArchiving(true);
    setError('');
    try {
      await apiFetch(`/api/v1/sites/${siteId}`, {
        method: 'PUT',
        body: JSON.stringify({ status: 'archived' }),
      });
      setArchived(true);
      setTimeout(onArchived, 1500);
    } catch (err: any) {
      setError(err?.message || 'Failed to archive site');
    } finally {
      setArchiving(false);
    }
  };

  const handleDelete = async () => {
    if (deleteConfirm !== 'delete') return;
    setDeleting(true);
    setError('');
    try {
      await apiFetch(`/api/v1/sites/${siteId}`, { method: 'DELETE' });
      onDeleted();
    } catch (err: any) {
      setError(err?.message || 'Failed to delete site');
      setDeleting(false);
    }
  };

  return (
    <div style={{ padding: '24px' }}>
      {error && (
        <div style={{ padding: '10px 14px', borderRadius: 6, marginBottom: 16, fontSize: 11, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', color: '#EF4444' }}>
          {error}
        </div>
      )}

      {/* ── Archive ── */}
      <div style={{
        padding: '20px', borderRadius: 8, marginBottom: 24,
        background: 'rgba(232,155,42,0.04)',
        border: '1px solid rgba(232,155,42,0.2)',
      }}>
        <div style={{ fontSize: 14, fontWeight: 600, color: '#E89B2A', marginBottom: 6 }}>
          Archive Site
        </div>
        <div style={{ fontSize: 12, color: '#8891A5', lineHeight: 1.6, marginBottom: 16 }}>
          Archiving will:
        </div>
        <ul style={{ fontSize: 12, color: '#8891A5', lineHeight: 1.8, margin: '0 0 16px 16px', padding: 0 }}>
          <li>Stop SOC monitoring for this site</li>
          <li>Lock customer portal access (read-only)</li>
          <li>Remove metrics from dashboards and reports</li>
          <li>Preserve all data — recordings, events, SOPs, configurations</li>
          <li>Allow full reactivation at any time</li>
        </ul>

        {archived ? (
          <div style={{ fontSize: 12, fontWeight: 600, color: '#22C55E' }}>
            ✓ Site archived successfully. Closing...
          </div>
        ) : (
          <button
            type="button"
            onClick={handleArchive}
            disabled={archiving}
            style={{
              padding: '8px 20px', borderRadius: 6, fontSize: 12, fontWeight: 600,
              background: 'rgba(232,155,42,0.1)', border: '1px solid rgba(232,155,42,0.4)',
              color: '#E89B2A', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            {archiving ? 'Archiving...' : 'Archive This Site'}
          </button>
        )}
      </div>

      {/* ── Delete ── */}
      <div style={{
        padding: '20px', borderRadius: 8,
        background: 'rgba(239,68,68,0.04)',
        border: '1px solid rgba(239,68,68,0.2)',
      }}>
        <div style={{ fontSize: 14, fontWeight: 600, color: '#EF4444', marginBottom: 6 }}>
          Permanently Delete Site
        </div>
        <div style={{ fontSize: 12, color: '#8891A5', lineHeight: 1.6, marginBottom: 16 }}>
          This will <strong style={{ color: '#EF4444' }}>permanently destroy</strong> all data for <strong style={{ color: '#E4E8F0' }}>{siteName}</strong>:
        </div>
        <ul style={{ fontSize: 12, color: '#8891A5', lineHeight: 1.8, margin: '0 0 16px 16px', padding: 0 }}>
          <li>All camera assignments and recordings</li>
          <li>All SOPs, schedules, and alert rules</li>
          <li>All security events, incidents, and audit logs</li>
          <li>All user access assignments</li>
          <li style={{ color: '#EF4444', fontWeight: 600 }}>This cannot be undone</li>
        </ul>

        <div style={{ marginBottom: 12 }}>
          <div style={{ fontSize: 11, color: '#8891A5', marginBottom: 6 }}>
            Type <strong style={{ color: '#EF4444', fontFamily: "'JetBrains Mono', monospace" }}>delete</strong> to confirm:
          </div>
          <input
            type="text"
            value={deleteConfirm}
            onChange={e => setDeleteConfirm(e.target.value.toLowerCase())}
            placeholder="delete"
            spellCheck={false}
            autoComplete="off"
            style={{
              padding: '8px 12px', borderRadius: 6, fontSize: 13, width: 200,
              fontFamily: "'JetBrains Mono', monospace",
              background: 'rgba(239,68,68,0.06)',
              border: `1px solid ${deleteConfirm === 'delete' ? 'rgba(239,68,68,0.5)' : 'rgba(239,68,68,0.15)'}`,
              color: deleteConfirm === 'delete' ? '#EF4444' : '#8891A5',
              outline: 'none',
            }}
          />
        </div>

        <button
          type="button"
          onClick={handleDelete}
          disabled={deleteConfirm !== 'delete' || deleting}
          style={{
            padding: '8px 20px', borderRadius: 6, fontSize: 12, fontWeight: 700,
            background: deleteConfirm === 'delete' ? 'rgba(239,68,68,0.15)' : 'rgba(255,255,255,0.02)',
            border: `1px solid ${deleteConfirm === 'delete' ? 'rgba(239,68,68,0.5)' : 'rgba(255,255,255,0.06)'}`,
            color: deleteConfirm === 'delete' ? '#EF4444' : '#4A5268',
            cursor: deleteConfirm === 'delete' ? 'pointer' : 'not-allowed',
            fontFamily: 'inherit',
          }}
        >
          {deleting ? 'Deleting...' : 'Permanently Delete Site'}
        </button>
      </div>
    </div>
  );
}
