import './portal.css';
import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';
import PortalMobileNav from '@/components/portal/PortalMobileNav';
import SupportWidget from '@/components/portal/SupportWidget';

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
                {/* SOC support chat widget — floating bubble bottom-right
                    that opens a slide-out panel for ticket history +
                    new messages. Hides itself for soc_operator role. */}
                <SupportWidget />
            </div>
        </RouteGuard>
    );
}
