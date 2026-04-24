import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';

export default function AdminLayout({ children }: { children: ReactNode }) {
    return (
        <RouteGuard allowed={['admin']}>
            {children}
        </RouteGuard>
    );
}
