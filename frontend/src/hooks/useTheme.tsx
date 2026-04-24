'use client';

import { createContext, useContext, useState, useEffect, useCallback } from 'react';

type Theme = 'light' | 'dark';

interface ThemeContextValue {
  theme: Theme;
  toggle: () => void;
  setTheme: (t: Theme) => void;
}

const ThemeContext = createContext<ThemeContextValue>({
  theme: 'light',
  toggle: () => {},
  setTheme: () => {},
});

export function useTheme() {
  return useContext(ThemeContext);
}

const DARK_OVERRIDES: Record<string, string> = {
  '--bg-base': '#080c10',
  '--bg-warm': '#0E1117',
  '--bg-card': '#0f1520',
  '--bg-card-hover': '#141c28',
  '--text-primary': '#E4E8F0',
  '--text-secondary': '#8891A5',
  '--text-dim': '#4A5268',
  '--border': 'rgba(255,255,255,0.06)',
  '--border-subtle': 'rgba(255,255,255,0.03)',
  '--accent': '#EF4444',
  '--green': '#22C55E',
  '--yellow': '#E89B2A',
  '--blue': '#E8732A',
  '--shadow-card': '0 2px 12px rgba(0,0,0,0.3)',
  '--shadow-hover': '0 6px 24px rgba(0,0,0,0.4)',
};

const LIGHT_DEFAULTS: Record<string, string> = {
  '--bg-base': '#f5f0e8',
  '--bg-warm': '#ede8dd',
  '--bg-card': '#ffffff',
  '--bg-card-hover': '#fafaf5',
  '--text-primary': '#2a2520',
  '--text-secondary': '#6b6560',
  '--text-dim': '#a09990',
  '--border': '#e0dbd0',
  '--border-subtle': '#f0ece4',
  '--accent': '#c84b2f',
  '--green': '#1a7a4a',
  '--yellow': '#d4a000',
  '--blue': '#2d7dd2',
  '--shadow-card': '0 1px 3px rgba(0,0,0,0.04)',
  '--shadow-hover': '0 4px 16px rgba(0,0,0,0.06)',
};

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<Theme>('light');

  useEffect(() => {
    const saved = localStorage.getItem('sg-theme') as Theme | null;
    if (saved === 'dark' || saved === 'light') {
      setThemeState(saved);
      applyTheme(saved);
    }
  }, []);

  const applyTheme = useCallback((t: Theme) => {
    const root = document.documentElement;
    const vars = t === 'dark' ? DARK_OVERRIDES : LIGHT_DEFAULTS;
    Object.entries(vars).forEach(([key, value]) => {
      root.style.setProperty(key, value);
    });
    root.setAttribute('data-theme', t);
  }, []);

  const setTheme = useCallback((t: Theme) => {
    setThemeState(t);
    localStorage.setItem('sg-theme', t);
    applyTheme(t);
  }, [applyTheme]);

  const toggle = useCallback(() => {
    setTheme(theme === 'light' ? 'dark' : 'light');
  }, [theme, setTheme]);

  return (
    <ThemeContext.Provider value={{ theme, toggle, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}

/** Standalone toggle button for use in any interface */
export function ThemeToggle({ style }: { style?: React.CSSProperties }) {
  const { theme, toggle } = useTheme();

  return (
    <button
      onClick={toggle}
      title={`Switch to ${theme === 'light' ? 'dark' : 'light'} mode`}
      style={{
        width: 32, height: 32, borderRadius: 6,
        background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.04)',
        border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.08)'}`,
        color: theme === 'dark' ? '#E89B2A' : '#6b6560',
        cursor: 'pointer', fontSize: 14,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        transition: 'all 0.2s',
        ...style,
      }}
    >
      {theme === 'dark' ? '☀️' : '🌙'}
    </button>
  );
}
