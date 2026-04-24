// ── Companies React Query Hooks ──

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { Company, CompanyUser } from '@/types/ironsight';

/** Fetch all companies */
export function useCompanies() {
  return useQuery<Company[]>({
    queryKey: ['companies'],
    queryFn: async () => {
      const { getCompanies } = await import('@/lib/ironsight-api');
      return getCompanies();
    },
    staleTime: 30_000,
  });
}

/** Fetch a single company */
export function useCompany(id: string | null) {
  return useQuery<Company>({
    queryKey: ['company', id],
    queryFn: async () => {
      const { getCompany } = await import('@/lib/ironsight-api');
      return getCompany(id!);
    },
    enabled: !!id,
  });
}

/** Fetch users belonging to a company */
export function useCompanyUsers(companyId: string | null) {
  return useQuery<CompanyUser[]>({
    queryKey: ['company-users', companyId],
    queryFn: async () => {
      const { getCompanyUsers } = await import('@/lib/ironsight-api');
      return getCompanyUsers(companyId!);
    },
    enabled: !!companyId,
  });
}

/** Create a new company */
export function useCreateCompany() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: Omit<Company, 'id' | 'created_at'>) => {
      const { createCompany } = await import('@/lib/ironsight-api');
      return createCompany(data);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['companies'] });
    },
  });
}
