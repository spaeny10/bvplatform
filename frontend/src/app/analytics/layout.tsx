import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';

export default function AnalyticsLayout({ children }: { children: ReactNode }) {
    return (
        <RouteGuard allowed={['soc_supervisor', 'admin', 'site_manager']}>
            {children}
        </RouteGuard>
    );
}
