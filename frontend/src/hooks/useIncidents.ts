// ── Incidents React Query Hooks ──

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  getIncidents, getIncident, updateIncidentStatus, addIncidentComment,
} from '@/lib/ironsight-api';
import type { IncidentSummary, IncidentDetail } from '@/types/ironsight';

/** Fetch incidents with optional filters */
export function useIncidents(filters?: {
  site_id?: string;
  status?: string;
  severity?: string;
  limit?: number;
}) {
  return useQuery<IncidentSummary[]>({
    queryKey: ['incidents', filters],
    queryFn: () => getIncidents(filters),
    refetchInterval: 30_000,
    staleTime: 10_000,
  });
}

/** Fetch a single incident detail */
export function useIncident(id: string | null) {
  return useQuery<IncidentDetail>({
    queryKey: ['incident', id],
    queryFn: () => getIncident(id!),
    enabled: !!id,
    staleTime: 5_000,
  });
}

/** Update incident status */
export function useUpdateIncidentStatus() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, status, note }: { id: string; status: string; note?: string }) =>
      updateIncidentStatus(id, status, note),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ['incident', variables.id] });
      queryClient.invalidateQueries({ queryKey: ['incidents'] });
    },
  });
}

/** Add a comment to an incident */
export function useAddComment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, text }: { id: string; text: string }) =>
      addIncidentComment(id, text),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ['incident', variables.id] });
    },
  });
}
