'use client';

// ComplianceRulesPanel — form + list for managing compliance rules for a camera.
// No canvas. Allows operators to bind a PPE zone to a ppe_required or no_go rule
// and specify which PPE classes are required inside the zone.
//
// P2-C-04. Routes: GET/POST/PUT/DELETE /api/cameras/{id}/compliance-rules.

import { useState, useEffect, useCallback } from 'react';
import {
    ComplianceRule, ComplianceRuleCreate, PPEZone,
    listComplianceRules, createComplianceRule, updateComplianceRule, deleteComplianceRule,
    listPPEZones,
} from '@/lib/api';

// Known YOLO PPE violation class labels. Users pick from this list.
const PPE_CLASS_OPTIONS = [
    { key: 'no-hat',   label: 'Hard Hat' },
    { key: 'no-vest',  label: 'Hi-Vis Vest' },
    { key: 'no-mask',  label: 'Face Mask' },
    { key: 'no-glove', label: 'Gloves' },
] as const;

interface Props {
    cameraId: string;
}

const emptyCreate = (): ComplianceRuleCreate => ({
    zone_id: '',
    rule_type: 'ppe_required',
    ppe_classes: [],
    enabled: true,
    site_wide: false,
});

export default function ComplianceRulesPanel({ cameraId }: Props) {
    const [rules, setRules] = useState<ComplianceRule[]>([]);
    const [zones, setZones] = useState<PPEZone[]>([]);
    const [form, setForm] = useState<ComplianceRuleCreate>(emptyCreate());
    const [editingId, setEditingId] = useState<string | null>(null);
    const [saving, setSaving] = useState(false);
    const [deleting, setDeleting] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);
    const [showForm, setShowForm] = useState(false);

    const load = useCallback(async () => {
        const [r, z] = await Promise.all([
            listComplianceRules(cameraId),
            listPPEZones(cameraId),
        ]);
        setRules(r);
        setZones(z);
    }, [cameraId]);

    useEffect(() => { load(); }, [load]);

    const flash = (msg: string, isError = false) => {
        if (isError) { setError(msg); setSuccess(null); }
        else { setSuccess(msg); setError(null); }
        setTimeout(() => { setError(null); setSuccess(null); }, 4000);
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!form.zone_id) { flash('Please select a zone.', true); return; }
        if (form.rule_type === 'ppe_required' && form.ppe_classes.length === 0) {
            flash('Please select at least one PPE class for a ppe_required rule.', true);
            return;
        }

        setSaving(true);
        try {
            if (editingId) {
                await updateComplianceRule(cameraId, editingId, form);
                flash('Rule updated.');
            } else {
                await createComplianceRule(cameraId, form);
                flash('Rule created.');
            }
            setForm(emptyCreate());
            setEditingId(null);
            setShowForm(false);
            await load();
        } catch (err: any) {
            flash(err?.message || 'Save failed', true);
        } finally {
            setSaving(false);
        }
    };

    const handleEdit = (rule: ComplianceRule) => {
        setForm({
            zone_id: rule.zone_id,
            rule_type: rule.rule_type,
            ppe_classes: rule.ppe_classes,
            enabled: rule.enabled,
            notes: rule.notes,
            site_wide: rule.site_wide,
        });
        setEditingId(rule.id);
        setShowForm(true);
    };

    const handleDelete = async (ruleId: string) => {
        setDeleting(ruleId);
        try {
            await deleteComplianceRule(cameraId, ruleId);
            flash('Rule deleted.');
            await load();
        } catch (err: any) {
            flash(err?.message || 'Delete failed', true);
        } finally {
            setDeleting(null);
        }
    };

    const toggleClass = (cls: string) => {
        setForm(prev => ({
            ...prev,
            ppe_classes: prev.ppe_classes.includes(cls)
                ? prev.ppe_classes.filter(c => c !== cls)
                : [...prev.ppe_classes, cls],
        }));
    };

    const cancelForm = () => {
        setForm(emptyCreate());
        setEditingId(null);
        setShowForm(false);
    };

    return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>

            {/* Add rule button */}
            {!showForm && (
                <button
                    type="button"
                    onClick={() => { setForm(emptyCreate()); setEditingId(null); setShowForm(true); }}
                    style={{
                        alignSelf: 'flex-start', padding: '7px 16px', fontSize: 11, fontWeight: 700,
                        borderRadius: 5, cursor: 'pointer', fontFamily: 'inherit',
                        background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.35)',
                        color: '#22C55E',
                    }}
                >
                    + New Compliance Rule
                </button>
            )}

            {/* Create/edit form */}
            {showForm && (
                <form onSubmit={handleSubmit}
                    style={{ padding: 14, borderRadius: 6, background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.08)', display: 'flex', flexDirection: 'column', gap: 12 }}>

                    <div style={{ fontSize: 11, fontWeight: 700, color: '#E4E8F0' }}>
                        {editingId ? 'Edit Compliance Rule' : 'New Compliance Rule'}
                    </div>

                    {/* Zone picker */}
                    <div>
                        <label style={{ display: 'block', fontSize: 10, color: '#8891A5', marginBottom: 4 }}>Zone</label>
                        {zones.length === 0 ? (
                            <div style={{ fontSize: 11, color: '#E89B2A' }}>
                                No PPE zones defined yet — draw zones in the "PPE Zones" tab first.
                            </div>
                        ) : (
                            <select
                                value={form.zone_id}
                                onChange={e => setForm(prev => ({ ...prev, zone_id: e.target.value }))}
                                style={{
                                    width: '100%', padding: '6px 8px', borderRadius: 4, fontSize: 11,
                                    background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.1)',
                                    color: '#E4E8F0', fontFamily: 'inherit',
                                }}
                            >
                                <option value="">— Select a zone —</option>
                                {zones.map(z => (
                                    <option key={z.id} value={z.id}>
                                        {z.name || z.zone_type} ({z.zone_type})
                                    </option>
                                ))}
                            </select>
                        )}
                    </div>

                    {/* Rule type */}
                    <div>
                        <label style={{ display: 'block', fontSize: 10, color: '#8891A5', marginBottom: 4 }}>Rule Type</label>
                        <div style={{ display: 'flex', gap: 8 }}>
                            {(['ppe_required', 'no_go'] as const).map(rt => (
                                <button
                                    key={rt}
                                    type="button"
                                    onClick={() => setForm(prev => ({ ...prev, rule_type: rt }))}
                                    style={{
                                        padding: '6px 14px', borderRadius: 4, fontSize: 11, fontWeight: 600, cursor: 'pointer', fontFamily: 'inherit',
                                        background: form.rule_type === rt ? 'rgba(34,197,94,0.12)' : 'rgba(255,255,255,0.02)',
                                        border: `1px solid ${form.rule_type === rt ? 'rgba(34,197,94,0.4)' : 'rgba(255,255,255,0.08)'}`,
                                        color: form.rule_type === rt ? '#22C55E' : '#8891A5',
                                    }}
                                >
                                    {rt === 'ppe_required' ? 'PPE Required' : 'No-Go Zone'}
                                </button>
                            ))}
                        </div>
                        {form.rule_type === 'no_go' && (
                            <div style={{ fontSize: 9, color: '#E89B2A', marginTop: 4 }}>
                                No-go violations route to the security alarm pipeline, not the PPE review queue.
                            </div>
                        )}
                    </div>

                    {/* PPE class selection — only for ppe_required */}
                    {form.rule_type === 'ppe_required' && (
                        <div>
                            <label style={{ display: 'block', fontSize: 10, color: '#8891A5', marginBottom: 4 }}>
                                Required PPE (select all that must be worn)
                            </label>
                            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                                {PPE_CLASS_OPTIONS.map(opt => {
                                    const selected = form.ppe_classes.includes(opt.key);
                                    return (
                                        <button
                                            key={opt.key}
                                            type="button"
                                            onClick={() => toggleClass(opt.key)}
                                            style={{
                                                padding: '5px 12px', borderRadius: 4, fontSize: 10, fontWeight: 600,
                                                cursor: 'pointer', fontFamily: 'inherit',
                                                background: selected ? 'rgba(34,197,94,0.12)' : 'rgba(255,255,255,0.02)',
                                                border: `1px solid ${selected ? 'rgba(34,197,94,0.4)' : 'rgba(255,255,255,0.08)'}`,
                                                color: selected ? '#22C55E' : '#8891A5',
                                            }}
                                        >
                                            {selected ? '✓ ' : ''}{opt.label}
                                        </button>
                                    );
                                })}
                            </div>
                        </div>
                    )}

                    {/* Site-wide toggle */}
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                        <input
                            type="checkbox"
                            checked={form.site_wide}
                            onChange={e => setForm(prev => ({ ...prev, site_wide: e.target.checked }))}
                        />
                        <span style={{ fontSize: 10, color: '#8891A5' }}>
                            Site-wide (applies to all cameras at this site — shown with SITE-WIDE badge in list)
                        </span>
                    </label>

                    {/* Enabled toggle */}
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                        <input
                            type="checkbox"
                            checked={form.enabled}
                            onChange={e => setForm(prev => ({ ...prev, enabled: e.target.checked }))}
                        />
                        <span style={{ fontSize: 10, color: '#8891A5' }}>Enabled</span>
                    </label>

                    {/* Notes */}
                    <div>
                        <label style={{ display: 'block', fontSize: 10, color: '#8891A5', marginBottom: 4 }}>Notes (optional)</label>
                        <input
                            value={form.notes || ''}
                            onChange={e => setForm(prev => ({ ...prev, notes: e.target.value || undefined }))}
                            placeholder="e.g. Hard hat required on scaffold level 2"
                            style={{
                                width: '100%', padding: '5px 8px', borderRadius: 4, fontSize: 10, boxSizing: 'border-box',
                                background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
                                color: '#E4E8F0', fontFamily: 'inherit',
                            }}
                        />
                    </div>

                    {/* Action buttons */}
                    <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                        <button type="button" onClick={cancelForm}
                            style={{ padding: '6px 16px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: 'none', border: '1px solid rgba(255,255,255,0.15)', color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit' }}>
                            Cancel
                        </button>
                        <button type="submit" disabled={saving}
                            style={{ padding: '6px 20px', borderRadius: 4, fontSize: 11, fontWeight: 700, background: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.4)', color: '#22C55E', cursor: saving ? 'wait' : 'pointer', fontFamily: 'inherit' }}>
                            {saving ? 'Saving...' : (editingId ? 'Update Rule' : 'Create Rule')}
                        </button>
                    </div>
                </form>
            )}

            {/* Status messages */}
            {(error || success) && (
                <div style={{
                    fontSize: 10, padding: '5px 10px', borderRadius: 4,
                    background: error ? 'rgba(239,68,68,0.08)' : 'rgba(34,197,94,0.08)',
                    color: error ? '#EF4444' : '#22C55E',
                    border: `1px solid ${error ? 'rgba(239,68,68,0.2)' : 'rgba(34,197,94,0.2)'}`,
                }}>
                    {error || success}
                </div>
            )}

            {/* Rule list */}
            {rules.length === 0 ? (
                <div style={{ fontSize: 11, color: '#4A5268', padding: '8px 0' }}>
                    No compliance rules configured for this camera yet.
                </div>
            ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268' }}>
                        Rules ({rules.length})
                    </div>
                    {rules.map(rule => (
                        <div key={rule.id}
                            style={{
                                display: 'flex', alignItems: 'center', gap: 8,
                                padding: '8px 10px', borderRadius: 4,
                                background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.04)',
                                opacity: rule.enabled ? 1 : 0.5,
                            }}
                        >
                            <div style={{ flex: 1, minWidth: 0 }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                                    <span style={{ fontSize: 11, fontWeight: 600, color: rule.rule_type === 'no_go' ? '#EF4444' : '#22C55E' }}>
                                        {rule.rule_type === 'no_go' ? 'No-Go' : 'PPE Required'}
                                    </span>
                                    {rule.zone_name && (
                                        <span style={{ fontSize: 10, color: '#8891A5' }}>in {rule.zone_name}</span>
                                    )}
                                    {rule.site_wide && (
                                        <span style={{ fontSize: 8, fontWeight: 700, padding: '1px 5px', borderRadius: 3, background: 'rgba(99,102,241,0.12)', border: '1px solid rgba(99,102,241,0.3)', color: '#818cf8' }}>
                                            SITE-WIDE
                                        </span>
                                    )}
                                    {!rule.enabled && (
                                        <span style={{ fontSize: 8, fontWeight: 700, padding: '1px 5px', borderRadius: 3, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#4A5268' }}>
                                            DISABLED
                                        </span>
                                    )}
                                </div>
                                {rule.rule_type === 'ppe_required' && rule.ppe_classes.length > 0 && (
                                    <div style={{ fontSize: 9, color: '#4A5268', marginTop: 2 }}>
                                        Requires: {rule.ppe_classes.join(', ')}
                                    </div>
                                )}
                                {rule.notes && (
                                    <div style={{ fontSize: 9, color: '#4A5268', marginTop: 2 }}>{rule.notes}</div>
                                )}
                            </div>
                            <button type="button" onClick={() => handleEdit(rule)}
                                style={{ fontSize: 8, fontWeight: 700, padding: '2px 8px', borderRadius: 3, background: 'rgba(59,130,246,0.08)', border: '1px solid rgba(59,130,246,0.2)', color: '#60a5fa', cursor: 'pointer', fontFamily: 'inherit' }}>
                                Edit
                            </button>
                            <button type="button" onClick={() => handleDelete(rule.id)} disabled={deleting === rule.id}
                                style={{ fontSize: 8, fontWeight: 700, padding: '2px 8px', borderRadius: 3, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', color: '#EF4444', cursor: deleting === rule.id ? 'wait' : 'pointer', fontFamily: 'inherit' }}>
                                {deleting === rule.id ? '...' : 'Del'}
                            </button>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}
