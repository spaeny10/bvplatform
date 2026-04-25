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

export function AuthProvider({ children }: { children: ReactNode }) {
    const [token, setToken] = useState<string | null>(null);
    const [user, setUser] = useState<AuthUser | null>(null);
    const [ready, setReady] = useState(false);

    // Rehydrate session on mount by validating the stored token with /auth/me
    useEffect(() => {
        const storedToken = localStorage.getItem(TOKEN_KEY);
        if (!storedToken) {
            setReady(true);
            return;
        }

        fetch('/auth/me', {
            headers: { Authorization: `Bearer ${storedToken}` },
        })
            .then(res => {
                if (!res.ok) throw new Error('token invalid');
                return res.json() as Promise<AuthUser>;
            })
            .then(u => {
                setToken(storedToken);
                setUser(u);
                localStorage.setItem(USER_KEY, JSON.stringify(u));
            })
            .catch(() => {
                // Token expired or invalid — clear it
                localStorage.removeItem(TOKEN_KEY);
                localStorage.removeItem(USER_KEY);
            })
            .finally(() => setReady(true));
    }, []);

    const login = useCallback(async (identifier: string, password: string) => {
        const res = await fetch('/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: identifier, password }),
        });
        if (!res.ok) {
            const text = await res.text();
            throw new Error(text.trim() || 'Login failed');
        }
        const data = await res.json() as { token: string; user: AuthUser };
        localStorage.setItem(TOKEN_KEY, data.token);
        localStorage.setItem(USER_KEY, JSON.stringify(data.user));
        setToken(data.token);
        setUser(data.user);
    }, []);

    const logout = useCallback(() => {
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
            isAuthenticated: !!token,
            hasPermission, canAccess,
        }}>
            {children}
        </AuthContext.Provider>
    );
}

/** Read the stored token outside of React context (e.g. in api.ts fetch helpers) */
export function getStoredToken(): string | null {
    if (typeof window === 'undefined') return null;
    return localStorage.getItem(TOKEN_KEY);
}
