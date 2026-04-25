import './portal.css';
import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';
import PortalMobileNav from '@/components/portal/PortalMobileNav';
import SupportWidget from '@/components/portal/SupportWidget';
import PortalLegalFooter from '@/components/portal/PortalLegalFooter';

export default function PortalLayout({ children }: { children: ReactNode }) {
    return (
        <RouteGuard allowed={['site_manager', 'customer', 'soc_supervisor', 'admin']}>
            <div style={{ fontFamily: "var(--font-family, 'Inter', sans-serif)" }}>
                {children}
                {/* Procurement-readable legal links — privacy notice,
                    sub-processor list, support email. Rendered low-weight
                    below page content so it doesn't compete with
                    operational dashboards but is one click from any
                    portal page. */}
                <PortalLegalFooter />
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
