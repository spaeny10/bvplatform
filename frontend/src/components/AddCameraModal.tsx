'use client';

// Extracted from CameraManager.tsx (P1-B-11 session 17). The Add Camera
// modal and its follow-up sense-webhook overlay. The two overlays move
// together because the sense webhook is the post-create UX for cameras
// created via this dialog — it only ever appears once, right after a
// sense_pushed camera is created.
//
// State that moves with the JSX: addForm, addError, adding, senseSetup.
// Parent passes `onClose` (when the user cancels or successfully closes)
// and `onRefresh` (to re-fetch the camera list after a successful create).

import { useState } from 'react';
import { createCamera } from '@/lib/api';
import SenseWebhookFields from './SenseWebhookFields';

interface Props {
    onClose: () => void;
    onRefresh: () => void;
}

interface AddForm {
    name: string;
    onvif_address: string;
    username: string;
    password: string;
    device_class: 'continuous' | 'sense_pushed';
}

export default function AddCameraModal({ onClose, onRefresh }: Props) {
    const [addForm, setAddForm] = useState<AddForm>({
        name: '',
        onvif_address: '',
        username: 'admin',
        password: '',
        device_class: 'continuous',
    });
    const [addError, setAddError] = useState<string | null>(null);
    const [adding, setAdding] = useState(false);
    // Post-create webhook details for sense cameras. Shown in a follow-up
    // overlay so the operator can copy the URL into the camera's Alarm
    // Server config — there's no other way to retrieve the token after
    // dismissing this view (it's stored, but not echoed in list views).
    const [senseSetup, setSenseSetup] = useState<{ url: string; cameraName: string } | null>(null);

    const handleAdd = async () => {
        setAddError(null);
        setAdding(true);
        try {
            const created = await createCamera(addForm);
            // Close the add form; the sense-setup overlay (if any) renders
            // independently below.
            onClose();
            setAddForm({ name: '', onvif_address: '', username: 'admin', password: '', device_class: 'continuous' });
            // For sense_pushed cameras the create response carries the
            // freshly-minted webhook token. Build the absolute URL once
            // and present it; for continuous cameras nothing extra to show.
            if (created?.device_class === 'sense_pushed' && created?.sense_webhook_token) {
                const origin = typeof window !== 'undefined' ? window.location.origin : '';
                setSenseSetup({
                    url: `${origin}/api/integrations/milesight/sense/${created.sense_webhook_token}`,
                    cameraName: created.name,
                });
            }
            onRefresh();
        } catch (err: any) {
            setAddError(err?.message || 'Failed to add camera — check the address, credentials, and that ONVIF is enabled.');
        } finally {
            setAdding(false);
        }
    };

    return (
        <>
            <div className="modal-overlay" onClick={onClose}>
                <div className="modal" onClick={(e) => e.stopPropagation()}>
                    <div className="modal-title">Add Camera</div>

                    <div className="form-group">
                        <label className="form-label">Camera Type</label>
                        <select
                            className="form-input"
                            value={addForm.device_class}
                            onChange={(e) => setAddForm({ ...addForm, device_class: e.target.value as 'continuous' | 'sense_pushed' })}
                        >
                            <option value="continuous">Continuous (RTSP + ONVIF events)</option>
                            <option value="sense_pushed">Sense / push-only (Milesight SC4xx, PIR/solar)</option>
                        </select>
                        <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 4, lineHeight: 1.4 }}>
                            {addForm.device_class === 'sense_pushed'
                                ? 'Skips RTSP and ONVIF event subscription. Camera will POST to a webhook URL we issue after creation — paste it into the camera’s Alarm Server config.'
                                : 'Default for normal IP cameras. Pulls RTSP + subscribes to ONVIF events.'}
                        </div>
                    </div>

                    <div className="form-group">
                        <label className="form-label">Camera Name</label>
                        <input
                            className="form-input"
                            placeholder="e.g., Front Door"
                            value={addForm.name}
                            onChange={(e) => setAddForm({ ...addForm, name: e.target.value })}
                        />
                    </div>

                    <div className="form-group">
                        <label className="form-label">ONVIF Address</label>
                        <input
                            className="form-input"
                            placeholder="e.g., 192.168.1.100:80"
                            value={addForm.onvif_address}
                            onChange={(e) => setAddForm({ ...addForm, onvif_address: e.target.value })}
                        />
                    </div>

                    <div className="form-group">
                        <label className="form-label">Username</label>
                        <input
                            className="form-input"
                            placeholder="admin"
                            value={addForm.username}
                            onChange={(e) => setAddForm({ ...addForm, username: e.target.value })}
                        />
                    </div>

                    <div className="form-group">
                        <label className="form-label">Password</label>
                        <input
                            className="form-input"
                            type="password"
                            placeholder="••••••••"
                            value={addForm.password}
                            onChange={(e) => setAddForm({ ...addForm, password: e.target.value })}
                        />
                    </div>

                    {addError && (
                        <div style={{ padding: '8px 12px', borderRadius: 6, fontSize: 11, lineHeight: 1.5, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', color: '#EF4444', marginBottom: 8, wordBreak: 'break-word' }}>
                            {addError}
                        </div>
                    )}

                    <div className="modal-actions">
                        <button className="btn" onClick={() => { onClose(); setAddError(null); }}>Cancel</button>
                        <button className="btn btn-primary" onClick={handleAdd} disabled={adding || !addForm.onvif_address.trim()}>
                            {adding ? 'Connecting...' : 'Add Camera'}
                        </button>
                    </div>
                </div>
            </div>

            {/* Sense webhook setup — shown once after creating a sense_pushed camera. */}
            {senseSetup && (
                <div className="modal-overlay" onClick={() => setSenseSetup(null)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 720 }}>
                        <div className="modal-title">Sense camera webhook</div>
                        <div style={{ padding: 12, fontSize: 13, color: 'var(--text-primary)', lineHeight: 1.6 }}>
                            <p style={{ marginTop: 0 }}>
                                <strong>{senseSetup.cameraName}</strong> is set up as a push-only device.
                                In the camera UI go to <em>Event → Alarm Settings → Alarm Server → Add</em>
                                and copy each value into its matching field:
                            </p>
                            <SenseWebhookFields url={senseSetup.url} />
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 12, lineHeight: 1.5 }}>
                                Leave User Name / Password blank — the long path token is the auth.
                                You can review these values later from the camera's Settings → General tab.
                            </div>
                        </div>
                        <div className="modal-actions">
                            <button className="btn btn-primary" onClick={() => setSenseSetup(null)}>Done</button>
                        </div>
                    </div>
                </div>
            )}
        </>
    );
}
