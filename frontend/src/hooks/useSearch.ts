// ── Search React Query Hook ──

import { useMutation } from '@tanstack/react-query';
import { searchFrames, getSearchSuggestions } from '@/lib/ironsight-api';
import { useSearchStore } from '@/stores/search-store';
import type { SearchFilters } from '@/types/ironsight';
import { useQuery } from '@tanstack/react-query';

/** Execute a semantic search (fires on submit, not onChange) */
export function useSearchMutation() {
  const setResults = useSearchStore((s) => s.setResults);
  const setHasSearched = useSearchStore((s) => s.setHasSearched);

  return useMutation({
    mutationFn: (filters: SearchFilters) => searchFrames(filters),
    onSuccess: (results) => {
      setResults(results);
      setHasSearched(true);
    },
  });
}

/** Debounced search suggestions */
export function useSuggestions(query: string) {
  return useQuery<string[]>({
    queryKey: ['search-suggestions', query],
    queryFn: () => getSearchSuggestions(query),
    enabled: query.length >= 2,
    staleTime: 30_000,
  });
}
