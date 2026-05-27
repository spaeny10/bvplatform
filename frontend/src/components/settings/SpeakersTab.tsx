'use client';

// Extracted from SettingsPage.tsx (P1-B-11 session 7). Speaker devices
// and pre-recorded audio messages — the only consumer of all this state
// was the speakers tab itself, so the 8 useState slots that lived on
// the parent come along to this child. The parent now passes only
// `isAdmin`.

import { useEffect, useState } from 'react';
import {
    Speaker, AudioMessage,
    listSpeakers, createSpeaker, deleteSpeaker,
    listAudioMessages, uploadAudioMessage, deleteAudioMessage,
} from '@/lib/api';

interface Props {
    isAdmin: boolean;
}

const CATS = ['warning', 'info', 'emergency', 'custom'] as const;
const CAT_COLORS: Record<string, string> = {
    warning: '#f59e0b', info: '#3b82f6', emergency: '#ef4444', custom: '#8b5cf6',
};

export default function SpeakersTab({ isAdmin }: Props) {
    const [speakers, setSpeakers] = useState<Speaker[]>([]);
    const [audioMessages, setAudioMessages] = useState<AudioMessage[]>([]);
    const [newSpeaker, setNewSpeaker] = useState({ name: '', onvif_address: '', username: '', password: '', zone: '' });
    const [addingSpeaker, setAddingSpeaker] = useState(false);
    const [newMsgName, setNewMsgName] = useState('');
    const [newMsgCategory, setNewMsgCategory] = useState('warning');
    const [newMsgFile, setNewMsgFile] = useState<File | null>(null);
    const [uploadingMsg, setUploadingMsg] = useState(false);

    // Load on mount — previously a load-on-first-render IIFE in the parent,
    // now an explicit useEffect.
    useEffect(() => {
        listSpeakers().then(setSpeakers).catch(() => {});
        listAudioMessages().then(setAudioMessages).catch(() => {});
    }, []);

    const refreshSpeakers = () => { listSpeakers().then(setSpeakers).catch(() => {}); };
    const refreshMessages = () => { listAudioMessages().then(setAudioMessages).catch(() => {}); };

    const handleAddSpeaker = async () => {
        if (!newSpeaker.name || !newSpeaker.onvif_address) return;
        setAddingSpeaker(true);
        try {
            await createSpeaker(newSpeaker);
            setNewSpeaker({ name: '', onvif_address: '', username: '', password: '', zone: '' });
            refreshSpeakers();
        } catch { /* ignore */ }
        setAddingSpeaker(false);
    };

    const handleUploadMsg = async () => {
        if (!newMsgName || !newMsgFile) return;
        setUploadingMsg(true);
        try {
            await uploadAudioMessage(newMsgName, newMsgCategory, newMsgFile);
            setNewMsgName('');
            setNewMsgFile(null);
            refreshMessages();
        } catch { /* ignore */ }
        setUploadingMsg(false);
    };

    return (
        <div className="settings-section" role="tabpanel">
            <div className="settings-section-title">Speaker Devices</div>
            <p className="settings-section-desc">ONVIF audio speakers for talk-down. Add speakers by their ONVIF address.</p>

            {/* Speaker list */}
            {speakers.length > 0 && (
                <div style={{ marginBottom: 16 }}>
                    {speakers.map(spk => (
                        <div key={spk.id} style={{
                            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                            padding: '10px 14px', marginBottom: 6, borderRadius: 8,
                            background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.06)',
                        }}>
                            <div>
                                <div style={{ fontWeight: 600, fontSize: 14 }}>
                                    <span style={{
                                        display: 'inline-block', width: 8, height: 8, borderRadius: '50%',
                                        background: spk.status === 'online' ? '#22c55e' : '#ef4444',
                                        marginRight: 8,
                                    }} />
                                    {spk.name}
                                    {spk.zone && <span style={{ opacity: 0.5, marginLeft: 8, fontSize: 12 }}>({spk.zone})</span>}
                                </div>
                                <div style={{ fontSize: 11, opacity: 0.5 }}>
                                    {spk.onvif_address} · {spk.manufacturer} {spk.model}
                                </div>
                            </div>
                            {isAdmin && (
                                <button
                                    style={{ background: 'transparent', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 14 }}
                                    onClick={async () => { await deleteSpeaker(spk.id); refreshSpeakers(); }}
                                >✕ Remove</button>
                            )}
                        </div>
                    ))}
                </div>
            )}

            {/* Add speaker form */}
            {isAdmin && (
                <div style={{
                    background: 'rgba(255,255,255,0.02)', border: '1px dashed rgba(255,255,255,0.1)',
                    borderRadius: 8, padding: 14, marginBottom: 20,
                }}>
                    <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>+ Add Speaker</div>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                        <input className="settings-input" placeholder="Speaker Name" value={newSpeaker.name}
                            onChange={e => setNewSpeaker({ ...newSpeaker, name: e.target.value })} />
                        <input className="settings-input" placeholder="ONVIF Address (e.g. 192.168.1.50)" value={newSpeaker.onvif_address}
                            onChange={e => setNewSpeaker({ ...newSpeaker, onvif_address: e.target.value })} />
                        <input className="settings-input" placeholder="Username" value={newSpeaker.username}
                            onChange={e => setNewSpeaker({ ...newSpeaker, username: e.target.value })} />
                        <input className="settings-input" placeholder="Password" type="password" value={newSpeaker.password}
                            onChange={e => setNewSpeaker({ ...newSpeaker, password: e.target.value })} />
                        <input className="settings-input" placeholder="Zone (e.g. Perimeter)" value={newSpeaker.zone}
                            onChange={e => setNewSpeaker({ ...newSpeaker, zone: e.target.value })} />
                        <button className="settings-save-btn" onClick={handleAddSpeaker} disabled={addingSpeaker || !newSpeaker.name || !newSpeaker.onvif_address}>
                            {addingSpeaker ? '⏳ Probing…' : '🔊 Add Speaker'}
                        </button>
                    </div>
                </div>
            )}

            {/* Audio Messages section */}
            <div className="settings-section-title" style={{ marginTop: 24 }}>Pre-recorded Messages</div>
            <p className="settings-section-desc">Upload WAV or MP3 audio files for talk-down playback.</p>

            {/* Message list */}
            {audioMessages.length > 0 && (
                <div style={{ marginBottom: 16 }}>
                    {CATS.map(cat => {
                        const msgs = audioMessages.filter(m => m.category === cat);
                        if (msgs.length === 0) return null;
                        return (
                            <div key={cat} style={{ marginBottom: 12 }}>
                                <div style={{
                                    fontSize: 11, fontWeight: 700, textTransform: 'uppercase',
                                    color: CAT_COLORS[cat], marginBottom: 4, letterSpacing: 1,
                                }}>{cat}</div>
                                {msgs.map(msg => (
                                    <div key={msg.id} style={{
                                        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                                        padding: '8px 12px', marginBottom: 4, borderRadius: 6,
                                        background: 'rgba(255,255,255,0.02)', borderLeft: `3px solid ${CAT_COLORS[cat]}`,
                                    }}>
                                        <div>
                                            <span style={{ fontWeight: 500 }}>{msg.name}</span>
                                            <span style={{ opacity: 0.4, marginLeft: 8, fontSize: 11 }}>
                                                {msg.duration > 0 ? `${msg.duration.toFixed(1)}s` : '—'} · {(msg.file_size / 1024).toFixed(0)} KB
                                            </span>
                                        </div>
                                        {isAdmin && (
                                            <button
                                                style={{ background: 'transparent', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 12 }}
                                                onClick={async () => { await deleteAudioMessage(msg.id); refreshMessages(); }}
                                            >✕</button>
                                        )}
                                    </div>
                                ))}
                            </div>
                        );
                    })}
                </div>
            )}
            {audioMessages.length === 0 && (
                <p style={{ opacity: 0.4, fontSize: 13, marginBottom: 16 }}>No messages uploaded yet.</p>
            )}

            {/* Upload form */}
            {isAdmin && (
                <div style={{
                    background: 'rgba(255,255,255,0.02)', border: '1px dashed rgba(255,255,255,0.1)',
                    borderRadius: 8, padding: 14,
                }}>
                    <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>+ Upload Message</div>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 8 }}>
                        <input className="settings-input" placeholder="Message Name" value={newMsgName}
                            onChange={e => setNewMsgName(e.target.value)} />
                        <select className="settings-input" value={newMsgCategory}
                            onChange={e => setNewMsgCategory(e.target.value)}
                            style={{ width: 'auto' }}>
                            <option value="warning">⚠️ Warning</option>
                            <option value="info">ℹ️ Info</option>
                            <option value="emergency">🚨 Emergency</option>
                            <option value="custom">🎵 Custom</option>
                        </select>
                    </div>
                    <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                        <input type="file" accept=".wav,.mp3,.ogg,.m4a"
                            onChange={e => setNewMsgFile(e.target.files?.[0] || null)}
                            style={{ flex: 1 }} />
                        <button className="settings-save-btn" onClick={handleUploadMsg}
                            disabled={uploadingMsg || !newMsgName || !newMsgFile}>
                            {uploadingMsg ? '⏳ Uploading…' : '⬆ Upload'}
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}
