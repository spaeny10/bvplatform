// ── Sites React Query Hooks ──

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  getSites, getSite, getSiteCameras, getSiteCompliance,
} from '@/lib/ironsight-api';
import type { SiteSummary, SiteDetail, SiteCreate } from '@/types/ironsight';

/** Fetch all sites, polling every 30s */
export function useSites() {
  return useQuery<SiteSummary[]>({
    queryKey: ['sites'],
    queryFn: getSites,
    refetchInterval: 30_000,
    staleTime: 10_000,
  });
}

/** Fetch a single site detail, polling every 10s */
export function useSite(id: string | null) {
  return useQuery<SiteDetail>({
    queryKey: ['site', id],
    queryFn: () => getSite(id!),
    enabled: !!id,
    refetchInterval: 10_000,
    staleTime: 5_000,
  });
}

/** Fetch cameras for a site */
export function useSiteCameras(siteId: string | null) {
  return useQuery({
    queryKey: ['site-cameras', siteId],
    queryFn: () => getSiteCameras(siteId!),
    enabled: !!siteId,
  });
}

/** Fetch compliance history for a site */
export function useSiteCompliance(siteId: string | null) {
  return useQuery({
    queryKey: ['site-compliance', siteId],
    queryFn: () => getSiteCompliance(siteId!),
    enabled: !!siteId,
  });
}

/** Update an existing site */
export function useUpdateSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: SiteCreate }) => {
      const { updateSite } = await import('@/lib/ironsight-api');
      return updateSite(id, data);
    },
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: ['sites'] });
      queryClient.invalidateQueries({ queryKey: ['site', variables.id] });
    },
  });
}

/** Create a new site */
export function useCreateSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: SiteCreate) => {
      const { createSite } = await import('@/lib/ironsight-api');
      return createSite(data);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sites'] });
    },
  });
}

/** Delete a site */
export function useDeleteSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      const { deleteSite } = await import('@/lib/ironsight-api');
      return deleteSite(id);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sites'] });
    },
  });
}
