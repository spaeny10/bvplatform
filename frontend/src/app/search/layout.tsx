import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';

export default function SearchLayout({ children }: { children: ReactNode }) {
    return (
        <RouteGuard allowed={['soc_supervisor', 'admin']}>
            <div style={{ fontFamily: "var(--font-family, 'Inter', sans-serif)" }}>
                {children}
            </div>
        </RouteGuard>
    );
}
