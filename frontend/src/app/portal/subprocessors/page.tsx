import Link from 'next/link';
import { BRAND } from '@/lib/branding';

// Sub-processor disclosure. US procurement teams expect this even
// when not legally required (it's a GDPR-style artifact, but US B2B
// buyers ask for the same list). Keep the data here in code so a
// rebuild ships an updated list, and bump LAST_UPDATED on every
// change. See Documents/USCompliance.md §5 for the canonical list.

const LAST_UPDATED = 'April 25, 2026';

export const metadata = {
    title: `Sub-processors — ${BRAND.name}`,
    description: 'Third parties that may process customer data on behalf of Ironsight.',
};

interface SubProcessor {
    name: string;
    parent?: string;
    purpose: string;
    dataCategories: string;
    location: string;
    optional: boolean;
}

const SUBPROCESSORS: SubProcessor[] = [
    {
        name: 'SendGrid',
        parent: 'Twilio Inc.',
        purpose: 'Outbound email — alarm notifications, evidence-share links, support-ticket notifications, monthly summaries',
        dataCategories: 'Recipient email address, alarm metadata, ticket subject and message content, generated scene descriptions',
        location: 'United States',
        optional: false,
    },
    {
        name: 'Twilio',
        purpose: 'Outbound SMS — high-severity alarm notifications',
        dataCategories: 'Recipient phone number, short alarm summary text',
        location: 'United States',
        optional: true,
    },
    {
        name: 'Cloud infrastructure provider',
        purpose: 'Compute, storage, networking. The specific provider depends on the customer’s deployment topology and is named in their Data Processing Addendum.',
        dataCategories: 'All platform data (recordings, events, user accounts, audit logs)',
        location: 'United States',
        optional: false,
    },
    {
        name: 'Vision-language model (Qwen-VL)',
        purpose: 'AI-generated scene descriptions of alarm frames for inclusion in customer notifications',
        dataCategories: 'Frame thumbnails (transient input), derived natural-language descriptions',
        location: 'Self-hosted on customer or platform infrastructure (United States)',
        optional: true,
    },
];

export default function SubprocessorsPage() {
    return (
        <div className="legal-page">
            <div className="legal-shell">
                <div className="legal-breadcrumb">
                    <Link href="/portal">← Back to portal</Link>
                </div>

                <h1 className="legal-title">Sub-processors</h1>
                <p className="legal-meta">Last updated: {LAST_UPDATED}</p>

                <p className="legal-intro">
                    {BRAND.name} uses the following third-party service
                    providers to deliver the platform. All sub-processors are
                    located in the United States and bound by written
                    agreements that include confidentiality and security
                    obligations consistent with this disclosure.
                </p>

                <table className="legal-table">
                    <thead>
                        <tr>
                            <th>Provider</th>
                            <th>Purpose</th>
                            <th>Data categories</th>
                            <th>Location</th>
                            <th>Required?</th>
                        </tr>
                    </thead>
                    <tbody>
                        {SUBPROCESSORS.map((sp) => (
                            <tr key={sp.name}>
                                <td>
                                    <div className="legal-table-name">{sp.name}</div>
                                    {sp.parent && (
                                        <div className="legal-table-parent">
                                            (operated by {sp.parent})
                                        </div>
                                    )}
                                </td>
                                <td>{sp.purpose}</td>
                                <td>{sp.dataCategories}</td>
                                <td>{sp.location}</td>
                                <td>
                                    {sp.optional ? (
                                        <span className="legal-pill legal-pill-optional">
                                            Optional
                                        </span>
                                    ) : (
                                        <span className="legal-pill legal-pill-required">
                                            Required
                                        </span>
                                    )}
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>

                <h2 className="legal-section-title">Notes</h2>
                <ul className="legal-section-body">
                    <li>
                        <strong>Recordings are not transmitted to third
                        parties</strong> by the platform itself. Customers may
                        choose to share evidence packages externally, but the
                        recipient of any such share is the customer&rsquo;s
                        choice and is not a sub-processor of {BRAND.name}.
                    </li>
                    <li>
                        Optional sub-processors may be disabled per-customer
                        in the deployment configuration if a customer prefers
                        not to use that feature.
                    </li>
                    <li>
                        Material changes to this list will be communicated to
                        customer administrators with at least 30 days&rsquo;
                        notice, unless an emergency security replacement is
                        required.
                    </li>
                </ul>

                <h2 className="legal-section-title">Contact</h2>
                <p className="legal-section-body">
                    Questions about a specific sub-processor? Email{' '}
                    <a href={`mailto:${BRAND.supportEmail}`}>
                        {BRAND.supportEmail}
                    </a>
                    .
                </p>
            </div>
        </div>
    );
}
