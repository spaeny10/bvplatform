'use client';

import { createContext, useContext, useCallback, useState, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';

export interface Toast {
    id: string;
    type: 'event' | 'success' | 'error' | 'info';
    eventType?: string;
    cameraName?: string;
    title: string;
    body?: string;
    onGo?: () => void;
    duration?: number; // ms, default 4000
}

interface ToastContextValue {
    push: (toast: Omit<Toast, 'id'>) => void;
    dismiss: (id: string) => void;
    enabled: boolean;
    setEnabled: (val: boolean) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

export function useToast() {
    const ctx = useContext(ToastContext);
    if (!ctx) throw new Error('useToast must be used within ToastProvider');
    return ctx;
}

// ── Colour palette matching event-type colours ──────────────────
const EVENT_COLORS: Record<string, string> = {
    motion: '#f59e0b',
    lpr: '#3b82f6',
    object: '#8b5cf6',
    face: '#ec4899',
    intrusion: '#ef4444',
    linecross: '#ef4444',
    tamper: '#f97316',
    videoloss: '#6b7280',
    success: '#22c55e',
    error: '#ef4444',
    info: '#6b7280',
};

const EVENT_ICONS: Record<string, string> = {
    motion: '🏃',
    lpr: '🚗',
    object: '📦',
    face: '👤',
    intrusion: '🚨',
    linecross: '⛔',
    tamper: '⚠️',
    videoloss: '📵',
    success: '✅',
    error: '❌',
    info: 'ℹ️',
};

// ── Individual Toast Item ────────────────────────────────────────
function ToastItem({ toast, onDismiss }: { toast: Toast & { visible: boolean }; onDismiss: (id: string) => void }) {
    const colorKey = toast.eventType ?? toast.type;
    const color = EVENT_COLORS[colorKey] ?? '#6b7280';
    const icon = EVENT_ICONS[colorKey] ?? 'ℹ️';

    return (
        <div
            role="alert"
            aria-live="assertive"
            style={{
                position: 'relative',
                display: 'flex',
                alignItems: 'flex-start',
                gap: 10,
                padding: '10px 12px',
                background: 'rgba(18,20,26,0.97)',
                border: `1px solid ${color}55`,
                borderLeft: `4px solid ${color}`,
                borderRadius: 10,
                boxShadow: `0 4px 24px rgba(0,0,0,0.5), 0 0 0 1px ${color}22`,
                backdropFilter: 'blur(12px)',
                minWidth: 260,
                maxWidth: 340,
                transform: toast.visible ? 'translateX(0)' : 'translateX(120%)',
                opacity: toast.visible ? 1 : 0,
                transition: 'transform 0.28s cubic-bezier(.22,1,.36,1), opacity 0.25s ease',
                pointerEvents: 'all',
            }}
        >
            {/* Icon */}
            <span style={{ fontSize: 18, lineHeight: 1.2, flexShrink: 0 }}>{icon}</span>

            {/* Content */}
            <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 2 }}>
                    <span style={{ fontWeight: 700, fontSize: 13, color: '#f0f4ff', letterSpacing: 0.2 }}>
                        {toast.title}
                    </span>
                    {toast.cameraName && (
                        <span style={{ fontSize: 10, background: `${color}22`, color, borderRadius: 4, padding: '1px 5px', flexShrink: 0 }}>
                            📷 {toast.cameraName}
                        </span>
                    )}
                </div>
                {toast.body && (
                    <div style={{ fontSize: 11, color: '#9ca3bb', lineHeight: 1.4 }}>{toast.body}</div>
                )}
                {toast.onGo && (
                    <button
                        onClick={() => { toast.onGo!(); onDismiss(toast.id); }}
                        style={{
                            marginTop: 5,
                            background: color,
                            border: 'none',
                            color: '#fff',
                            borderRadius: 5,
                            padding: '3px 10px',
                            fontSize: 11,
                            fontWeight: 700,
                            cursor: 'pointer',
                            letterSpacing: 0.3,
                        }}
                    >
                        Go ⏩
                    </button>
                )}
            </div>

            {/* Dismiss */}
            <button
                onClick={() => onDismiss(toast.id)}
                aria-label="Dismiss"
                style={{
                    background: 'transparent',
                    border: 'none',
                    color: '#6b7280',
                    cursor: 'pointer',
                    fontSize: 14,
                    padding: 0,
                    lineHeight: 1,
                    flexShrink: 0,
                }}
            >✕</button>

            {/* Progress bar */}
            <ProgressBar color={color} duration={toast.duration ?? 4000} onDone={() => onDismiss(toast.id)} />
        </div>
    );
}

function ProgressBar({ color, duration, onDone }: { color: string; duration: number; onDone: () => void }) {
    const [width, setWidth] = useState(100);
    const startRef = useRef(performance.now());
    const rafRef = useRef<number>(0);

    useEffect(() => {
        const tick = (now: number) => {
            const elapsed = now - startRef.current;
            const pct = Math.max(0, 100 - (elapsed / duration) * 100);
            setWidth(pct);
            if (pct > 0) {
                rafRef.current = requestAnimationFrame(tick);
            } else {
                onDone();
            }
        };
        rafRef.current = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(rafRef.current);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    return (
        <div style={{
            position: 'absolute', bottom: 0, left: 0, right: 0, height: 2,
            background: 'rgba(255,255,255,0.08)', borderRadius: '0 0 10px 10px', overflow: 'hidden',
        }}>
            <div style={{ height: '100%', width: `${width}%`, background: color, transition: 'width 0.1s linear' }} />
        </div>
    );
}

// ── Provider ─────────────────────────────────────────────────────
interface ToastWithVisible extends Toast { visible: boolean; }

export function ToastProvider({ children }: { children: React.ReactNode }) {
    const [toasts, setToasts] = useState<ToastWithVisible[]>([]);
    const [mounted, setMounted] = useState(false);
    const [enabled, setEnabledState] = useState(true);

    useEffect(() => {
        setMounted(true);
        // Read preference from localStorage
        const stored = localStorage.getItem('onvif_toasts_enabled');
        if (stored !== null) setEnabledState(stored !== 'false');
    }, []);

    const setEnabled = useCallback((val: boolean) => {
        setEnabledState(val);
        localStorage.setItem('onvif_toasts_enabled', String(val));
    }, []);

    const dismiss = useCallback((id: string) => {
        setToasts(prev => prev.map(t => t.id === id ? { ...t, visible: false } : t));
        setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 320);
    }, []);

    const push = useCallback((toast: Omit<Toast, 'id'>) => {
        // Skip if toasts are disabled
        if (!enabledRef.current) return;
        const id = Math.random().toString(36).slice(2);
        const item: ToastWithVisible = { ...toast, id, visible: false };
        setToasts(prev => [...prev, item]);
        requestAnimationFrame(() => {
            setToasts(prev => prev.map(t => t.id === id ? { ...t, visible: true } : t));
        });
    }, []);

    // Use ref so push callback doesn't need to depend on enabled state
    const enabledRef = useRef(enabled);
    useEffect(() => { enabledRef.current = enabled; }, [enabled]);

    const container = mounted ? createPortal(
        <div
            aria-label="Notifications"
            style={{
                position: 'fixed',
                top: 16,
                right: 16,
                zIndex: 9999,
                display: 'flex',
                flexDirection: 'column',
                gap: 8,
                alignItems: 'flex-end',
                pointerEvents: 'none',
            }}
        >
            {toasts.map(t => (
                <ToastItem key={t.id} toast={t} onDismiss={dismiss} />
            ))}
        </div>,
        document.body,
    ) : null;

    return (
        <ToastContext.Provider value={{ push, dismiss, enabled, setEnabled }}>
            {children}
            {container}
        </ToastContext.Provider>
    );
}
