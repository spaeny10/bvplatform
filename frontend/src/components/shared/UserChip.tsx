'use client';

import { useState, useRef, useEffect } from 'react';
import { useAuth, ROLE_INFO } from '@/contexts/AuthContext';

/**
 * Inline user chip for placement inside a navbar.
 * Renders a compact avatar+name button; clicking opens a dropdown with sign-out.
 */
export default function UserChip() {
    const { user, logout } = useAuth();
    const [open, setOpen] = useState(false);
    const ref = useRef<HTMLDivElement>(null);

    // Close when clicking outside
    useEffect(() => {
        if (!open) return;
        const handler = (e: MouseEvent) => {
            if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
        };
        document.addEventListener('mousedown', handler);
        return () => document.removeEventListener('mousedown', handler);
    }, [open]);

    if (!user) return null;

    const info = ROLE_INFO[user.role] ?? ROLE_INFO.viewer;
    const initials = (user.display_name || user.username)
        .split(' ')
        .map((w: string) => w[0])
        .slice(0, 2)
        .join('')
        .toUpperCase();

    return (
        <div ref={ref} style={{ position: 'relative', flexShrink: 0 }}>
            <button
                onClick={() => setOpen(v => !v)}
                style={{
                    display: 'flex', alignItems: 'center', gap: 7,
                    padding: '4px 10px 4px 5px',
                    borderRadius: 6,
                    background: open ? 'rgba(255,255,255,0.06)' : 'transparent',
                    border: `1px solid ${open ? info.color + '35' : 'rgba(255,255,255,0.07)'}`,
                    cursor: 'pointer',
                    transition: 'all 0.15s',
                }}
                onMouseEnter={e => {
                    if (!open) {
                        e.currentTarget.style.background = 'rgba(255,255,255,0.05)';
                        e.currentTarget.style.borderColor = 'rgba(255,255,255,0.12)';
                    }
                }}
                onMouseLeave={e => {
                    if (!open) {
                        e.currentTarget.style.background = 'transparent';
                        e.currentTarget.style.borderColor = 'rgba(255,255,255,0.07)';
                    }
                }}
            >
                {/* Avatar */}
                <div style={{
                    width: 24, height: 24, borderRadius: '50%',
                    background: `${info.color}18`,
                    border: `1px solid ${info.color}35`,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 9, fontWeight: 700,
                    color: info.color,
                    fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
                    flexShrink: 0,
                }}>
                    {initials}
                </div>

                {/* Name */}
                <span style={{
                    fontSize: 12, fontWeight: 600,
                    color: '#C8D0E0', whiteSpace: 'nowrap',
                    maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis',
                }}>
                    {user.display_name || user.username}
                </span>

                {/* Caret */}
                <span style={{
                    color: '#3A4258', fontSize: 8,
                    transform: open ? 'rotate(180deg)' : 'rotate(0)',
                    transition: 'transform 0.15s',
                }}>▾</span>
            </button>

            {/* Dropdown */}
            {open && (
                <div style={{
                    position: 'absolute', top: 'calc(100% + 6px)', right: 0,
                    background: '#0E1117',
                    border: '1px solid rgba(255,255,255,0.08)',
                    borderRadius: 8, padding: 6, minWidth: 210,
                    boxShadow: '0 8px 32px rgba(0,0,0,0.55)',
                    zIndex: 1000,
                }}>
                    {/* User summary */}
                    <div style={{
                        padding: '8px 10px 10px',
                        borderBottom: '1px solid rgba(255,255,255,0.05)',
                        marginBottom: 4,
                    }}>
                        <div style={{ fontSize: 12, fontWeight: 600, color: '#E4E8F0', marginBottom: 2 }}>
                            {user.display_name || user.username}
                        </div>
                        {user.email && (
                            <div style={{ fontSize: 10, color: '#6B7A99', marginBottom: 6 }}>{user.email}</div>
                        )}
                        <div style={{
                            display: 'inline-flex', alignItems: 'center', gap: 4,
                            padding: '2px 7px', borderRadius: 4,
                            background: `${info.color}12`,
                            border: `1px solid ${info.color}22`,
                        }}>
                            <span style={{
                                width: 5, height: 5, borderRadius: '50%',
                                background: info.color, flexShrink: 0,
                            }} />
                            <span style={{
                                fontSize: 9, color: info.color,
                                fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
                                letterSpacing: '0.05em', textTransform: 'uppercase',
                            }}>
                                {info.label}
                            </span>
                        </div>
                    </div>

                    {/* Admin panel — internal admins only */}
                    {user.role === 'admin' && (
                        <a
                            href="/admin"
                            style={{
                                display: 'flex', alignItems: 'center', gap: 8,
                                width: '100%', padding: '8px 10px', borderRadius: 4,
                                background: 'transparent', border: '1px solid transparent',
                                color: '#8891A5', cursor: 'pointer', fontSize: 11,
                                fontFamily: 'inherit', transition: 'all 0.15s', textDecoration: 'none',
                            }}
                            onMouseEnter={e => {
                                e.currentTarget.style.color = '#E89B2A';
                                e.currentTarget.style.background = 'rgba(232,115,42,0.08)';
                                e.currentTarget.style.borderColor = 'rgba(232,115,42,0.18)';
                            }}
                            onMouseLeave={e => {
                                e.currentTarget.style.color = '#8891A5';
                                e.currentTarget.style.background = 'transparent';
                                e.currentTarget.style.borderColor = 'transparent';
                            }}
                        >
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                            </svg>
                            Admin Panel
                        </a>
                    )}

                    {/* Sign out */}
                    <button
                        onClick={logout}
                        style={{
                            display: 'flex', alignItems: 'center', gap: 8,
                            width: '100%', padding: '8px 10px', borderRadius: 4,
                            background: 'transparent', border: '1px solid transparent',
                            color: '#8891A5', cursor: 'pointer', fontSize: 11,
                            fontFamily: 'inherit', transition: 'all 0.15s', textAlign: 'left',
                        }}
                        onMouseEnter={e => {
                            e.currentTarget.style.color = '#EF4444';
                            e.currentTarget.style.background = 'rgba(239,68,68,0.08)';
                            e.currentTarget.style.borderColor = 'rgba(239,68,68,0.18)';
                        }}
                        onMouseLeave={e => {
                            e.currentTarget.style.color = '#8891A5';
                            e.currentTarget.style.background = 'transparent';
                            e.currentTarget.style.borderColor = 'transparent';
                        }}
                    >
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
                            <polyline points="16 17 21 12 16 7" />
                            <line x1="21" y1="12" x2="9" y2="12" />
                        </svg>
                        Sign out
                    </button>
                </div>
            )}
        </div>
    );
}
