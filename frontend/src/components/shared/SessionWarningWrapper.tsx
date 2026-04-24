'use client';

import { useSessionManager, SessionWarningModal } from '@/hooks/useSessionManager';
import { useAuth } from '@/contexts/AuthContext';

export default function SessionWarningWrapper() {
  const { showWarning, timeRemaining, extendSession } = useSessionManager();
  const { logout } = useAuth();

  return (
    <SessionWarningModal
      show={showWarning}
      timeRemaining={timeRemaining}
      onExtend={extendSession}
      onLogout={logout}
    />
  );
}
