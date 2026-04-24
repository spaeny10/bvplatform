import type { ReactNode } from 'react';

export default function IncidentDetailLayout({ children }: { children: ReactNode }) {
    return (
        <div style={{ fontFamily: "var(--font-family, 'Inter', sans-serif)" }}>
            {children}
        </div>
    );
}
