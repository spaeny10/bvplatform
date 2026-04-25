import Link from 'next/link';
import { BRAND } from '@/lib/branding';

// Customer-facing privacy notice. US-only deployment scope; see
// Documents/USCompliance.md for the regulatory rationale. This page
// is intentionally generic and high-level — the binding terms for
// any specific customer live in their executed Data Processing
// Addendum, not here.
//
// If the document below changes substantively, bump the
// LAST_UPDATED constant. Procurement teams compare timestamps when
// evaluating vendor-privacy posture.

const LAST_UPDATED = 'April 25, 2026';
const EFFECTIVE_DATE = 'April 25, 2026';

export const metadata = {
    title: `Privacy Notice — ${BRAND.name}`,
    description: 'How Ironsight handles personal data on behalf of its customers.',
};

export default function PrivacyPage() {
    return (
        <div className="legal-page">
            <div className="legal-shell">
                <div className="legal-breadcrumb">
                    <Link href="/portal">← Back to portal</Link>
                </div>

                <h1 className="legal-title">Privacy Notice</h1>
                <p className="legal-meta">
                    Last updated: {LAST_UPDATED} · Effective: {EFFECTIVE_DATE}
                </p>

                <Section title="1. Who we are">
                    <p>
                        {BRAND.name} (&ldquo;{BRAND.name},&rdquo; &ldquo;we,&rdquo; or &ldquo;us&rdquo;) provides
                        a security-monitoring and video-intelligence platform to
                        commercial customers. This Privacy Notice describes how
                        we handle personal data in connection with our services.
                    </p>
                    <p>
                        For purposes of US state privacy laws (CCPA/CPRA, VCDPA,
                        CPA, CTDPA, UCPA, TDPSA, OCPA, and similar regimes),
                        we typically act as a <em>service provider</em> or{' '}
                        <em>processor</em> on behalf of our customers, who are
                        the controllers of the surveillance data hosted on
                        their behalf.
                    </p>
                </Section>

                <Section title="2. Scope">
                    <p>
                        {BRAND.name} is operated for customers located in the
                        United States. We do not target the European Economic
                        Area, the United Kingdom, or other non-US jurisdictions,
                        and the General Data Protection Regulation (GDPR) and
                        UK GDPR do not apply to our services.
                    </p>
                </Section>

                <Section title="3. Information we process">
                    <p>
                        On behalf of our customers, our platform processes:
                    </p>
                    <ul>
                        <li>
                            <strong>Video and image data</strong> captured by
                            cameras at our customers&rsquo; sites, including
                            footage of individuals who appear in those scenes.
                        </li>
                        <li>
                            <strong>Event metadata</strong> generated from that
                            video — detection bounding boxes, AI-generated
                            scene descriptions, alarm timestamps, and related
                            telemetry.
                        </li>
                        <li>
                            <strong>Customer-user account data</strong> — names,
                            email addresses, phone numbers, role assignments,
                            and authentication credentials of the customer&rsquo;s
                            personnel who use the platform.
                        </li>
                        <li>
                            <strong>Operational logs</strong> — audit-trail
                            records of who accessed what within the platform,
                            for security and chain-of-custody purposes.
                        </li>
                    </ul>
                    <p>
                        We do not collect biometric identifiers such as face
                        embeddings, voiceprints, or fingerprints. Audio
                        recording is disabled by default and may only be
                        enabled subject to applicable state-law consent
                        requirements.
                    </p>
                </Section>

                <Section title="4. How we use the information">
                    <p>
                        We process data only to provide the services our
                        customers have contracted for, including:
                    </p>
                    <ul>
                        <li>
                            Detecting and alerting on security events at
                            customer sites.
                        </li>
                        <li>
                            Generating evidence packages for incidents at the
                            customer&rsquo;s direction.
                        </li>
                        <li>Producing reports and analytics for the customer.</li>
                        <li>
                            Securing the platform, preventing abuse, and
                            maintaining audit trails.
                        </li>
                        <li>
                            Communicating with customer-authorized contacts
                            about events at their sites (email, SMS, in-app
                            notifications).
                        </li>
                    </ul>
                    <p>
                        We do not sell personal information. We do not share
                        personal information with third parties for
                        cross-context behavioral advertising.
                    </p>
                </Section>

                <Section title="5. Sub-processors">
                    <p>
                        We use a small number of US-based service providers to
                        deliver the platform. The current list is published at{' '}
                        <Link href="/portal/subprocessors">
                            /portal/subprocessors
                        </Link>{' '}
                        and is updated when changes occur.
                    </p>
                </Section>

                <Section title="6. Retention">
                    <p>
                        Recording retention is configured per customer site,
                        typically between 30 and 90 days. Audit-trail records
                        are retained for a minimum of 365 days as required by
                        UL 827B central-station alarm-monitoring standards.
                        Closed support-ticket conversations are purged 180
                        days after the last reply. Specific retention windows
                        are defined in each customer&rsquo;s Data Processing
                        Addendum.
                    </p>
                </Section>

                <Section title="7. Security">
                    <p>
                        We maintain administrative, technical, and physical
                        safeguards designed to protect the data on our
                        platform, including:
                    </p>
                    <ul>
                        <li>Encryption in transit (TLS) for all platform traffic.</li>
                        <li>
                            Multi-factor authentication for security-operator
                            and administrator roles.
                        </li>
                        <li>
                            Role-based access control with per-organization
                            data isolation.
                        </li>
                        <li>
                            An append-only audit log capturing access and
                            administrative actions.
                        </li>
                        <li>
                            HMAC-signed evidence exports with chain-of-custody
                            tracking.
                        </li>
                        <li>
                            Account-lockout protections and rate-limited
                            authentication endpoints.
                        </li>
                    </ul>
                </Section>

                <Section title="8. Customer-personnel rights">
                    <p>
                        If you are an end-user of a customer&rsquo;s {BRAND.name}{' '}
                        deployment (for example, a site manager or contact),
                        the customer is generally responsible for honoring
                        your rights as a data subject under applicable US
                        state privacy laws. Direct requests about your account
                        to your customer&rsquo;s administrator. We will assist
                        the customer in fulfilling such requests as required
                        by their agreement with us.
                    </p>
                </Section>

                <Section title="9. Children">
                    <p>
                        The platform is a B2B security-monitoring product and
                        is not directed to children. We do not knowingly
                        collect personal information from children under 13.
                    </p>
                </Section>

                <Section title="10. Changes to this notice">
                    <p>
                        We may update this notice from time to time. The
                        &ldquo;Last updated&rdquo; date at the top reflects
                        the most recent revision. Material changes will be
                        communicated to customer administrators through the
                        platform.
                    </p>
                </Section>

                <Section title="11. Contact">
                    <p>
                        Questions about this notice? Email{' '}
                        <a href={`mailto:${BRAND.supportEmail}`}>
                            {BRAND.supportEmail}
                        </a>
                        .
                    </p>
                </Section>
            </div>
        </div>
    );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
    return (
        <section className="legal-section">
            <h2 className="legal-section-title">{title}</h2>
            <div className="legal-section-body">{children}</div>
        </section>
    );
}
