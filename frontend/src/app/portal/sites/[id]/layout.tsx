import type { ReactNode } from 'react';

export default function SiteDrilldownLayout({ children }: { children: ReactNode }) {
    return (
        <div style={{ fontFamily: "var(--font-family, 'Inter', sans-serif)" }}>
            {children}
        </div>
    );
}
