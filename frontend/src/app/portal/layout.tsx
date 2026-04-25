import './portal.css';
import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';
import PortalMobileNav from '@/components/portal/PortalMobileNav';

export default function PortalLayout({ children }: { children: ReactNode }) {
    return (
        <RouteGuard allowed={['site_manager', 'customer', 'soc_supervisor', 'admin']}>
            <div style={{ fontFamily: "var(--font-family, 'Inter', sans-serif)" }}>
                {children}
                {/* Mobile-only bottom-tab nav. Renders only at viewports
                    ≤ 640px via CSS (controlled inside the component).
                    Desktop keeps using each portal page's existing
                    top-bar navigation. */}
                <PortalMobileNav />
            </div>
        </RouteGuard>
    );
}
