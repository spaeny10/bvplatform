'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useAuth } from '@/contexts/AuthContext';
import { BRAND } from '@/lib/branding';
import SLAReportCard from '@/components/reports/SLAReportCard';
import VerificationQueueCard from '@/components/reports/VerificationQueueCard';
import EvidenceSharesCard from '@/components/reports/EvidenceSharesCard';
import SupportTicketsCard from '@/components/reports/SupportTicketsCard';

// Reports surface for SOC supervisors and admins.
//
// Tabs map 1:1 to the three new operational tracking surfaces we
// shipped today:
//   - Performance: GET /api/reports/sla
//   - Queue:       Verification queue (un-verified high-severity)
//   - Shares:      Per-incident evidence-share lifecycle
//
// Anything we add in the future (failed-login report, AVS-score
// distribution, recording-gap timeline) lands here as a new tab so
// supervisors don't have to learn another route.

type Tab = 'performance' | 'queue' | 'shares' | 'support';

const TABS: Array<{ key: Tab; label: string; subtitle: string }> = [
  { key: 'performance', label: 'Performance', subtitle: 'SLA response times' },
  { key: 'queue',       label: 'Verification', subtitle: 'Awaiting four-eyes' },
  { key: 'shares',      label: 'Evidence shares', subtitle: 'Public-link lifecycle' },
  { key: 'support',     label: 'Support', subtitle: 'Customer ticket inbox' },
];

export default function ReportsPage() {
  const { user } = useAuth();
  const [tab, setTab] = useState<Tab>('performance');

  return (
    <div className="reports-page">
      <header className="reports-header">
        <div className="reports-header-left">
          <Link href="/" className="reports-brand">
            {BRAND.name}
          </Link>
          <span className="reports-divider">/</span>
          <span className="reports-section">Reports</span>
        </div>
        <div className="reports-header-right">
          <span className="reports-user">
            {user?.display_name || user?.username} · {user?.role}
          </span>
        </div>
      </header>

      <nav className="reports-tabs" role="tablist">
        {TABS.map((t) => (
          <button
            key={t.key}
            role="tab"
            aria-selected={tab === t.key}
            className={`reports-tab ${tab === t.key ? 'active' : ''}`}
            onClick={() => setTab(t.key)}
          >
            <div className="reports-tab-label">{t.label}</div>
            <div className="reports-tab-subtitle">{t.subtitle}</div>
          </button>
        ))}
      </nav>

      <main className="reports-content">
        {tab === 'performance' && <SLAReportCard />}
        {tab === 'queue' && <VerificationQueueCard />}
        {tab === 'shares' && <EvidenceSharesCard />}
        {tab === 'support' && <SupportTicketsCard />}
      </main>
    </div>
  );
}
