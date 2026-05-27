'use client';

// Extracted from ActiveAlarmView.tsx (P1-B-11). Renders the
// "Was this accurate? Yes/No" feedback control under the AI assessment
// summary. Calls submitAIFeedback once and locks itself disabled after.

import { useState } from 'react';
import { useOperatorStore } from '@/stores/operator-store';

export default function AIFeedbackButtons({ alarmId }: { alarmId: string }) {
  const [feedback, setFeedback] = useState<'agreed' | 'disagreed' | null>(null);
  const addLogEntry = useOperatorStore(s => s.addActionLogEntry);

  const handleFeedback = async (agreed: boolean) => {
    setFeedback(agreed ? 'agreed' : 'disagreed');
    addLogEntry(`AI assessment: ${agreed ? 'agreed' : 'disagreed'}`, true);
    const { submitAIFeedback } = await import('@/lib/ironsight-api');
    submitAIFeedback(alarmId, agreed);
  };

  return (
    <div style={{ marginTop: 8, fontSize: 12, color: '#6B7590' }}>
      Was this accurate?{' '}
      <button
        onClick={() => handleFeedback(true)}
        disabled={feedback !== null}
        style={{
          color: feedback === 'agreed' ? '#8891A5' : '#6B7590',
          background: 'none', border: 'none', cursor: feedback !== null ? 'default' : 'pointer',
          textDecoration: 'underline', fontFamily: 'inherit', fontSize: 12,
          opacity: feedback === 'disagreed' ? 0.4 : 1,
        }}
      >
        Yes
      </button>
      {' · '}
      <button
        onClick={() => handleFeedback(false)}
        disabled={feedback !== null}
        style={{
          color: feedback === 'disagreed' ? '#8891A5' : '#6B7590',
          background: 'none', border: 'none', cursor: feedback !== null ? 'default' : 'pointer',
          textDecoration: 'underline', fontFamily: 'inherit', fontSize: 12,
          opacity: feedback === 'agreed' ? 0.4 : 1,
        }}
      >
        No
      </button>
      {feedback && <span style={{ color: '#4A5268', marginLeft: 8 }}>Recorded</span>}
    </div>
  );
}
