// ── Camera + Speaker Assignment React Query Hooks ──
// Master device registry + site assignment.

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { IRONSightCamera, CameraAssignment, PlatformSpeaker } from '@/types/ironsight';

function authHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : null;
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

/** Fetch ALL cameras from NVR master list + platform assignments.
 *  The NVR's /api/cameras is the source of truth for real, connected cameras.
 *  We merge in site_id/location from the platform registry so assignment state
 *  is visible alongside the actual hardware list. */
export function useMasterCameras() {
  return useQuery<IRONSightCamera[]>({
    queryKey: ['master-cameras'],
    queryFn: async () => {
      // 1. Fetch real NVR cameras (discovered + added via settings)
      let nvrCameras: any[] = [];
      try {
        const res = await fetch('/api/cameras', { headers: authHeaders() });
        const data = await res.json();
        nvrCameras = Array.isArray(data) ? data : [];
      } catch { /* NVR offline */ }

      // 2. Fetch platform camera registry (has site_id assignments)
      let platformCameras: IRONSightCamera[] = [];
      try {
        const res = await fetch('/api/v1/cameras', { headers: authHeaders() });
        const data = await res.json();
        platformCameras = Array.isArray(data) ? data : [];
      } catch { /* platform offline */ }

      // 3. Build a lookup of platform assignments by onvif_address or name
      const platformByAddr = new Map<string, IRONSightCamera>();
      const platformById = new Map<string, IRONSightCamera>();
      for (const pc of platformCameras) {
        if (pc.onvif_address) platformByAddr.set(pc.onvif_address, pc);
        platformById.set(pc.id, pc);
      }

      // 4. Merge: NVR cameras are the master list, enriched with assignment info
      const merged: IRONSightCamera[] = nvrCameras.map((nvr: any) => {
        const match = platformByAddr.get(nvr.onvif_address) || platformById.get(nvr.id);
        return {
          id: nvr.id,
          name: nvr.name || 'Camera',
          onvif_address: nvr.onvif_address || '',
          manufacturer: nvr.manufacturer || '',
          model: nvr.model || '',
          status: nvr.status || 'offline',
          recording: nvr.recording ?? false,
          site_id: match?.site_id || null,
          location: match?.location || '',
        } as IRONSightCamera;
      });

      // 5. Include platform-only cameras that aren't in the NVR (orphaned assignments)
      const nvrIds = new Set(nvrCameras.map((c: any) => c.id));
      const nvrAddrs = new Set(nvrCameras.map((c: any) => c.onvif_address).filter(Boolean));
      for (const pc of platformCameras) {
        if (!nvrIds.has(pc.id) && !(pc.onvif_address && nvrAddrs.has(pc.onvif_address))) {
          merged.push(pc);
        }
      }

      return merged;
    },
    staleTime: 10_000,
  });
}

/** Fetch ALL speakers from master registry (assigned + unassigned) */
export function useMasterSpeakers() {
  return useQuery<PlatformSpeaker[]>({
    queryKey: ['master-speakers'],
    queryFn: async () => {
      try {
        const { getAllSpeakers } = await import('@/lib/ironsight-api');
        return getAllSpeakers();
      } catch { return []; }
    },
    staleTime: 10_000,
  });
}

// Keep old name as alias for backward compat
export const useIRONSightCameras = useMasterCameras;

/** Create a new camera in master registry */
export function useCreateCamera() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: { name: string; onvif_address?: string; manufacturer?: string; model?: string }) => {
      const res = await fetch('/api/v1/cameras', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });
      return res.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['master-cameras'] });
    },
  });
}

/** Delete a camera from master registry */
export function useDeleteCamera() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (cameraId: string) => {
      await fetch(`/api/v1/cameras/${cameraId}`, { method: 'DELETE' });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['master-cameras'] });
      queryClient.invalidateQueries({ queryKey: ['sites'] });
    },
  });
}

/** Fetch camera assignments for a site */
export function useCameraAssignments(siteId: string | null) {
  return useQuery<CameraAssignment[]>({
    queryKey: ['camera-assignments', siteId],
    queryFn: async () => {
      const { getCameraAssignments } = await import('@/lib/ironsight-api');
      return getCameraAssignments(siteId!);
    },
    enabled: !!siteId,
  });
}

/** Assign a camera to a site */
export function useAssignCamera() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ siteId, cameraId, locationLabel }: { siteId: string; cameraId: string; locationLabel: string }) => {
      const { assignCameraToSite } = await import('@/lib/ironsight-api');
      return assignCameraToSite(siteId, cameraId, locationLabel);
    },
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ['camera-assignments', variables.siteId] });
      queryClient.invalidateQueries({ queryKey: ['master-cameras'] });
      queryClient.invalidateQueries({ queryKey: ['site', variables.siteId] });
      queryClient.invalidateQueries({ queryKey: ['sites'] });
    },
  });
}

/** Unassign a camera from a site (camera stays in master registry) */
export function useUnassignCamera() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ siteId, cameraId }: { siteId: string; cameraId: string }) => {
      const { unassignCamera } = await import('@/lib/ironsight-api');
      return unassignCamera(siteId, cameraId);
    },
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ['camera-assignments', variables.siteId] });
      queryClient.invalidateQueries({ queryKey: ['master-cameras'] });
      queryClient.invalidateQueries({ queryKey: ['site', variables.siteId] });
      queryClient.invalidateQueries({ queryKey: ['sites'] });
    },
  });
}

/** Assign a speaker to a site */
export function useAssignSpeaker() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ siteId, speakerId, locationLabel }: { siteId: string; speakerId: string; locationLabel: string }) => {
      const { assignSpeakerToSite } = await import('@/lib/ironsight-api');
      return assignSpeakerToSite(siteId, speakerId, locationLabel);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['master-speakers'] });
      queryClient.invalidateQueries({ queryKey: ['sites'] });
    },
  });
}

/** Unassign a speaker from a site */
export function useUnassignSpeaker() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ siteId, speakerId }: { siteId: string; speakerId: string }) => {
      const { unassignSpeaker } = await import('@/lib/ironsight-api');
      return unassignSpeaker(siteId, speakerId);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['master-speakers'] });
      queryClient.invalidateQueries({ queryKey: ['sites'] });
    },
  });
}
