// ── Search Store ──
// Global state for the semantic video search workflow.

import { create } from 'zustand';
import type { SearchResult, SearchFilters } from '@/types/ironsight';

interface SearchState {
  // Query input
  query: string;
  setQuery: (q: string) => void;

  // Filters
  filters: Omit<SearchFilters, 'query'>;
  updateFilters: (partial: Partial<Omit<SearchFilters, 'query'>>) => void;
  resetFilters: () => void;

  // Results
  results: SearchResult[];
  setResults: (results: SearchResult[]) => void;

  // Selection
  selectedResultId: string | null;
  selectResult: (id: string | null) => void;

  // Search state
  hasSearched: boolean;
  setHasSearched: (v: boolean) => void;
}

const DEFAULT_FILTERS: Omit<SearchFilters, 'query'> = {
  violation_types: [],
  site_ids: [],
  date_range: undefined,
  confidence_min: undefined,
  time_of_day: undefined,
  model: 'hybrid',
};

export const useSearchStore = create<SearchState>((set) => ({
  query: '',
  setQuery: (q) => set({ query: q }),

  filters: { ...DEFAULT_FILTERS },
  updateFilters: (partial) =>
    set((state) => ({ filters: { ...state.filters, ...partial } })),
  resetFilters: () => set({ filters: { ...DEFAULT_FILTERS } }),

  results: [],
  setResults: (results) => set({ results, selectedResultId: null }),

  selectedResultId: null,
  selectResult: (id) => set({ selectedResultId: id }),

  hasSearched: false,
  setHasSearched: (v) => set({ hasSearched: v }),
}));
