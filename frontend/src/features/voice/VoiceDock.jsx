import React, { useState, useRef, useEffect, useCallback } from 'react';
import { sendChat, normalizeServiceMessages, extractAssistantContent, fetchSessions, fetchSessionMessages, fetchModels } from '../../api/chat';

// TTS_ENABLED: mirrors App.jsx. false stops /voice/api/tts fetches when the
// local voice service (:8787) is not running.
const TTS_ENABLED = false;

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
  const [toolTrace, setToolTrace] = useState([]);
  const [draft, setDraft] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [promptOpen, setPromptOpen] = useState(false);
  const [agentOnline, setAgentOnline] = useState(null); // null = checking
  const [models, setModels] = useState([]);

  const micRef = useRef(null);
  const interimRef = useRef('');
  const audioCtxRef = useRef(null);
  const audioSourceRef = useRef(null);
  const scrollRef = useRef(null);
  const promptBodyRef = useRef(null);

  const loadSessions = () => {
    setSessionsLoading(true);
    fetchSessions().then(list => { setSessions(list || []); setSessionsLoading(false); }).catch(() => setSessionsLoading(false));
  };

  const handleSelectSession = (name) => {
    setMenuOpen(false);
    setTranscript([]);
    if (onSelectSession) onSelectSession(name);
  };
  useEffect(() => {
    fetchModels()
      .then(list => { setModels(list || []); setAgentOnline(true); })
      .catch(() => setAgentOnline(false));
  }, []);

  useEffect(() => {
    if (!promptOpen) return;
    const onWheel = e => {
      const el = promptBodyRef.current;
      if (!el) return;
      e.preventDefault();
      el.scrollTop += e.deltaY;
    };
    window.addEventListener('wheel', onWheel, { passive: false });
    return () => window.removeEventListener('wheel', onWheel);
  }, [promptOpen]);

  useEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [transcript, interim]);

  const speak = useCallback(async (text) => {
    if (!TTS_ENABLED || muted || !text) return;
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
    setToolTrace([]);
    let gotStreamed = false;
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
              if (ev.type === 'tool') {
                setToolTrace(t => [...t, ev]);
              } else if (ev.type === 'user') {
                setTranscript(t => [...t, { role: 'user', text: ev.text }]);
              } else if (ev.type === 'context') {
                setTranscript(t => [...t, { role: 'context', text: ev.label }]);
              } else if (ev.type === 'message') {
                gotStreamed = true;
                setTranscript(t => [...t, { role: 'fairy', text: ev.text }]);
              } else if (ev.type === 'done' && ev.messages) {
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
            if (ev.type === 'tool') {
              setToolTrace(t => [...t, ev]);
            } else if (ev.type === 'user') {
              setTranscript(t => [...t, { role: 'user', text: ev.text }]);
            } else if (ev.type === 'context') {
              setTranscript(t => [...t, { role: 'context', text: ev.label }]);
            } else if (ev.type === 'message') {
              gotStreamed = true;
              setTranscript(t => [...t, { role: 'fairy', text: ev.text }]);
            } else if (ev.type === 'done' && ev.messages) {
              const norm = normalizeServiceMessages(ev.messages);
              const last = [...norm].reverse().find(m => m.role === 'assistant' && m.content && m.content.trim());
              if (last) finalText = readableText(last.content);
            }
          } catch {}
        }
      }
      if (!finalText) finalText = '抱歉，主人，我这边没有拿到可用回复。';
      if (!gotStreamed) setTranscript(t => [...t, { role: 'fairy', text: finalText }]);
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

  const sendDraft = () => {
    const msg = draft.trim();
    if (!msg || status === 'thinking') return;
    setDraft('');
    runTurn(msg);
  };

  const openMenu = () => {
    const next = !menuOpen;
    setMenuOpen(next);
    if (next) setMenuPanel('current');
  };

  const techStatus = STATUS_TECH[status] || 'STANDBY';

  return (
    <div className={`voice-dock voice-${status}`}>
      {/* ambient layers */}
      <div className="voice-dock-bg" aria-hidden="true" />
      <div className="voice-dock-texture" aria-hidden="true" />
      <div className="voice-tool-panel">
        <div className="voice-tool-panel-title">工具过程</div>
        <div className="voice-tool-panel-list">
          {toolTrace.length === 0 ? <div className="voice-tool-empty">等待 agent 工具调用...</div> : null}
          {toolTrace.map((t, i) => (
            <div key={i} className="voice-tool-item">
              <div className="voice-tool-item-head">
                <span className="voice-tool-name">{t.name}</span>
                <span className={`voice-tool-state${t.isError ? ' error' : ''}`}>{t.isError ? 'ERR' : 'OK'}</span>
              </div>
              <div className="voice-tool-args">{typeof t.arguments === 'string' ? t.arguments : JSON.stringify(t.arguments)}</div>
              {t.result ? <div className="voice-tool-result">{t.result}</div> : null}
            </div>
          ))}
        </div>
      </div>
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
        <button
          type="button"
          className={`voice-mic voice-mic-top${recording ? ' recording' : ''}`}
          onClick={toggleMic}
          disabled={status === 'thinking'}
          aria-label={recording ? '停止录音' : '开始语音对话'}
          title={recording ? '点击结束并发送' : '点击开始语音对话'}
        >
          <MicIcon />
        </button>
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
              <button type="button" className="voice-dock-menu-option" onClick={() => setMenuPanel('current')}>
                <span className="voice-dock-menu-option-title">Current</span>
                <span className="voice-dock-menu-option-meta">当前会话历史回复</span>
              </button>
              <button type="button" className="voice-dock-menu-option" onClick={() => setMenuPanel('config')}>
                <span className="voice-dock-menu-option-title">Config</span>
                <span className="voice-dock-menu-option-meta">模型 / 播报 / 连接</span>
              </button>
            </>
          ) : menuPanel === 'current' ? (
            <>
              <button type="button" className="voice-dock-menu-back" onClick={() => setMenuPanel('home')}>← Back</button>
              <div className="voice-dock-menu-title">Current Session</div>
              <div className="voice-dock-menu-history">
                {transcript.length === 0 ? <div className="voice-dock-menu-empty">暂无历史回复</div> : null}
                {transcript.map((m, i) => (
                  <div key={i} className={`voice-dock-history-item ${m.role}`}>
                    <span className="voice-dock-history-role">{m.role === 'user' ? 'YOU' : m.role === 'context' ? 'CTX' : 'FAIRY'}</span>
                    <span className="voice-dock-history-text">{m.text}</span>
                  </div>
                ))}
              </div>
            </>
          ) : menuPanel === 'session' ? (
            <>
              <button type="button" className="voice-dock-menu-back" onClick={() => setMenuPanel('home')}>← Back</button>
              <div className="voice-dock-menu-title">Session</div>
              <div className="voice-dock-menu-list">
                {sessionsLoading ? <div className="voice-dock-menu-empty">加载中...</div> : null}
                {!sessionsLoading && sessions.length === 0 ? <div className="voice-dock-menu-empty">暂无历史会话</div> : null}
                {sessions.map(s => (
                  <button key={s.name} type="button" className="voice-dock-menu-item" onClick={() => handleSelectSession(s.name)}>
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
                <div className="voice-dock-config-row voice-dock-config-prompt">
                  <span className="voice-tech">System Prompt</span>
                  <button type="button" className="voice-dock-prompt-open" onClick={() => setPromptOpen(true)}>
                    {systemPrompt ? '查看' : '暂无'}
                  </button>
                </div>
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
        {(() => {
          const lastFairy = [...transcript].reverse().find(m => m.role === 'fairy');
          if (!lastFairy) return <div className="voice-empty">等待 Fairy 回复...</div>;
          return (
            <div className="voice-line voice-line-fairy voice-line-current">
              <span className="voice-line-name">FAIRY</span>
              <span className="voice-line-text">{lastFairy.text}</span>
            </div>
          );
        })()}
        <div className="voice-dialog-input">
          <input
            value={draft}
            onChange={e => setDraft(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); sendDraft(); } }}
            placeholder="请输入你的指令..."
            disabled={status === 'thinking'}
          />
          <button type="button" onClick={sendDraft} disabled={!draft.trim() || status === 'thinking'} aria-label="发送">↓</button>
        </div>
      </div>

      {/* system prompt modal */}
      {promptOpen ? (
        <div className="voice-dock-modal" onClick={() => setPromptOpen(false)}>
          <div className="voice-dock-modal-box" onClick={e => e.stopPropagation()}>
            <div className="voice-dock-modal-header">
              <span>System Prompt</span>
              <button type="button" onClick={() => setPromptOpen(false)} aria-label="关闭">×</button>
            </div>
            <div className="voice-dock-modal-body" ref={promptBodyRef}>
              {systemPrompt || '暂无'}
            </div>
          </div>
        </div>
      ) : null}

      {/* footer */}
      <footer className="voice-footer">
        <span className="voice-footer-item">Session // <b>{sessionName}</b></span>
        <span className="voice-footer-item voice-footer-right">Fairy · 3rd Gen Sequential AI · Ready</span>
      </footer>
    </div>
  );
}
