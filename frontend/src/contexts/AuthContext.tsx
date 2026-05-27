'use client';

import { createContext, useContext, useState, useCallback, useEffect, ReactNode } from 'react';

// ── Role Definitions ──
// Must match backend database.ValidRoles exactly
export type UserRole =
    | 'admin'
    | 'soc_operator'
    | 'soc_supervisor'
    | 'site_manager'
    | 'customer'
    | 'viewer';

export interface AuthUser {
    id: string;
    username: string;
    role: UserRole;
    display_name: string;
    email: string;
    phone: string;
    organization_id?: string;
    assigned_site_ids: string[];
}

interface AuthContextValue {
    user: AuthUser | null;
    // token is kept in the context shape for backward compat with consumers
    // that read it, but is always null post-P1-A-02-part2 — the JWT lives
    // in the HttpOnly ironsight_session cookie and is never accessible to JS.
    token: string | null;
    login: (identifier: string, password: string) => Promise<void>;
    logout: () => void;
    isAuthenticated: boolean;
    hasPermission: (route: string) => boolean;
    canAccess: (roles: UserRole[]) => boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) throw new Error('useAuth must be used within AuthProvider');
    return ctx;
}

// TOKEN_KEY: kept only so logout() can clear any stale localStorage entry
// from a session before P1-A-02 part 2. Never written. Safe to remove once
// no legacy sessions can have this key set.
const TOKEN_KEY = 'ironsight_token';
const USER_KEY  = 'ironsight_user';

// ── Route Access Matrix ──
// soc_operator is intentionally limited to /operator only.
const ROUTE_PERMISSIONS: Record<string, UserRole[]> = {
    '/operator':  ['soc_operator', 'soc_supervisor', 'admin'],
    '/analytics': ['soc_supervisor', 'admin', 'site_manager'],
    '/portal':    ['site_manager', 'customer', 'soc_supervisor', 'admin'],
    '/admin':     ['admin'],
    // Reports surface is shared between admin and supervisor — supervisors
    // need to see SLA performance, the verification queue, and evidence
    // shares without getting the full admin tooling.
    '/reports':   ['admin', 'soc_supervisor'],
    '/search':    ['soc_supervisor', 'admin'],
    '/':          ['soc_supervisor', 'site_manager', 'customer', 'admin', 'viewer'],
    '/login':     ['soc_operator', 'soc_supervisor', 'site_manager', 'customer', 'admin', 'viewer'],
};

// ── Role Display Info ──
export const ROLE_INFO: Record<UserRole, { label: string; color: string; description: string }> = {
    admin:          { label: 'Administrator',  color: '#EF4444', description: 'Full system access' },
    soc_operator:   { label: 'SOC Operator',   color: '#E8732A', description: 'Monitor sites, claim alarms' },
    soc_supervisor: { label: 'SOC Supervisor', color: '#a855f7', description: 'Oversee operators and analytics' },
    site_manager:   { label: 'Site Manager',   color: '#22C55E', description: 'View portal, manage site SOPs' },
    customer:       { label: 'Customer',        color: '#E89B2A', description: 'View compliance reports and portal' },
    viewer:         { label: 'Viewer',          color: '#6B7A99', description: 'Camera feeds — read only' },
};

// Demo session — used when NEXT_PUBLIC_DEMO_MODE=1 OR when the user
// hits any route with `?demo=1`. Bypasses /auth/me so the portal
// renders without a working backend, and pins the role to `customer`
// so the customer-facing surfaces (the ones we actually want to
// preview) are what gets shown.
const DEMO_USER: AuthUser = {
    id: 'demo-user',
    username: 'spierce',
    role: 'customer',
    display_name: 'Sandra Pierce',
    email: 'spierce@apexcg.com',
    phone: '312-555-0198',
    organization_id: 'co-alpha001',
    assigned_site_ids: ['ACG-301', 'ACG-302'],
};

function demoModeActive(): boolean {
    if (typeof window === 'undefined') return false;
    if (process.env.NEXT_PUBLIC_DEMO_MODE === '1') return true;
    if (window.location.search.includes('demo=1')) {
        // Sticky for the session — once you opt in, internal nav keeps it.
        try { window.sessionStorage.setItem('ironsight_demo', '1'); } catch { /* ignore */ }
        return true;
    }
    try { return window.sessionStorage.getItem('ironsight_demo') === '1'; } catch { return false; }
}

export function AuthProvider({ children }: { children: ReactNode }) {
    const [token, setToken] = useState<string | null>(null);
    const [user, setUser] = useState<AuthUser | null>(null);
    const [ready, setReady] = useState(false);

    // Rehydrate session on mount by probing /auth/me.
    //
    // P1-A-02 part 2: the session JWT now lives in the ironsight_session
    // HttpOnly cookie and is never accessible to JS. Rehydration calls
    // /auth/me with credentials:'include' so the browser attaches both
    // cookies automatically. The Authorization header is no longer sent.
    //
    // SSO path (X-Forwarded-Email injected by NPM + oauth2-proxy):
    // the backend identifies the user from the header even without a
    // session cookie — this path continues to work unchanged.
    useEffect(() => {
        // Demo bypass: skip the API round-trip and inject the demo
        // customer. This is the path used by `npm run dev` previews
        // when no backend is running.
        if (demoModeActive()) {
            setToken(null); // no token in JS context — cookie owns the credential
            setUser(DEMO_USER);
            setReady(true);
            return;
        }

        // Cookie is sent automatically; CSRF is not required on GET.
        fetch('/auth/me', { credentials: 'include' })
            .then(res => {
                if (!res.ok) throw new Error('not authenticated');
                return res.json() as Promise<AuthUser>;
            })
            .then(u => {
                setToken(null); // JWT stays in HttpOnly cookie — never exposed to JS
                setUser(u);
                localStorage.setItem(USER_KEY, JSON.stringify(u));
            })
            .catch(() => {
                // No SSO header AND no valid session cookie — clear any stale state.
                localStorage.removeItem(TOKEN_KEY);
                localStorage.removeItem(USER_KEY);
            })
            .finally(() => setReady(true));
    }, []);

    const login = useCallback(async (identifier: string, password: string) => {
        // P1-A-02 part 2: credentials:'include' lets the browser store the
        // ironsight_session + ironsight_csrf cookies from the Set-Cookie
        // response headers. We no longer write the JWT to localStorage.
        // The response body still contains `token` for legacy clients; we
        // ignore it here — the JWT lives in the HttpOnly cookie (P1-A-02 PR3).
        const res = await fetch('/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: identifier, password }),
            credentials: 'include',
        });
        if (!res.ok) {
            const text = await res.text();
            throw new Error(text.trim() || 'Login failed');
        }
        const data = await res.json() as { token: string; user: AuthUser };
        // Persist the user object (not the token) for role-based redirects in
        // login/page.tsx. The token stays in the HttpOnly cookie — never in JS.
        // NOTE: ironsight_user in localStorage is intentionally kept here. See
        // plan §5 "ironsight_user localStorage reads post-migration" — login/page.tsx
        // reads ironsight_user after login for redirect; that read is safe.
        localStorage.setItem(USER_KEY, JSON.stringify(data.user));
        setToken(null); // JWT in cookie — not exposed to JS
        setUser(data.user);
    }, []);

    const logout = useCallback(() => {
        // P1-A-02 part 2: POST /auth/logout to clear the session cookies
        // server-side (HandleLogout sets Max-Age=0). Fire-and-forget — the
        // redirect happens regardless so the user isn't stuck if the request
        // fails. CSRF is required on the logout POST.
        const csrfToken = typeof window !== 'undefined'
            ? (document.cookie.split('; ').find(r => r.startsWith('ironsight_csrf='))?.split('=')[1] ?? '')
            : '';
        fetch('/auth/logout', {
            method: 'POST',
            credentials: 'include',
            headers: csrfToken ? { 'X-CSRF-Token': csrfToken } : {},
        }).catch(() => { /* best-effort */ });
        localStorage.removeItem(TOKEN_KEY);
        localStorage.removeItem(USER_KEY);
        setToken(null);
        setUser(null);
        window.location.href = '/login';
    }, []);

    const hasPermission = useCallback((route: string) => {
        if (!user) return false;
        const matched = Object.keys(ROUTE_PERMISSIONS)
            .filter(r => route.startsWith(r))
            .sort((a, b) => b.length - a.length)[0];
        if (!matched) return true;
        return ROUTE_PERMISSIONS[matched].includes(user.role);
    }, [user]);

    const canAccess = useCallback((roles: UserRole[]) => {
        if (!user) return false;
        return roles.includes(user.role);
    }, [user]);

    // Don't render children until session check is complete
    if (!ready) return null;

    return (
        <AuthContext.Provider value={{
            user, token, login, logout,
            // Authenticated if we have a user object — populated by /auth/me
            // whether the user arrived via cookie (local-login) or via the
            // SSO header-trust path. The token field is always null post-P1-A-02-part2.
            isAuthenticated: !!user,
            hasPermission, canAccess,
        }}>
            {children}
        </AuthContext.Provider>
    );
}

