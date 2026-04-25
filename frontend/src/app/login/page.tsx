'use client';

import { useState, FormEvent } from 'react';
import { useAuth } from '@/contexts/AuthContext';
import { useRouter } from 'next/navigation';
import { BRAND } from '@/lib/branding';

// Generate floating particle data at module level so they don't re-render
const PARTICLES = Array.from({ length: 24 }, (_, i) => ({
    id: i,
    size: 3 + Math.random() * 5,
    left: Math.random() * 100,
    duration: 12 + Math.random() * 18,
    delay: Math.random() * 15,
    color: ['#E8732A', '#B22234', '#E89B2A'][i % 3],
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
                    {/* IRONSight Wordmark */}
                    <div className="login-logo-animated" style={{
                        fontSize: 32, fontWeight: 800, letterSpacing: '-0.03em',
                        color: '#B0B8C8', lineHeight: 1, marginBottom: 8,
                    }}>
                        IRON
                        <span style={{ position: 'relative', display: 'inline-block' }}>
                            <span style={{ color: '#E4E8F0' }}>S</span>
                            {/* 3 brand dots from logo */}
                            <span className="login-brand-dots" style={{
                                position: 'absolute', top: -5, left: '50%', transform: 'translateX(-50%)',
                                display: 'flex', gap: 3,
                            }}>
                                <span style={{ width: 5, height: 5, borderRadius: '50%', background: '#E8732A', boxShadow: '0 0 8px #E8732A60' }} />
                                <span style={{ width: 5, height: 5, borderRadius: '50%', background: '#B22234', boxShadow: '0 0 8px #B2223460' }} />
                                <span style={{ width: 5, height: 5, borderRadius: '50%', background: '#E89B2A', boxShadow: '0 0 8px #E89B2A60' }} />
                            </span>
                        </span>
                        <span style={{ color: '#E4E8F0' }}>ight</span>
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
