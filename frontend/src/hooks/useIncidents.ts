// ── Incidents React Query Hooks ──

import { useQuery } from '@tanstack/react-query';
import { getIncidents, getIncident } from '@/lib/ironsight-api';
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
    // `?? []` belt-and-suspenders: the API marshalled a nil slice as
    // literal null for customer/site-manager scopes, which crashed
    // every consumer doing `incidents.filter(...)` (portal error
    // boundary). The backend now returns [], but a null from any
    // older deploy must never take the portal down again.
    queryFn: async () => (await getIncidents(filters)) ?? [],
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

// F-09: useUpdateIncidentStatus / useAddComment removed alongside their
// client fns — the routes they called (PUT /api/v1/incidents/{id}/status,
// POST /api/v1/incidents/{id}/comments) never existed server-side and the
// hooks had no component callers.
