'use client';

import { useState, FormEvent } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { useRouter } from 'next/navigation';
import { BRAND } from '@/lib/branding';
import Logo from '@/components/shared/Logo';

// Generate floating particle data at module level so they don't re-render.
// Particle colors are pulled from the brand palette so a rebrand to a
// different color scheme automatically updates the login screen ambience —
// no hand-editing of hex literals required. BRAND.colors gives us
// primary / accent2 / accent3; we cycle three particles through each.
const PARTICLE_COLORS = [
    BRAND.colors.primary,
    BRAND.colors.secondary,
    BRAND.colors.tertiary,
];
const PARTICLES = Array.from({ length: 24 }, (_, i) => ({
    id: i,
    size: 3 + Math.random() * 5,
    left: Math.random() * 100,
    duration: 12 + Math.random() * 18,
    delay: Math.random() * 15,
    color: PARTICLE_COLORS[i % PARTICLE_COLORS.length],
    opacity: 0.15 + Math.random() * 0.25,
}));

export default function LoginPage() {
    const { login } = useAuth();
    const router = useRouter();
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(false);

    const handleSubmit = async (e: FormEvent) => {
        e.preventDefault();
        setError('');
        setLoading(true);
        try {
            await login(username, password);
            // Role-based redirect after login
            const stored = localStorage.getItem('ironsight_user');
            const u = stored ? JSON.parse(stored) : null;
            const role = u?.role ?? '';
            if (role === 'soc_operator' || role === 'soc_supervisor') {
                router.replace('/operator');
            } else if (role === 'site_manager' || role === 'customer') {
                router.replace('/portal');
            } else {
                router.replace('/');
            }
        } catch (err: any) {
            setError(err.message ?? 'Login failed');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="login-page">
            {/* Floating brand-dot particles */}
            <div className="login-particles">
                {PARTICLES.map(p => (
                    <div
                        key={p.id}
                        className="login-particle"
                        style={{
                            width: p.size,
                            height: p.size,
                            left: `${p.left}%`,
                            bottom: '-10px',
                            background: p.color,
                            animationDuration: `${p.duration}s`,
                            animationDelay: `${p.delay}s`,
                            boxShadow: `0 0 ${p.size * 2}px ${p.color}40`,
                        }}
                    />
                ))}
            </div>

            <div className="login-card">
                {/* Logo */}
                <div className="login-logo">
                    {/*
                     * Wordmark routes through the shared Logo component, which
                     * derives the letters from BRAND.name and the accent dots
                     * from BRAND.colors. Two wins over the previous inline
                     * version: a future product rename Just Works (the old
                     * version hardcoded "IRON | S | ight" and would render
                     * wrong text after a rebrand), and the colors come from
                     * the brand palette rather than light/dark-incompatible
                     * hex literals on the page.
                     */}
                    <div className="login-logo-animated" style={{ marginBottom: 8 }}>
                        <Logo height={44} />
                    </div>
                    <div className="login-subtitle">{BRAND.tagline} — Sign in to continue</div>
                </div>

                <form className="login-form" onSubmit={handleSubmit}>
                    <label htmlFor="username">Username or Email</label>
                    <input
                        id="username"
                        type="text"
                        value={username}
                        onChange={e => setUsername(e.target.value)}
                        autoComplete="username"
                        autoFocus
                        required
                        placeholder="admin or user@example.com"
                    />

                    <label htmlFor="password">Password</label>
                    <input
                        id="password"
                        type="password"
                        value={password}
                        onChange={e => setPassword(e.target.value)}
                        autoComplete="current-password"
                        required
                        placeholder="••••••••"
                    />

                    {error && <div className="login-error">⚠ {error}</div>}

                    <button type="submit" className="btn-login" disabled={loading}>
                        {loading ? 'Signing in…' : 'Sign In'}
                    </button>
                </form>

                <p style={{ color: 'var(--text-muted)', fontSize: 12, textAlign: 'center', marginTop: 20 }}>
                    Default: admin / admin &nbsp;·&nbsp; SOC: jhayes / demo123 &nbsp;·&nbsp; Portal: marcus.webb / demo123
                </p>
            </div>
        </div>
    );
}
