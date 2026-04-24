// ── Portal Store ──
// Global UI state for the customer safety dashboard workflow.

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface PortalState {
  // Customer / org selection
  selectedCustomerId: string | null;
  selectCustomer: (id: string) => void;

  // Active site drill-down
  activeSiteId: string | null;
  selectSite: (id: string | null) => void;

  // Date range filter
  dateRange: { start: string; end: string };
  setDateRange: (range: { start: string; end: string }) => void;
}

function defaultDateRange() {
  const end = new Date();
  const start = new Date(end.getTime() - 7 * 86400000);
  return {
    start: start.toISOString().slice(0, 10),
    end: end.toISOString().slice(0, 10),
  };
}

export const usePortalStore = create<PortalState>()(
  persist(
    (set) => ({
      selectedCustomerId: null,
      selectCustomer: (id) => set({ selectedCustomerId: id }),

      activeSiteId: null,
      selectSite: (id) => set({ activeSiteId: id }),

      dateRange: defaultDateRange(),
      setDateRange: (range) => set({ dateRange: range }),
    }),
    {
      name: 'sg-portal-store',
      partialize: (state) => ({
        selectedCustomerId: state.selectedCustomerId,
        activeSiteId: state.activeSiteId,
        dateRange: state.dateRange,
      }),
    }
  )
);
