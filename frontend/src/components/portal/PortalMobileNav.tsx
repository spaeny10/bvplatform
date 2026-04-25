'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

// Bottom-tab nav for the customer portal on mobile viewports.
//
// Construction owners and site managers use Ironsight from their
// phones — in trucks, on-site, between meetings — far more than from
// a desk. The portal's existing top-bar nav works at desktop widths
// but loses badly to a thumb-reachable bottom-tab pattern on phones.
// This component renders only on viewports ≤ 640px (CSS-driven, no
// JS resize listener), and stays hidden on desktop where the
// horizontal nav already does the job.
//
// Tabs match the routes a customer actually traverses: dashboard,
// incidents, history, notifications, status. Active state matches
// any sub-path so /portal/sites/CS-547 still shows "Sites" lit up.

interface Tab {
    href: string;
    label: string;
    icon: string;
    matchPrefix?: string; // override for sub-route matching
}

const TABS: Tab[] = [
    { href: '/portal',                label: 'Dashboard', icon: '🏠' },
    { href: '/portal/history',        label: 'History',   icon: '🔎' },
    { href: '/portal/notifications',  label: 'Alerts',    icon: '🔔' },
    { href: '/status',                label: 'Status',    icon: '📡' },
];

export default function PortalMobileNav() {
    const pathname = usePathname() || '';

    return (
        <nav
            className="portal-mobile-nav"
            aria-label="Portal navigation"
            style={{
                // CSS positioning + z-index inline; visibility controlled by
                // the media query in the <style> block below so SSR + hydration
                // both agree on whether to render.
                position: 'fixed',
                bottom: 0, left: 0, right: 0,
                background: 'var(--sg-surface-1, rgba(15,18,24,0.96))',
                borderTop: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.08))',
                backdropFilter: 'blur(8px)',
                WebkitBackdropFilter: 'blur(8px)',
                display: 'flex', justifyContent: 'space-around',
                padding: '8px 4px calc(8px + env(safe-area-inset-bottom)) 4px',
                zIndex: 100,
                fontFamily: "var(--font-family, 'Inter', sans-serif)",
            }}
        >
            <style>{`
                .portal-mobile-nav {
                    display: none;
                }
                @media (max-width: 640px) {
                    .portal-mobile-nav {
                        display: flex !important;
                    }
                    /* Reserve space at the bottom of the portal pages so the
                       last row of content doesn't sit behind the nav. The
                       constant matches the rendered nav height + safe-area. */
                    body {
                        padding-bottom: calc(64px + env(safe-area-inset-bottom));
                    }
                }
            `}</style>
            {TABS.map((t) => {
                const active = isActive(pathname, t);
                return (
                    <Link
                        key={t.href}
                        href={t.href}
                        aria-current={active ? 'page' : undefined}
                        style={{
                            flex: 1,
                            display: 'flex', flexDirection: 'column', alignItems: 'center',
                            gap: 2, padding: '6px 4px',
                            textDecoration: 'none',
                            color: active
                                ? 'var(--brand-primary, #E8732A)'
                                : 'var(--sg-text-dim, #9CA3AF)',
                            fontSize: 10, fontWeight: 600,
                            transition: 'color 0.15s',
                        }}
                    >
                        <span aria-hidden="true" style={{ fontSize: 20, lineHeight: 1 }}>
                            {t.icon}
                        </span>
                        <span style={{ letterSpacing: 0.3 }}>{t.label}</span>
                    </Link>
                );
            })}
        </nav>
    );
}

function isActive(pathname: string, t: Tab): boolean {
    const prefix = t.matchPrefix ?? t.href;
    if (prefix === '/portal') {
        // Dashboard tab lights up only on the exact /portal index, so
        // sub-routes (/portal/sites/X, /portal/incidents/Y) don't all
        // appear to be on the dashboard.
        return pathname === '/portal' || pathname === '/portal/';
    }
    return pathname === prefix || pathname.startsWith(prefix + '/');
}
