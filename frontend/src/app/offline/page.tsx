'use client';

import { BRAND } from '@/lib/branding';

// Offline fallback page — served by the service worker when the user
// hits a route they haven't previously visited (so it's not in the
// runtime cache) AND the network is dead. The previously-visited
// routes still render from cache; this page is the safety net for
// "first-time + no network" navigation.
//
// Deliberately self-contained: no API calls, no fetch, no auth
// hook usage. The page must render even if every JavaScript module
// can't reach the server.

export default function OfflinePage() {
    return (
        <div style={{
            minHeight: '100vh',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            background: 'var(--sg-bg-base, #0c1015)',
            color: 'var(--sg-text-primary, #E4E8F0)',
            fontFamily: "var(--font-family, 'Inter', sans-serif)",
            padding: 24, textAlign: 'center',
        }}>
            <div style={{ maxWidth: 360 }}>
                <div style={{ fontSize: 48, marginBottom: 16, opacity: 0.6 }}>📡</div>
                <h1 style={{ fontSize: 22, fontWeight: 700, margin: '0 0 8px' }}>
                    You're offline
                </h1>
                <p style={{ fontSize: 14, lineHeight: 1.6, color: 'var(--sg-text-dim, #9CA3AF)', margin: '0 0 20px' }}>
                    {BRAND.name} can't reach the server right now. Pages you've
                    visited recently still work; pages you haven't will
                    open the moment you reconnect.
                </p>
                <button
                    onClick={() => location.reload()}
                    style={{
                        padding: '10px 20px',
                        background: 'var(--brand-primary, #E8732A)',
                        color: '#fff',
                        border: 'none',
                        borderRadius: 4,
                        fontSize: 13, fontWeight: 600,
                        cursor: 'pointer',
                        fontFamily: 'inherit',
                    }}
                >
                    Try again
                </button>
            </div>
        </div>
    );
}
