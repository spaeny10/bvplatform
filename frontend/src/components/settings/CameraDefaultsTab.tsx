'use client';

// Extracted from SettingsPage.tsx (P1-B-11 session 8). Camera Defaults
// tab: per-new-camera retention / recording-mode / segment-duration
// defaults. Reads + writes happen via the same `settingsDraft` state
// machine the SystemTab uses, so this stays prop-driven (parent owns
// the draft, this child only renders the form).

import type { SystemSettings } from '@/lib/api';

interface Props {
    isAdmin: boolean;
    settingsDraft: SystemSettings | null;
    settingsSaving: boolean;
    settingsMsg: { ok: boolean; text: string } | null;
    patchDraft: (key: keyof SystemSettings, val: string | number) => void;
    handleSaveSettings: () => void;
}

export default function CameraDefaultsTab({
    isAdmin, settingsDraft, settingsSaving, settingsMsg,
    patchDraft, handleSaveSettings,
}: Props) {
    return (
        <div className="settings-section" role="tabpanel">
            <div className="settings-section-title">Camera Defaults</div>
            <p className="settings-section-desc">
                These values are applied automatically when a new camera is added.
                They do not retroactively change existing cameras.
            </p>

            {settingsDraft && (
                <div className="settings-form">
                    <div className="settings-form-row">
                        <label className="settings-label">
                            <span className="settings-label-text">Default Retention</span>
                            <span className="settings-label-hint">Days before recordings are deleted</span>
                        </label>
                        <div className="settings-input-with-unit">
                            <input
                                className="settings-input"
                                type="number"
                                min={1}
                                max={365}
                                value={settingsDraft.default_retention_days}
                                onChange={e => patchDraft('default_retention_days', parseInt(e.target.value) || 30)}
                                disabled={!isAdmin}
                            />
                            <span className="settings-input-unit">days</span>
                        </div>
                    </div>

                    <div className="settings-form-row">
                        <label className="settings-label">
                            <span className="settings-label-text">Default Recording Mode</span>
                            <span className="settings-label-hint">How new cameras record by default</span>
                        </label>
                        <select
                            className="settings-input settings-select"
                            value={settingsDraft.default_recording_mode}
                            onChange={e => patchDraft('default_recording_mode', e.target.value)}
                            disabled={!isAdmin}
                        >
                            <option value="continuous">Continuous</option>
                            <option value="event">Event-triggered</option>
                        </select>
                    </div>

                    <div className="settings-form-row">
                        <label className="settings-label">
                            <span className="settings-label-text">Segment Duration</span>
                            <span className="settings-label-hint">Length of each recording file</span>
                        </label>
                        <div className="settings-input-with-unit">
                            <input
                                className="settings-input"
                                type="number"
                                min={10}
                                max={3600}
                                value={settingsDraft.default_segment_duration}
                                onChange={e => patchDraft('default_segment_duration', parseInt(e.target.value) || 60)}
                                disabled={!isAdmin}
                            />
                            <span className="settings-input-unit">seconds</span>
                        </div>
                    </div>
                </div>
            )}

            {isAdmin && (
                <div className="settings-actions">
                    <button className="settings-save-btn" onClick={handleSaveSettings} disabled={settingsSaving}>
                        {settingsSaving ? '⏳ Saving…' : '💾 Save Defaults'}
                    </button>
                    {settingsMsg && (
                        <span className={`settings-msg ${settingsMsg.ok ? 'ok' : 'err'}`}>{settingsMsg.text}</span>
                    )}
                </div>
            )}
        </div>
    );
}
