'use client';

// Extracted from CameraManager.tsx (P1-B-11 session 15). Decomposes a
// webhook URL into the four fields the Milesight Sense camera's Alarm
// Server dialog expects (Protocol / Host / Port / Path) and renders each
// with its own copy button. The camera's UI splits the URL across
// separate inputs, so showing the full URL alone forces the operator to
// parse it by hand. Used by the post-create overlay and the camera-
// settings General tab.

export default function SenseWebhookFields({ url }: { url: string }) {
    const parts = (() => {
        try {
            const u = new URL(url);
            return {
                protocol: u.protocol === 'https:' ? 'HTTPS' : 'HTTP',
                host: u.hostname,
                port: u.port || (u.protocol === 'https:' ? '443' : '80'),
                path: u.pathname + (u.search || ''),
            };
        } catch {
            return { protocol: '', host: '', port: '', path: url };
        }
    })();
    const rows: { label: string; value: string }[] = [
        { label: 'Protocol Type', value: parts.protocol },
        { label: 'Destination IP/Host Name', value: parts.host },
        { label: 'Port', value: parts.port },
        { label: 'Path', value: parts.path },
    ];
    return (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12, marginBottom: 8 }}>
            <tbody>
                {rows.map(row => (
                    <tr key={row.label} style={{ borderTop: '1px solid rgba(255,255,255,0.06)' }}>
                        <td style={{ padding: '8px 4px', color: 'var(--text-muted)', width: '38%' }}>
                            {row.label}
                        </td>
                        <td style={{
                            padding: '8px 4px',
                            fontFamily: "'JetBrains Mono', monospace",
                            color: '#22c55e',
                            wordBreak: 'break-all',
                        }}>
                            {row.value || <span style={{ color: 'var(--text-muted)' }}>—</span>}
                        </td>
                        <td style={{ padding: '8px 4px', width: 70, textAlign: 'right' }}>
                            <button
                                className="btn"
                                style={{ padding: '3px 10px', fontSize: 10 }}
                                onClick={() => navigator.clipboard?.writeText(row.value).catch(() => {})}
                                disabled={!row.value}
                            >
                                Copy
                            </button>
                        </td>
                    </tr>
                ))}
            </tbody>
        </table>
    );
}
