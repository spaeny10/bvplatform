import './reports.css';
import type { ReactNode } from 'react';
import RouteGuard from '@/components/shared/RouteGuard';

export default function ReportsLayout({ children }: { children: ReactNode }) {
  return (
    <RouteGuard allowed={['admin', 'soc_supervisor']}>
      <div style={{ fontFamily: "var(--font-family, 'Inter', sans-serif)" }}>
        {children}
      </div>
    </RouteGuard>
  );
}
