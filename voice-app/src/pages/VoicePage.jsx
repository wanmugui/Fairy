import React, { useState } from 'react';
import VoiceDock from '../features/voice/VoiceDock';

export default function VoicePage() {
  const [sessionName, setSessionName] = useState('voice-design');
  const [model, setModel] = useState('minimax-text-01');
  return (
    <div className="voice-page">
      <VoiceDock
        model={model}
        sessionName={sessionName}
        muted={false}
        onTurnComplete={() => {}}
        onSelectSession={name => setSessionName(name)}
      />
    </div>
  );
}