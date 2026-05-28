'use client';

// Admin API helpers extracted from app/admin/page.tsx (P1-B-11 session 2).
// These wrap authFetch with admin-page-specific ergonomics:
//   - apiFetch throws on non-2xx so handlers don't have to check res.ok
//   - useApiAction wraps a Promise<void> in success/error toast bracket
//
// Both are reusable from any admin sub-component (CompanyCard,
// SitesAndCustomersTab, future UsersTab extraction) — keeping them in
// the page file made every extraction drag a copy of these along.

import { authFetch } from './api';
import { useToast } from '@/components/ToastProvider';

/**
 * Throwing variant of authFetch — used by admin handlers that want
 * an Error with the server's response body for toast display, rather
 * than checking res.ok manually at every call site. JWT injection and
 * 401 → /login redirect are inherited from authFetch, so all three
 * fetch paths in the codebase (this, fetchJSON, raw authFetch) now
 * share the same auth layer.
 */
export async function apiFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers ?? {});
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json');
  }
  const res = await authFetch(url, { ...init, headers });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(body || `${init.method || 'GET'} ${url} failed (${res.status})`);
  }
  return res;
}

/**
 * Toast-wrapped action runner. Used by admin handlers to bracket
 * their apiFetch calls with consistent success/error notifications.
 * Returns true on success, false on error — callers that want
 * post-action UX (e.g. close a modal only on success) can branch.
 */
export function useApiAction(): (label: string, fn: () => Promise<void>) => Promise<boolean> {
  const toast = useToast();
  return async (label, fn) => {
    try {
      await fn();
      toast.push({ type: 'success', title: label, duration: 2500 });
      return true;
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      toast.push({ type: 'error', title: label + ' failed', body: msg, duration: 6000 });
      return false;
    }
  };
}
