'use client';

import { useState } from 'react';
import {
  useMasterCameras, useMasterSpeakers,
  useAssignCamera, useUnassignCamera,
  useAssignSpeaker, useUnassignSpeaker,
} from '@/hooks/useCameraAssignment';
import type { IRONSightCamera, PlatformSpeaker } from '@/types/ironsight';
import VCAZoneEditor from '@/components/VCAZoneEditor';

interface Props {
  siteId: string;
  onClose: () => void;
}

type DeviceTab = 'cameras' | 'speakers';

// ─── Camera row ────────────────────────────────────────────────

export function CameraSection({ siteId }: { siteId: string }) {
  const { data: allCameras = [], isLoading } = useMasterCameras();
  const assignCamera = useAssignCamera();
  const unassignCamera = useUnassignCamera();

  const [locationLabels, setLocationLabels] = useState<Record<string, string>>({});
  const [vcaCameraId, setVcaCameraId] = useState<string | null>(null);
  const [testingCam, setTestingCam] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; msg: string }>>({});

  const handleTestCamera = async (camId: string) => {
    setTestingCam(camId);
    try {
      const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : '';
      const res = await fetch(`/api/cameras/${camId}/vca/snapshot`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (res.ok) {
        setTestResults(prev => ({ ...prev, [camId]: { ok: true, msg: 'Stream reachable' } }));
      } else {
        const text = await res.text();
        setTestResults(prev => ({ ...prev, [camId]: { ok: false, msg: text.slice(0, 60) || `HTTP ${res.status}` } }));
      }
    } catch (err: any) {
      setTestResults(prev => ({ ...prev, [camId]: { ok: false, msg: err?.message || 'Connection failed' } }));
    } finally {
      setTestingCam(null);
    }
  };

  const assignedHere = allCameras.filter((c: IRONSightCamera) => c.site_id === siteId);
  const unassigned    = allCameras.filter((c: IRONSightCamera) => !c.site_id);
  const elsewhere     = allCameras.filter((c: IRONSightCamera) => c.site_id && c.site_id !== siteId);

  const handleAssign = async (cameraId: string) => {
    await assignCamera.mutateAsync({ siteId, cameraId, locationLabel: locationLabels[cameraId] || '' });
  };

  const handleUnassign = async (cameraId: string) => {
    await unassignCamera.mutateAsync({ siteId, cameraId });
  };

  // Move: unassign from current site, then assign here
  const handleMove = async (cam: IRONSightCamera) => {
    await unassignCamera.mutateAsync({ siteId: cam.site_id!, cameraId: cam.id });
    await assignCamera.mutateAsync({ siteId, cameraId: cam.id, locationLabel: locationLabels[cam.id] || '' });
  };


  if (isLoading) return <div style={{ padding: 40, textAlign: 'center', color: '#4A5268' }}>Loading cameras...</div>;

  return (
    <>
      {/* Info bar — cameras come from NVR Settings */}
      <div style={{ padding: '10px 16px', borderBottom: '1px solid rgba(255,255,255,0.04)', display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{ fontSize: 11, color: '#4A5268' }}>
          Showing cameras from NVR master list. Add new cameras via <strong style={{ color: '#8891A5' }}>Admin → NVR Settings</strong>.
        </span>
      </div>

      {/* Assigned here */}
      {assignedHere.length > 0 && (
        <>
          <SectionHeader label="Assigned to This Site" count={assignedHere.length} color="#22C55E" />
          {assignedHere.map((cam: IRONSightCamera) => {
            const mfg = (cam.manufacturer || '').toLowerCase();
            const model = (cam.model || '').toLowerCase();
            const isMilesight = mfg.includes('milesight') || model.startsWith('ms-c') || model.startsWith('ms-n');
            const vcaOpen = vcaCameraId === cam.id;

            return (
              <div key={cam.id}>
                <div className="admin-camera-item">
                  <div className="admin-camera-info">
                    <div className="admin-camera-name" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      {cam.location || cam.name}
                      {isMilesight && (
                        <span style={{ fontSize: 8, fontWeight: 700, padding: '1px 5px', borderRadius: 3, background: 'rgba(59,130,246,0.12)', color: '#3B82F6', border: '1px solid rgba(59,130,246,0.25)' }}>
                          MILESIGHT
                        </span>
                      )}
                    </div>
                    <div className="admin-camera-meta">
                      {cam.name}{cam.onvif_address ? ` · ${cam.onvif_address}` : ''}{cam.manufacturer ? ` · ${cam.manufacturer} ${cam.model}` : ''}
                    </div>
                  </div>
                  <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
                    {isMilesight && (
                      <button
                        type="button"
                        className="admin-btn admin-btn-ghost"
                        onClick={e => { e.stopPropagation(); setVcaCameraId(vcaOpen ? null : cam.id); }}
                        style={{
                          padding: '3px 10px', fontSize: 10,
                          color: vcaOpen ? '#3B82F6' : undefined,
                          borderColor: vcaOpen ? 'rgba(59,130,246,0.4)' : undefined,
                          background: vcaOpen ? 'rgba(59,130,246,0.08)' : undefined,
                        }}
                      >
                        🚧 {vcaOpen ? 'Close Zones' : 'Configure Zones'}
                      </button>
                    )}
                    <span className="admin-camera-status online">assigned</span>
                    <button className="admin-btn admin-btn-ghost" onClick={() => handleUnassign(cam.id)} style={{ padding: '3px 10px', fontSize: 10 }}>
                      Unassign
                    </button>
                  </div>
                </div>

                {/* Inline VCA Zone Editor */}
                {vcaOpen && (
                  <div
                    onClick={e => e.stopPropagation()}
                    onMouseDown={e => e.stopPropagation()}
                    style={{
                      padding: '12px 16px', borderBottom: '1px solid rgba(255,255,255,0.04)',
                      background: 'rgba(59,130,246,0.02)',
                    }}
                  >
                    <VCAZoneEditor cameraId={cam.id} cameraIp={cam.onvif_address?.replace(/^https?:\/\//, '').replace(/\/.*/, '') || undefined} />
                  </div>
                )}
              </div>
            );
          })}
        </>
      )}

      {/* Unassigned pool */}
      <SectionHeader label="Available — Unassigned" count={unassigned.length} color="#E8732A" />
      {unassigned.length === 0 && (
        <div style={{ padding: '12px 16px', color: '#4A5268', fontSize: 11 }}>
          No unassigned cameras. Register a new one above.
        </div>
      )}
      {unassigned.map((cam: IRONSightCamera) => {
        const test = testResults[cam.id];
        return (
          <div key={cam.id} className="admin-camera-item" style={{ flexWrap: 'wrap' }}>
            <div className="admin-camera-info">
              <div className="admin-camera-name" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                {cam.name}
                {test && (
                  <span style={{
                    fontSize: 8, fontWeight: 700, padding: '1px 5px', borderRadius: 3,
                    background: test.ok ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)',
                    color: test.ok ? '#22C55E' : '#EF4444',
                    border: `1px solid ${test.ok ? 'rgba(34,197,94,0.25)' : 'rgba(239,68,68,0.25)'}`,
                  }}>
                    {test.ok ? '✓ ONLINE' : '✕ ' + test.msg}
                  </span>
                )}
              </div>
              <div className="admin-camera-meta">
                {cam.id.slice(0, 12)}{cam.onvif_address ? ` · ${cam.onvif_address}` : ''}{cam.manufacturer ? ` · ${cam.manufacturer} ${cam.model}` : ''}
              </div>
              <input
                className="admin-input"
                style={{ marginTop: 6, fontSize: 11, padding: '4px 8px' }}
                placeholder="Location label at this site (e.g. North Perimeter)"
                value={locationLabels[cam.id] || ''}
                onChange={e => setLocationLabels(p => ({ ...p, [cam.id]: e.target.value }))}
                onClick={e => e.stopPropagation()}
              />
            </div>
            <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
              <button
                type="button"
                className="admin-btn admin-btn-ghost"
                onClick={() => handleTestCamera(cam.id)}
                disabled={testingCam === cam.id}
                style={{ padding: '4px 10px', fontSize: 10 }}
              >
                {testingCam === cam.id ? 'Testing...' : 'Test'}
              </button>
              <button className="admin-btn admin-btn-primary" onClick={() => handleAssign(cam.id)} style={{ padding: '4px 12px', fontSize: 10 }}>
                Assign
              </button>
            </div>
          </div>
        );
      })}

      {/* Cameras on other sites — can be moved */}
      {elsewhere.length > 0 && (
        <>
          <SectionHeader label="At Other Sites — can move here" count={elsewhere.length} color="#4A5268" />
          {elsewhere.map((cam: IRONSightCamera) => (
            <div key={cam.id} className="admin-camera-item" style={{ opacity: 0.7 }}>
              <div className="admin-camera-info">
                <div className="admin-camera-name">{cam.name}</div>
                <div className="admin-camera-meta">
                  {cam.location ? `"${cam.location}" at ` : ''}{cam.site_id}
                  {cam.manufacturer ? ` · ${cam.manufacturer} ${cam.model}` : ''}
                </div>
                <input
                  className="admin-input"
                  style={{ marginTop: 6, fontSize: 11, padding: '4px 8px' }}
                  placeholder="New location label here"
                  value={locationLabels[cam.id] || ''}
                  onChange={e => setLocationLabels(p => ({ ...p, [cam.id]: e.target.value }))}
                  onClick={e => e.stopPropagation()}
                />
              </div>
              <button
                className="admin-btn admin-btn-ghost"
                onClick={() => handleMove(cam)}
                style={{ padding: '3px 10px', fontSize: 10, color: 'var(--accent-orange)', borderColor: 'rgba(232,115,42,0.3)' }}
              >
                Move Here
              </button>
            </div>
          ))}
        </>
      )}
    </>
  );
}

// ─── Speaker row ───────────────────────────────────────────────

export function SpeakerSection({ siteId }: { siteId: string }) {
  const { data: allSpeakers = [], isLoading } = useMasterSpeakers();
  const assignSpeaker = useAssignSpeaker();
  const unassignSpeaker = useUnassignSpeaker();

  const [locationLabels, setLocationLabels] = useState<Record<string, string>>({});

  const assignedHere = allSpeakers.filter((s: PlatformSpeaker) => s.site_id === siteId);
  const unassigned    = allSpeakers.filter((s: PlatformSpeaker) => !s.site_id);
  const elsewhere     = allSpeakers.filter((s: PlatformSpeaker) => s.site_id && s.site_id !== siteId);

  const handleAssign = async (speakerId: string) => {
    await assignSpeaker.mutateAsync({ siteId, speakerId, locationLabel: locationLabels[speakerId] || '' });
  };

  const handleUnassign = async (speakerId: string) => {
    await unassignSpeaker.mutateAsync({ siteId, speakerId });
  };

  const handleMove = async (spk: PlatformSpeaker) => {
    await unassignSpeaker.mutateAsync({ siteId: spk.site_id!, speakerId: spk.id });
    await assignSpeaker.mutateAsync({ siteId, speakerId: spk.id, locationLabel: locationLabels[spk.id] || '' });
  };

  if (isLoading) return <div style={{ padding: 40, textAlign: 'center', color: '#4A5268' }}>Loading speakers...</div>;

  if (allSpeakers.length === 0) {
    return (
      <div style={{ padding: 32, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
        No speakers registered. Add speakers via Settings → Speakers.
      </div>
    );
  }

  return (
    <>
      {assignedHere.length > 0 && (
        <>
          <SectionHeader label="Assigned to This Site" count={assignedHere.length} color="#22C55E" />
          {assignedHere.map((spk: PlatformSpeaker) => (
            <div key={spk.id} className="admin-camera-item">
              <div className="admin-camera-info">
                <div className="admin-camera-name">{spk.location || spk.name}</div>
                <div className="admin-camera-meta">
                  {spk.name}{spk.onvif_address ? ` · ${spk.onvif_address}` : ''}{spk.manufacturer ? ` · ${spk.manufacturer} ${spk.model}` : ''}
                </div>
              </div>
              <span className="admin-camera-status online">assigned</span>
              <button className="admin-btn admin-btn-ghost" onClick={() => handleUnassign(spk.id)} style={{ padding: '3px 10px', fontSize: 10 }}>
                Unassign
              </button>
            </div>
          ))}
        </>
      )}

      <SectionHeader label="Available — Unassigned" count={unassigned.length} color="#E8732A" />
      {unassigned.length === 0 && (
        <div style={{ padding: '12px 16px', color: '#4A5268', fontSize: 11 }}>All speakers are assigned to sites.</div>
      )}
      {unassigned.map((spk: PlatformSpeaker) => (
        <div key={spk.id} className="admin-camera-item">
          <div className="admin-camera-info">
            <div className="admin-camera-name">{spk.name}</div>
            <div className="admin-camera-meta">
              {spk.zone ? `Zone: ${spk.zone} · ` : ''}{spk.onvif_address}{spk.manufacturer ? ` · ${spk.manufacturer} ${spk.model}` : ''}
            </div>
            <input
              className="admin-input"
              style={{ marginTop: 6, fontSize: 11, padding: '4px 8px' }}
              placeholder="Location label at this site (e.g. Front Gate Speaker)"
              value={locationLabels[spk.id] || ''}
              onChange={e => setLocationLabels(p => ({ ...p, [spk.id]: e.target.value }))}
              onClick={e => e.stopPropagation()}
            />
          </div>
          <button className="admin-btn admin-btn-primary" onClick={() => handleAssign(spk.id)} style={{ padding: '4px 12px', fontSize: 10 }}>
            Assign
          </button>
        </div>
      ))}

      {elsewhere.length > 0 && (
        <>
          <SectionHeader label="At Other Sites — can move here" count={elsewhere.length} color="#4A5268" />
          {elsewhere.map((spk: PlatformSpeaker) => (
            <div key={spk.id} className="admin-camera-item" style={{ opacity: 0.7 }}>
              <div className="admin-camera-info">
                <div className="admin-camera-name">{spk.name}</div>
                <div className="admin-camera-meta">
                  {spk.location ? `"${spk.location}" at ` : ''}{spk.site_id}
                </div>
                <input
                  className="admin-input"
                  style={{ marginTop: 6, fontSize: 11, padding: '4px 8px' }}
                  placeholder="New location label here"
                  value={locationLabels[spk.id] || ''}
                  onChange={e => setLocationLabels(p => ({ ...p, [spk.id]: e.target.value }))}
                  onClick={e => e.stopPropagation()}
                />
              </div>
              <button
                className="admin-btn admin-btn-ghost"
                onClick={() => handleMove(spk)}
                style={{ padding: '3px 10px', fontSize: 10, color: 'var(--accent-orange)', borderColor: 'rgba(232,115,42,0.3)' }}
              >
                Move Here
              </button>
            </div>
          ))}
        </>
      )}
    </>
  );
}

// ─── Shared section header ──────────────────────────────────────

function SectionHeader({ label, count, color }: { label: string; count: number; color: string }) {
  return (
    <div style={{ padding: '10px 16px 5px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color }}>
      {label} ({count})
    </div>
  );
}

// ─── Modal shell ────────────────────────────────────────────────

export default function AssignCameraModal({ siteId, onClose }: Props) {
  const [tab, setTab] = useState<DeviceTab>('cameras');
  const { data: allCameras = [] } = useMasterCameras();
  const { data: allSpeakers = [] } = useMasterSpeakers();

  const camsHere = allCameras.filter((c: IRONSightCamera) => c.site_id === siteId).length;
  const spksHere = allSpeakers.filter((s: PlatformSpeaker) => s.site_id === siteId).length;

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal wide" onClick={e => e.stopPropagation()}>
        {/* Header */}
        <div className="admin-modal-header">
          <div>
            <div className="admin-modal-title">Device Management</div>
            <div style={{ fontSize: 11, color: '#4A5268', marginTop: 2 }}>
              Site: {siteId} · {camsHere} camera{camsHere !== 1 ? 's' : ''} · {spksHere} speaker{spksHere !== 1 ? 's' : ''}
            </div>
          </div>
          <button className="admin-modal-close" onClick={onClose}>x</button>
        </div>

        {/* Tabs */}
        <div style={{ display: 'flex', borderBottom: '1px solid rgba(255,255,255,0.06)', padding: '0 16px' }}>
          {(['cameras', 'speakers'] as DeviceTab[]).map(t => (
            <button
              key={t}
              onClick={() => setTab(t)}
              style={{
                background: 'none', border: 'none', cursor: 'pointer',
                padding: '10px 16px', fontSize: 11, fontWeight: 600,
                color: tab === t ? 'var(--accent-orange)' : '#4A5268',
                borderBottom: tab === t ? '2px solid var(--accent-orange)' : '2px solid transparent',
                marginBottom: -1, textTransform: 'capitalize',
              }}
            >
              {t === 'cameras' ? `📷 Cameras` : `🔊 Speakers`}
            </button>
          ))}
        </div>

        {/* Body */}
        <div style={{ maxHeight: '60vh', overflowY: 'auto' }}>
          {tab === 'cameras' ? <CameraSection siteId={siteId} /> : <SpeakerSection siteId={siteId} />}
        </div>

        {/* Footer */}
        <div className="admin-modal-footer">
          <div style={{ fontSize: 10, color: '#4A5268' }}>
            Location labels are the site-scoped names customers see — the device identity stays private.
          </div>
          <button className="admin-btn admin-btn-ghost" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  );
}
