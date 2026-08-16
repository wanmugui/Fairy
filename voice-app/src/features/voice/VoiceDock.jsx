import React, { useState, useRef, useEffect, useCallback } from 'react';
import { sendChat, normalizeServiceMessages, extractAssistantContent, fetchSessions, fetchModels } from '../../api/chat';

function readableText(content) {
  if (!content) return '';
  const report = content.match(/<report[^>]*>([\s\S]*?)<\/report>/i);
  const base = report ? report[1] : extractAssistantContent(content);
  return base.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
}


const STATUS_TECH = {
  idle: 'STANDBY',
  listening: 'LISTENING',
  thinking: 'PROCESSING',
  speaking: 'SPEAKING',
};

function MicIcon() {
  return (
    <svg viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="9" y="3" width="6" height="11" rx="3" />
      <path d="M5 11a7 7 0 0 0 14 0" />
      <line x1="12" y1="18" x2="12" y2="21" />
    </svg>
  );
}

function SoundIcon() {
  return (
    <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
      <path d="M15.5 8.5a5 5 0 0 1 0 7" />
      <path d="M18.5 5.5a9 9 0 0 1 0 13" />
    </svg>
  );
}

function MutedIcon() {
  return (
    <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
      <line x1="22" y1="9" x2="16" y2="15" />
      <line x1="16" y1="9" x2="22" y2="15" />
    </svg>
  );
}

export default function VoiceDock({ model, sessionName, muted: mutedProp, onTurnComplete, onSelectSession }) {
  const [transcript, setTranscript] = useState([]);
  const [status, setStatus] = useState('idle');
  const [interim, setInterim] = useState('');
  const [recording, setRecording] = useState(false);
  const [muted, setMuted] = useState(Boolean(mutedProp));
  const [menuOpen, setMenuOpen] = useState(false);
  const [menuPanel, setMenuPanel] = useState('home');
  const [sessions, setSessions] = useState([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [agentOnline, setAgentOnline] = useState(null); // null = checking
  const [models, setModels] = useState([]);

  const micRef = useRef(null);
  const interimRef = useRef('');
  const audioCtxRef = useRef(null);
  const audioSourceRef = useRef(null);
  const scrollRef = useRef(null);

  // Probe the agent API so the HUD can show link state.
  useEffect(() => {
    fetchModels()
      .then(list => { setModels(list || []); setAgentOnline(true); })
      .catch(() => setAgentOnline(false));
  }, []);

  useEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [transcript, interim]);

  const speak = useCallback(async (text) => {
    if (muted || !text) return;
    try {
      const r = await fetch('/voice/api/tts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json; charset=utf-8' },
        body: JSON.stringify({ text: text.slice(0, 200) }),
      });
      const d = await r.json();
      if (!d.audio_b64) return;
      const raw = Uint8Array.from(atob(d.audio_b64), ch => ch.charCodeAt(0));
      const f32 = new Float32Array(raw.buffer);
      const ctx = audioCtxRef.current || (audioCtxRef.current = new (window.AudioContext || window.webkitAudioContext)());
      if (ctx.state === 'suspended') ctx.resume();
      const buf = ctx.createBuffer(2, f32.length / 2, d.sample_rate || 48000);
      for (let ch = 0; ch < 2; ch++) {
        const data = buf.getChannelData(ch);
        for (let i = 0; i < f32.length / 2; i++) data[i] = f32[i * 2 + ch];
      }
      if (audioSourceRef.current) { try { audioSourceRef.current.stop(); } catch {} }
      const src = ctx.createBufferSource();
      src.buffer = buf;
      src.connect(ctx.destination);
      src.onended = () => {
        audioSourceRef.current = null;
        setStatus(s => (s === 'speaking' ? 'idle' : s));
      };
      audioSourceRef.current = src;
      setStatus('speaking');
      src.start();
    } catch (e) { setStatus('idle'); }
  }, [muted]);

  const runTurn = useCallback(async (userText) => {
    setTranscript(t => [...t, { role: 'user', text: userText }]);
    setStatus('thinking');
    try {
      const res = await sendChat(userText, model, sessionName);
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let finalText = '';
      let done = false;
      while (!done) {
        const { done: rd, value } = await reader.read();
        done = rd;
        if (value) {
          buffer += decoder.decode(value, { stream: !done });
          const lines = buffer.split('\n');
          buffer = lines.pop() || '';
          for (const line of lines) {
            const t = line.trim();
            if (!t.startsWith('data: ')) continue;
            const raw = t.slice(6);
            if (raw === '[DONE]') continue;
            try {
              const ev = JSON.parse(raw);
              if (ev.type === 'done' && ev.messages) {
                const norm = normalizeServiceMessages(ev.messages);
                const last = [...norm].reverse().find(m => m.role === 'assistant' && m.content && m.content.trim());
                if (last) finalText = readableText(last.content);
              }
            } catch {}
          }
        }
      }
      const dataLine = buffer.trim();
      if (dataLine.startsWith('data: ')) {
        const raw = dataLine.slice(6);
        if (raw !== '[DONE]') {
          try {
            const ev = JSON.parse(raw);
            if (ev.type === 'done' && ev.messages) {
              const norm = normalizeServiceMessages(ev.messages);
              const last = [...norm].reverse().find(m => m.role === 'assistant' && m.content && m.content.trim());
              if (last) finalText = readableText(last.content);
            }
          } catch {}
        }
      }
      if (!finalText) finalText = '抱歉，主人，我这边没有拿到可用回复。';
      setTranscript(t => [...t, { role: 'fairy', text: finalText }]);
      if (onTurnComplete) onTurnComplete();
      await speak(finalText);
      setStatus('idle');
    } catch (e) {
      setTranscript(t => [...t, { role: 'fairy', text: '网络或服务出错了：' + e }]);
      setStatus('idle');
    }
  }, [model, sessionName, onTurnComplete, speak]);

  const stopMic = () => {
    const m = micRef.current;
    if (!m) return;
    if (m.ws && m.ws.readyState === 1) m.ws.send(JSON.stringify({ type: 'end' }));
    setTimeout(() => {
      try { if (m.processor) m.processor.disconnect(); } catch {}
      try { if (m.source) m.source.disconnect(); } catch {}
      try { if (m.stream) m.stream.getTracks().forEach(t => t.stop()); } catch {}
      try { if (m.ctx && m.ctx.state !== 'closed') m.ctx.close(); } catch {}
      try { if (m.ws) m.ws.close(); } catch {}
      micRef.current = null;
      setRecording(false);
      const t = (m.finalText || interimRef.current || '').trim();
      interimRef.current = '';
      setInterim('');
      if (t) runTurn(t);
      else setStatus('idle');
    }, 450);
  };

  const startMic = () => {
    if (micRef.current || status === 'thinking') return;
    if (!navigator.mediaDevices || !window.AudioContext) return;
    const ws = new WebSocket(`ws://${location.hostname}:5173/voice-ws`);
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    let stream = null, source = null, processor = null;
    ws.onmessage = e => {
      try {
        const d = JSON.parse(e.data);
        if (d && d.text) {
          interimRef.current = d.text;
          setInterim(d.text);
          if (d.type === 'final' && micRef.current) micRef.current.finalText = d.text;
        }
      } catch {}
    };
    micRef.current = { ws, ctx, finalText: '' };
    navigator.mediaDevices.getUserMedia({ audio: true })
      .then(ms => {
        stream = ms;
        source = ctx.createMediaStreamSource(ms);
        processor = ctx.createScriptProcessor(4096, 1, 1);
        const gain = ctx.createGain();
        gain.gain.value = 0;
        processor.onaudioprocess = ev => {
          const input = ev.inputBuffer.getChannelData(0);
          if (micRef.current && ws.readyState === 1) {
            const out = new Float32Array(Math.ceil(input.length / 3));
            for (let i = 0, j = 0; i < input.length; i += 3) out[j++] = input[i];
            ws.send(out.buffer);
          }
        };
        source.connect(processor);
        processor.connect(gain);
        gain.connect(ctx.destination);
        micRef.current.stream = ms;
        micRef.current.source = source;
        micRef.current.processor = processor;
      })
      .catch(() => stopMic());
    setRecording(true);
    setStatus('listening');
    setInterim('');
  };

  const toggleMic = useCallback(() => {
    if (status === 'thinking') return;
    if (recording || micRef.current) stopMic();
    else startMic();
  }, [status, recording]);

  const openMenu = () => {
    const next = !menuOpen;
    setMenuOpen(next);
    if (next) setMenuPanel('home');
  };

  const loadSessions = () => {
    setSessionsLoading(true);
    fetchSessions().then(list => { setSessions(list || []); setSessionsLoading(false); }).catch(() => setSessionsLoading(false));
  };

  const techStatus = STATUS_TECH[status] || 'STANDBY';

  return (
    <div className={`voice-dock voice-${status}`}>
      {/* ambient layers */}
      <div className="voice-dock-bg" aria-hidden="true" />
      <div className="voice-dock-texture" aria-hidden="true" />
      <div className="voice-dock-web" aria-hidden="true" />
      <div className="voice-dock-grid" aria-hidden="true" />
      <div className="voice-dock-icon voice-dock-icon-01" aria-hidden="true" />
      <div className="voice-dock-strip voice-dock-strip-top" aria-hidden="true" />
      <div className="voice-dock-strip voice-dock-strip-bottom" aria-hidden="true" />
      <div className="voice-dock-film voice-dock-film-top" aria-hidden="true" />
      <div className="voice-dock-film voice-dock-film-bottom" aria-hidden="true" />
      <div className="voice-dock-vignette" aria-hidden="true" />

      {/* corner HUD brackets */}
      <span className="voice-hud voice-hud-tl" aria-hidden="true" />
      <span className="voice-hud voice-hud-tr" aria-hidden="true" />
      <span className="voice-hud voice-hud-bl" aria-hidden="true" />
      <span className="voice-hud voice-hud-br" aria-hidden="true" />

      {/* top HUD bar */}
      <header className="voice-hudbar">
        <div className="voice-hudbar-brand">
          <span className="voice-brand">FAIRY</span>
          <span className="voice-brand-sub">Voice Link // Agent System</span>
        </div>
        <div className="voice-hudbar-status">
          <span className="voice-tech">Sys.Status</span>
          <span className="voice-tech-value voice-tech-status">{techStatus}</span>
        </div>
        <div className="voice-hudbar-meta">
          <span className="voice-tech">Model</span>
          <span className="voice-tech-value">{model}</span>
          <span className="voice-tech-sep" />
          <span className="voice-tech">Agent</span>
          <span className={`voice-tech-value agent-link ${agentOnline === null ? '' : agentOnline ? 'on' : 'off'}`}>
            {agentOnline === null ? 'LINK…' : agentOnline ? 'ONLINE' : 'OFFLINE'}
          </span>
        </div>
        <button type="button" className={`voice-mute-btn${muted ? ' muted' : ''}`} onClick={() => setMuted(m => !m)} title="语音播报开关" aria-label="语音播报开关">
          {muted ? <MutedIcon /> : <SoundIcon />}
        </button>
        <button type="button" className={`voice-dock-button${menuOpen ? ' active' : ''}`} onClick={openMenu} aria-expanded={menuOpen} aria-label="Fairy 功能设置">
          <span className="voice-dock-button-dot" />
        </button>
      </header>

      {/* menu */}
      {menuOpen ? (
        <div className="voice-dock-menu">
          {menuPanel === 'home' ? (
            <>
              <button type="button" className="voice-dock-menu-option" onClick={() => { setMenuPanel('session'); if (sessions.length === 0) loadSessions(); }}>
                <span className="voice-dock-menu-option-title">Session</span>
                <span className="voice-dock-menu-option-meta">历史会话 / 切换语境</span>
              </button>
              <button type="button" className="voice-dock-menu-option" onClick={() => setMenuPanel('config')}>
                <span className="voice-dock-menu-option-title">Config</span>
                <span className="voice-dock-menu-option-meta">模型 / 播报 / 连接</span>
              </button>
            </>
          ) : menuPanel === 'session' ? (
            <>
              <button type="button" className="voice-dock-menu-back" onClick={() => setMenuPanel('home')}>← Back</button>
              <div className="voice-dock-menu-title">Session</div>
              <div className="voice-dock-menu-list">
                {sessionsLoading ? <div className="voice-dock-menu-empty">加载中...</div> : null}
                {!sessionsLoading && sessions.length === 0 ? <div className="voice-dock-menu-empty">暂无历史会话</div> : null}
                {sessions.map(s => (
                  <button key={s.name} type="button" className="voice-dock-menu-item" onClick={() => { setMenuOpen(false); if (onSelectSession) onSelectSession(s.name); }}>
                    <span className="voice-dock-menu-item-name">{s.name}</span>
                    <span className="voice-dock-menu-item-meta">{s.message_count} 条 · {s.modified ? s.modified.slice(11, 19) : ''}</span>
                  </button>
                ))}
              </div>
            </>
          ) : (
            <>
              <button type="button" className="voice-dock-menu-back" onClick={() => setMenuPanel('home')}>← Back</button>
              <div className="voice-dock-menu-title">Config</div>
              <div className="voice-dock-menu-config">
                <div className="voice-dock-config-row"><span className="voice-tech">Model</span><span className="voice-tech-value">{model}</span></div>
                <div className="voice-dock-config-row"><span className="voice-tech">Session</span><span className="voice-tech-value">{sessionName}</span></div>
                <div className="voice-dock-config-row"><span className="voice-tech">TTS Output</span>
                  <button type="button" className={`voice-config-toggle${muted ? ' off' : ''}`} onClick={() => setMuted(m => !m)}>{muted ? 'OFF' : 'ON'}</button>
                </div>
                <div className="voice-dock-config-row"><span className="voice-tech">Agent Link</span>
                  <span className={`voice-tech-value agent-link ${agentOnline === null ? '' : agentOnline ? 'on' : 'off'}`}>
                    {agentOnline === null ? 'LINK…' : agentOnline ? 'ONLINE' : 'OFFLINE'}
                  </span>
                </div>
                {models.length > 0 ? (
                  <div className="voice-dock-config-row voice-dock-config-models">
                    <span className="voice-tech">Available</span>
                    <span className="voice-tech-value">{models.map(m => m.id).join(' / ')}</span>
                  </div>
                ) : null}
              </div>
            </>
          )}
        </div>
      ) : null}

      {/* stage: rolltext + avatar + waveform */}
      <div className="voice-stage">
        <div className="voice-dock-rolltext" aria-hidden="true"><span>THE THIRD GENERATION SEQUENTIAL INTEGRATED UNIVERSAL ARTIFICIAL INTELLIGENCE · FAIRY</span></div>

        <div className="fairy-avatar" aria-label="Fairy 动态形象">
          <span className="fairy-layer fairy-wave" />
          <span className="fairy-layer fairy-bg" />
          <span className="fairy-layer fairy-wheel" />
          <span className="fairy-layer fairy-shine" />
          <span className="fairy-layer fairy-ring-lb" />
          <span className="fairy-layer fairy-ring-b" />
          <span className="fairy-layer fairy-pupil" />
          <span className="fairy-layer fairy-dot" />
        </div>

        {(status === 'speaking' || status === 'listening') ? (
          <div className="voice-waveform" aria-hidden="true">
            {Array.from({ length: 24 }).map((_, i) => <span key={i} style={{ animationDelay: `${(i % 8) * 0.08}s` }} />)}
          </div>
        ) : null}
      </div>

      {/* transcript dialog */}
      <div className="voice-dock-dialog" ref={scrollRef}>
        {transcript.length === 0 && !interim ? (
          <div className="voice-empty">等待指令 · 点击下方麦克风开始对话</div>
        ) : null}
        {transcript.map((m, i) => (
          <div key={i} className={`voice-line voice-line-${m.role}`}>
            <span className="voice-line-name">{m.role === 'user' ? 'YOU' : 'FAIRY'}</span>
            <span className="voice-line-text">{m.text}</span>
          </div>
        ))}
        {interim ? <div className="voice-line voice-line-interim"><span className="voice-line-name">YOU</span><span className="voice-line-text">{interim}…</span></div> : null}
      </div>

      {/* mic cluster */}
      <div className="voice-controls">
        <button
          type="button"
          className={`voice-mic${recording ? ' recording' : ''}`}
          onClick={toggleMic}
          disabled={status === 'thinking'}
          aria-label={recording ? '停止录音' : '开始语音对话'}
          title={recording ? '点击结束并发送' : '点击开始语音对话'}
        >
          <MicIcon />
        </button>
        <span className="voice-hint">
          {status === 'thinking' ? 'Fairy 正在处理…' : recording ? '聆听中 · 再次点击结束并发送' : '点击麦克风开始语音对话'}
        </span>
      </div>

      {/* footer */}
      <footer className="voice-footer">
        <span className="voice-footer-item">Session // <b>{sessionName}</b></span>
        <span className="voice-footer-item voice-footer-right">Fairy · 3rd Gen Sequential AI · Ready</span>
      </footer>
    </div>
  );
}
