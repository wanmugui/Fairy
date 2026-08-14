import React from 'react';
import VoiceDock from '../features/voice/VoiceDock';

export default function VoicePage() {
  return (
    <div className="voice-page">
      <VoiceDock model="minimax-text-01" sessionName="voice-design" muted={false} onTurnComplete={() => {}} />
    </div>
  );
}
