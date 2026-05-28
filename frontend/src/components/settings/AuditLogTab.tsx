'use client';

// Extracted from SettingsPage.tsx (P1-B-11 session 9). Audit Log viewer
// — paginated table of user actions with username filter. State is
// local to the tab (entries/total/page/filter), so the four useState
// slots that lived on the parent come along with the JSX.

import { useState } from 'react';
import { type AuditEntry, queryAuditLog } from '@/lib/api';

const AUDIT_PAGE_SIZE = 25;

export default function AuditLogTab() {
    const [auditEntries, setAuditEntries] = useState<AuditEntry[]>([]);
    const [auditTotal, setAuditTotal] = useState(0);
    const [auditPage, setAuditPage] = useState(0);
    const [auditFilter, setAuditFilter] = useState('');

    const runSearch = async (page: number = auditPage) => {
        try {
            const data = await queryAuditLog({
                username: auditFilter || undefined,
                limit: AUDIT_PAGE_SIZE,
                offset: page * AUDIT_PAGE_SIZE,
            });
            setAuditEntries(data.entries);
            setAuditTotal(data.total);
            setAuditPage(page);
        } catch { /* ignore */ }
    };

    return (
        <div className="settings-section">
            <div className="settings-section-header">
                <h3>Audit Log</h3>
                <p style={{ fontSize: 13, opacity: 0.6, margin: '4px 0 0' }}>All user actions are recorded here</p>
            </div>

            <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
                <input
                    type="text"
                    className="settings-input"
                    placeholder="Filter by username..."
                    value={auditFilter}
                    onChange={e => setAuditFilter(e.target.value)}
                    style={{ flex: 1 }}
                />
                <button
                    className="settings-btn settings-btn-primary"
                    onClick={() => runSearch(0)}
                >
                    Search
                </button>
            </div>

            <div style={{ overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                    <thead>
                        <tr style={{ borderBottom: '1px solid rgba(255,255,255,0.1)' }}>
                            <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>Time</th>
                            <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>User</th>
                            <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>Action</th>
                            <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>Target</th>
                            <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>IP</th>
                        </tr>
                    </thead>
                    <tbody>
                        {auditEntries.length === 0 ? (
                            <tr><td colSpan={5} style={{ padding: 20, textAlign: 'center', opacity: 0.5 }}>No entries — click Search to load</td></tr>
                        ) : auditEntries.map(e => (
                            <tr key={e.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.04)' }}>
                                <td style={{ padding: '6px', whiteSpace: 'nowrap', opacity: 0.7 }}>{new Date(e.created_at).toLocaleString()}</td>
                                <td style={{ padding: '6px', fontWeight: 600 }}>{e.username}</td>
                                <td style={{ padding: '6px' }}>
                                    <span style={{
                                        background: e.action.includes('delete') ? 'rgba(239,68,68,0.15)' :
                                            e.action.includes('create') ? 'rgba(34,197,94,0.15)' :
                                                'rgba(255,255,255,0.06)',
                                        padding: '2px 8px', borderRadius: 4, fontSize: 12,
                                        color: e.action.includes('delete') ? '#ef4444' :
                                            e.action.includes('create') ? '#22c55e' : 'inherit'
                                    }}>
                                        {e.action}
                                    </span>
                                </td>
                                <td style={{ padding: '6px', opacity: 0.7 }}>{e.target_type}{e.target_id ? ` / ${e.target_id.substring(0, 8)}...` : ''}</td>
                                <td style={{ padding: '6px', fontSize: 11, opacity: 0.5, fontFamily: 'monospace' }}>{e.ip_address}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>

            {auditTotal > AUDIT_PAGE_SIZE && (
                <div style={{ display: 'flex', justifyContent: 'center', gap: 12, marginTop: 16 }}>
                    <button className="settings-btn" disabled={auditPage === 0} onClick={() => runSearch(auditPage - 1)}>◀ Prev</button>
                    <span style={{ fontSize: 13, alignSelf: 'center', opacity: 0.6 }}>
                        Page {auditPage + 1} of {Math.ceil(auditTotal / AUDIT_PAGE_SIZE)}
                    </span>
                    <button className="settings-btn" disabled={(auditPage + 1) * AUDIT_PAGE_SIZE >= auditTotal} onClick={() => runSearch(auditPage + 1)}>Next ▶</button>
                </div>
            )}
        </div>
    );
}
