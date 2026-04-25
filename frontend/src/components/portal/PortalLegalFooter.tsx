import Link from 'next/link';
import { BRAND } from '@/lib/branding';

// Small footer rendered at the bottom of every portal page. Surfaces
// the procurement-readable artifacts (privacy notice, sub-processor
// list) alongside the support email — the kind of thing a corporate
// security questionnaire reviewer expects to find one click away
// from any product page.
//
// Visual weight is deliberately low — single line, dim color, small
// font — so it doesn't compete with operational content but is
// unambiguously present.

export default function PortalLegalFooter() {
    const year = new Date().getFullYear();
    return (
        <footer className="portal-legal-footer">
            <span className="portal-legal-footer-brand">
                © {year} {BRAND.name}
            </span>
            <span className="portal-legal-footer-sep">·</span>
            <Link href="/portal/privacy">Privacy</Link>
            <span className="portal-legal-footer-sep">·</span>
            <Link href="/portal/subprocessors">Sub-processors</Link>
            <span className="portal-legal-footer-sep">·</span>
            <a href={`mailto:${BRAND.supportEmail}`}>{BRAND.supportEmail}</a>
        </footer>
    );
}
