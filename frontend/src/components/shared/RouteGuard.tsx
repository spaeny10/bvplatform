'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth, type UserRole } from '@/contexts/AuthContext';

interface Props {
  /** Roles that may view this route. Everyone else is redirected. */
  allowed: UserRole[];
  children: React.ReactNode;
}

/** Returns the natural home page for a given role. */
function roleHome(role: UserRole): string {
  if (role === 'soc_operator') return '/operator';
  if (role === 'soc_supervisor') return '/operator';
  if (role === 'site_manager' || role === 'customer') return '/portal';
  return '/';
}

/**
 * Wraps a route segment and redirects unauthorised users before rendering.
 * Place inside a layout so every page in the segment is protected.
 */
export default function RouteGuard({ allowed, children }: Props) {
  const { user, isAuthenticated } = useAuth();
  const router = useRouter();

  const permitted = isAuthenticated && !!user && allowed.includes(user.role);

  useEffect(() => {
    if (!isAuthenticated) {
      router.replace('/login');
    } else if (user && !allowed.includes(user.role)) {
      router.replace(roleHome(user.role));
    }
  }, [isAuthenticated, user, router, allowed]);

  // Don't flash restricted content while redirecting
  if (!permitted) return null;

  return <>{children}</>;
}
