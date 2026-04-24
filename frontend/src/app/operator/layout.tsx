import './operator.css';
import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';

export default function OperatorLayout({ children }: { children: ReactNode }) {
    return (
        <RouteGuard allowed={['soc_operator', 'soc_supervisor', 'admin']}>
            <div style={{ fontFamily: "var(--font-family, 'Inter', sans-serif)", height: '100vh', display: 'flex', flexDirection: 'column' }}>
                {children}
            </div>
        </RouteGuard>
    );
}
