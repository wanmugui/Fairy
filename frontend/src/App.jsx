import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import Sidebar from './components/Sidebar';
import ChatArea from './features/chat/ChatArea';
import { SubtaskStreamContext } from './components/ToolBlock';
import InputBar from './components/InputBar';
import VoiceDock from './features/voice/VoiceDock';
// import VersionNav from './components/VersionNav';  // temporarily disabled (results-only view)
import AskModal from './components/AskModal';
import PPTConfigModal from './components/PPTConfigModal';
import {
  fetchModels, fetchSessions, fetchSessionMessages,
  deleteSession, createSession, sendChat, normalizeServiceMessages, extractAssistantContent
} from './api/chat';

async function genSessionName() {
  // 命名格式: chat-YYYYMMDD-HHMMSS-N，与旧 Python 服务保持一致
  const d = new Date();
  const pad = n => String(n).padStart(2, '0');
  const dateStr = d.getFullYear() + pad(d.getMonth()+1) + pad(d.getDate());
  const timeStr = pad(d.getHours()) + pad(d.getMinutes()) + pad(d.getSeconds());
  const prefix = 'chat-' + dateStr + '-' + timeStr + '-';
  try {
    const sessions = await fetchSessions();
    const count = sessions.filter(s => s.name && s.name.startsWith(prefix)).length;
    return prefix + (count + 1);
  } catch {
    return prefix + '1';
  }
}

function escapePptConfigValue(value) {
  return String(value || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function createPptDeckID() {
  if (globalThis.crypto && typeof globalThis.crypto.randomUUID === 'function') {
    return 'pptid_' + globalThis.crypto.randomUUID();
  }
  return 'pptid_' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
}

function formatPptConfigMessage(config) {
  const deckDir = '/mnt/data/result/' + createPptDeckID();
  return `[用户已确认 PPT 制作参数]
<ppt_config>
  <role>${escapePptConfigValue(config.role)}</role>
  <scene>${escapePptConfigValue(config.scene)}</scene>
  <audience>${escapePptConfigValue(config.audience)}</audience>
  <page_count_desc>${escapePptConfigValue(config.page_count_desc)}</page_count_desc>
  <ppt_mode>${escapePptConfigValue(config.ppt_mode)}</ppt_mode>
${config.ppt_mode === 'template' ? `  <template_name>${escapePptConfigValue(config.template_name)}</template_name>
` : ''}  <deck_dir>${deckDir}</deck_dir>
</ppt_config>`;
}

function isPPTConfigAsk(ask) {
  return ask?.askType === 'ppt_mode.confirm_params' ||
    (ask?.questions || []).some(question => question?.id === 'ppt_mode.confirm_params');
}

export default function App() {
  const [mode, setMode] = useState('all-tags');
  const [models, setModels] = useState([]);
  const [selectedModel, setSelectedModel] = useState('');
  const [messages, setMessages] = useState([]);
  const [subtaskStreams, setSubtaskStreams] = useState({});
  const [sessions, setSessions] = useState([]);
  const [sessionName, setSessionName] = useState('');
  const [loading, setLoading] = useState(false);
  const [stats, setStats] = useState({ current: null, session: null });
  const [pendingAskUser, setPendingAskUser] = useState(null);
  const abortRef = useRef(null);
  const [muted, setMuted] = useState(false);
  const audioCtxRef = useRef(null);
  const audioSourceRef = useRef(null);
  const [voiceMode, setVoiceMode] = useState(false);
  const sendStartRef = useRef(0);
  const sessionNameRef = useRef('');
  const pollTokenRef = useRef(0);
  const loadTokenRef = useRef(0);
  const streamSessionRef = useRef('');
  const healTimerRef = useRef(null);
  const streamGenRef = useRef(0); // bumped on every switch/newChat to kill in-flight SSE streams
  const [sessionRealMs, setSessionRealMs] = useState(0);

  useEffect(() => {
    fetchModels().then(list => {
      setModels(list);
      if (list.length) setSelectedModel(list[0].id);
    });
    refreshSessions();
    genSessionName().then(setSessionName);
  }, []);

  // Show each turn's real wait time on its user message: real is stored on
  // the turn's final assistant message; map it back to the preceding user msg.
  const applyTurnRealToUsers = useCallback((msgs) => {
    let lastReal = null;
    for (let i = msgs.length - 1; i >= 0; i--) {
      const m = msgs[i];
      if (m && m.role === 'assistant' && m.real_ms) {
        lastReal = m.real_ms;
      } else if (m && m.role === 'user' && lastReal) {
        m.real_ms = lastReal;
        lastReal = null;
      }
    }
    return msgs;
  }, []);

  // Reflection flow may emit a draft <report> then the final <report> in the
  // same turn. Collapse to the last report and mark it as the updated final.
  const collapseDraftReports = useCallback((msgs) => {
    const out = [];
    let pending = [];
    const flush = () => {
      if (pending.length > 1) {
        const last = pending[pending.length - 1];
        out.push({ ...last, replacedDraft: true });
      } else {
        out.push(...pending);
      }
      pending = [];
    };
    for (const m of msgs) {
      if (m && m.role === 'user') { flush(); out.push(m); }
      else if (m && m.role === 'assistant' && /<report\b/i.test(m.content || '')) { pending.push(m); }
      else { flush(); out.push(m); }
    }
    flush();
    return out;
  }, []);

  const refreshSessions = useCallback(async () => {
    try { setSessions(await fetchSessions()); } catch { /* ignore */ }
  }, []);

  const loadSession = useCallback(async (name) => {
    const token = ++loadTokenRef.current;
    // Hard session refresh on switch: kill any leftover status poller / SSE
    // stream from the previously displayed session so old content can never
    // write into this view after the switch.
    pollTokenRef.current++;
    streamGenRef.current++;
    // Switch immediately so any in-flight stream from another session is
    // stopped by the session guard, and the sidebar highlight follows.
    sessionNameRef.current = name;
    setSessionName(name);
    setSubtaskStreams({}); // stale subtask cards from the previous session must not linger
    try {
      if (abortRef.current) abortRef.current.abort();
      setLoading(false);
      const res = await fetchSessionMessages(name);
      if (token !== loadTokenRef.current) return; // stale: a newer switch happened
      const msgs = normalizeServiceMessages(res);
      const savedModel = res && res.data && res.data.model;
      const savedUsage = res && res.data && res.data.usage;
      if (savedModel && models.length) {
        const stillValid = models.some(m => m.id === savedModel);
        if (stillValid) {
          setSelectedModel(prev => prev !== savedModel ? savedModel : prev);
        }
      }
      if (savedUsage) {
        setStats(prev => ({ ...prev, session: savedUsage }));
      }
      setSessionName(name);
      sessionNameRef.current = name;
      setMessages(applyTurnRealToUsers(collapseDraftReports(msgs)));
      const savedUsageReal = (savedUsage && Number(savedUsage.real_ms)) || 0;
      let localReal = 0;
      try { localReal = Number(localStorage.getItem('real_ms_' + name)) || 0; } catch {}
      setSessionRealMs(savedUsageReal || localReal);
      // Post-switch refresh: re-fetch the conversation shortly after so any
      // contamination from a dying stream/poller is wiped from the view.
      const loadStartSend = sendStartRef.current;
      setTimeout(async () => {
        if (token !== loadTokenRef.current) return; // switched again meanwhile
        if (sendStartRef.current !== loadStartSend) return; // a new send started
        try {
          const res2 = await fetchSessionMessages(name);
          if (token !== loadTokenRef.current) return;
          setMessages(applyTurnRealToUsers(collapseDraftReports(normalizeServiceMessages(res2))));
        } catch { /* ignore */ }
      }, 500);
      const sessUsage = res && res.data && res.data.usage;
      if (sessUsage) {
        setStats({ current: null, session: sessUsage });
      }
      // If this chat's agent is still running (switched away / refreshed),
      // show the thinking state and poll until it finishes.
      try {
        const st = await fetch('/api/sessions/' + encodeURIComponent(name) + '/status').then(r => r.json());
        if (st && st.running) {
          setLoading(true);
          const token = ++pollTokenRef.current;
          pollSessionStatus(name, token);
        }
      } catch { /* ignore */ }
    } catch { /* ignore */ }
  }, [models]);

  const pollSessionStatus = useCallback(async (name, token) => {
    const deadline = Date.now() + 30 * 60 * 1000; // max 30 min poll
    while (Date.now() < deadline) {
      await new Promise(r => setTimeout(r, 3000));
      if (token !== pollTokenRef.current) return; // switched to another chat
      try {
        const st = await fetch('/api/sessions/' + encodeURIComponent(name) + '/status').then(r => r.json());
        if (token !== pollTokenRef.current) return;
        if (st && st.running) {
          // Keep refreshing messages so new steps / subtask cards appear live.
          const res = await fetchSessionMessages(name);
          if (token !== pollTokenRef.current) return;
          setMessages(applyTurnRealToUsers(collapseDraftReports(normalizeServiceMessages(res))));
          continue;
        }
        // finished: reload once and clear loading
        const res = await fetchSessionMessages(name);
        if (token !== pollTokenRef.current) return;
        setMessages(applyTurnRealToUsers(collapseDraftReports(normalizeServiceMessages(res))));
        const us = res && res.data && res.data.usage;
        if (us) setStats(prev => ({ ...prev, session: us }));
        const usReal = (us && Number(us.real_ms)) || 0;
        let pollLocalReal = 0;
        try { pollLocalReal = Number(localStorage.getItem('real_ms_' + name)) || 0; } catch {}
        setSessionRealMs(usReal || pollLocalReal);
        setLoading(false);
        refreshSessions();
        return;
      } catch { /* keep polling */ }
    }
    if (token === pollTokenRef.current) setLoading(false);
  }, [refreshSessions]);


  const newChat = useCallback(async () => {
    const sess = await genSessionName();
    setSessionName(sess);
    sessionNameRef.current = sess;
    pollTokenRef.current++;
    streamGenRef.current++;
    setMessages([]);
    setSubtaskStreams({});
    setStats({ current: null, session: null });
    setSessionRealMs(0);
    try { await createSession(sess); } catch {}
    refreshSessions();
  }, [refreshSessions]);

  const delSession = useCallback(async (name) => {
    try {
      await deleteSession(name);
      if (sessionName === name) newChat();
      refreshSessions();
    } catch { /* ignore */ }
  }, [sessionName, newChat, refreshSessions]);

  const send = useCallback(async (text, forceSession) => {
    if (loading || !selectedModel) return;
    const sess = forceSession || sessionName || (await genSessionName());
    // Answering an ask_user from another session must first switch to that
    // session's history, otherwise the answer + stream would mix into the
    // currently displayed conversation.
    if (forceSession && forceSession !== sessionNameRef.current) {
      await loadSession(forceSession);
    }
    if (forceSession) {
      if (sessionName !== forceSession) setSessionName(forceSession);
      sessionNameRef.current = forceSession;
    } else if (!sessionName) {
      setSessionName(sess); sessionNameRef.current = sess;
    } else {
      sessionNameRef.current = sess;
    }

    const userMsg = { role: 'user', content: text };
    setMessages(prev => [...prev, userMsg]);
    sendStartRef.current = Date.now();
    setLoading(true);

    const controller = new AbortController();
    abortRef.current = controller;
    const myGen = streamGenRef.current;

    try {
      const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json; charset=utf-8' },
        body: JSON.stringify({ message: text, model: selectedModel, session: sess, stream: true }),
        signal: controller.signal,
      });

      if (!response.ok) {
        if (sessionNameRef.current === sess) {
          setMessages(prev => [...prev, { role: 'system', content: 'HTTP Error: ' + response.status }]);
        }
        return;
      }

      const contentType = response.headers.get('Content-Type') || '';
      if (contentType.indexOf('text/event-stream') >= 0) {
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let streamDone = false;

        while (!streamDone) {
          const { done, value } = await reader.read();
          streamDone = done;
          if (value) {
            buffer += decoder.decode(value, { stream: !streamDone });
            const lines = buffer.split('\n');
            buffer = lines.pop() || '';
            for (const line of lines) {
              if (line.startsWith('data: ')) {
                const raw = line.slice(6);
                if (raw === '[DONE]') continue;
                try {
                  const event = JSON.parse(raw);
                  if (streamGenRef.current !== myGen || sessionNameRef.current !== sess) { controller.abort(); streamDone = true; break; }
                  streamSessionRef.current = sess;
                  handleSSEEvent(event);
                } catch { /* skip */ }
              }
            }
          }
        }
        if (buffer.trim()) {
          const dataLine = buffer.trim();
          if (dataLine.startsWith('data: ')) {
            const raw = dataLine.slice(6);
            if (raw !== '[DONE]') {
              if (streamGenRef.current !== myGen || sessionNameRef.current !== sess) { controller.abort(); }
              else {
                streamSessionRef.current = sess;
                try { handleSSEEvent(JSON.parse(raw)); } catch { /* ignore */ }
              }
            }
          }
        }
      } else {
        try {
          const data = await response.json();
          if (sessionNameRef.current === sess) {
            if (data.data && data.data.messages) {
              setMessages(applyTurnRealToUsers(collapseDraftReports(normalizeServiceMessages(data))));
            } else if (data.error) {
              setMessages(prev => [...prev, { role: 'system', content: 'Error: ' + data.error }]);
            }
          }
        } catch { /* ignore */ }
      }
    } catch (err) {
      if (err.name !== 'AbortError' && sessionNameRef.current === sess) {
        setMessages(prev => [...prev, { role: 'system', content: 'Network Error: ' + err.message }]);
      }
    } finally {
      setLoading(false);
      abortRef.current = null;
      refreshSessions();
    }
  }, [loading, selectedModel, sessionName, refreshSessions, loadSession]);

  const triggerSelfHeal = useCallback(() => {
    const name = sessionNameRef.current;
    if (!name) return;
    if (healTimerRef.current) clearTimeout(healTimerRef.current);
    healTimerRef.current = setTimeout(() => {
      healTimerRef.current = null;
      loadSession(name);
    }, 300);
  }, [loadSession]);

  const handleSSEEvent = useCallback((event) => {
    // Ignore events that belong to a different session (the server tags every
    // SSE event with its session) ? bulletproof against cross-session mixing.
    if (event.session && event.session !== sessionNameRef.current) {
      // A foreign-session event leaked through: reload the current session to
      // wipe any contamination it may have added.
      triggerSelfHeal();
      return;
    }
    if ((event.contents && Array.isArray(event.contents)) ||
        (event.data && event.data.messages && Array.isArray(event.data.messages))) {
      setMessages(prev => [...prev, ...normalizeServiceMessages(event.data ? event : [event])]);
      return;
    }
    if (event.type === 'subtask_event') {
      const t = event.title || 'subtask';
      setSubtaskStreams(prev => ({
        ...prev,
        [t]: [...(prev[t] || []), event.event]
      }));
      return;
    }
    if (event.type === 'assistant') {
      const draft = !!event.draft;
      const isReport = /<report\b/i.test(event.content || '');
      setMessages(prev => {
        const copy = [...prev];
        if (isReport && !draft && copy.length) {
          // Replace the previous draft report in this turn with the final report.
          for (let i = copy.length - 1; i >= 0; i--) {
            const m = copy[i];
            if (m && m.role === 'user') break;
            if (m && m.role === 'assistant' && m.draft && /<report\b/i.test(m.content || '')) {
              copy[i] = { role: 'assistant', content: event.content || '', tool_calls: event.tool_calls || [], replacedDraft: true };
              return copy;
            }
          }
        }
        copy.push({ role: 'assistant', content: event.content || '', tool_calls: event.tool_calls || [], draft });
        return copy;
      });
    } else if (event.type === 'tool_result' || (event.type === 'tool_call' && event.status === 'end')) {
      setMessages(prev => {
        const copy = [...prev];
        const callId = event.call_id || event.tool_call_id || '';
        const name = event.tool || event.name || '';
        const content = event.result !== undefined ? event.result : (event.content || '');
        if (!content && !event.error) return copy;
        for (let i = copy.length - 1; i >= 0; i--) {
          const m = copy[i];
          if (m.role === 'assistant' && m.tool_calls && m.tool_calls.length > 0) {
            const matchIdx = callId
              ? m.tool_calls.findIndex(tc => (tc.id || tc.tool_call_id || '') === callId)
              : -1;
            if (matchIdx >= 0 || callId === '') {
              if (!m._streamResults) m._streamResults = [];
              m._streamResults.push({
                tool_call_id: callId,
                name,
                content: content || (event.error ? JSON.stringify({ error: event.error }) : '')
              });
              break;
            }
          }
        }
        return copy;
      });
    } else if (event.type === 'done') {
      const realMs = sendStartRef.current ? (Date.now() - sendStartRef.current) : 0;
      if (realMs > 0) {
        setSessionRealMs(prev => {
          const next = prev + realMs;
          if (sessionNameRef.current) {
            try { localStorage.setItem('real_ms_' + sessionNameRef.current, String(next)); } catch {}
            try {
              fetch('/api/sessions/' + encodeURIComponent(sessionNameRef.current) + '/real', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json; charset=utf-8' },
                body: JSON.stringify({ real_ms: realMs })
              }).catch(() => {});
            } catch {}
          }
          return next;
        });
      }
      if (event.usage || event.session_usage) {
        setStats(prev => ({
          ...prev,
          session: event.session_usage || event.usage || prev.session
        }));
      }
            if (event.messages) {
        const normalized = applyTurnRealToUsers(collapseDraftReports(normalizeServiceMessages(event.messages)));
        if (realMs > 0) {
          // live: stamp this turn's real onto its user message
          for (let i = normalized.length - 1; i >= 0; i--) {
            if (normalized[i].role === 'user') { normalized[i].real_ms = realMs; break; }
          }
        }
        setMessages(normalized);
        const lastAssistant = [...normalized].reverse().find(m => m.role === 'assistant' && m.content && m.content.trim());
        if (lastAssistant) {
          const content = lastAssistant.content || '';
          const report = content.match(/<report[^>]*>([\s\S]*?)<\/report>/i);
          const readable = (report ? report[1] : extractAssistantContent(content)).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
          if (readable) speak(readable);
        }
      }
    } else if (event.type === 'request_done') {
      const realMs = sendStartRef.current ? (Date.now() - sendStartRef.current) : 0;
      setStats(prev => ({
        current: {
          usage: event.usage || null,
          duration_ms: event.duration_ms || 0,
          real_ms: realMs || prev.current?.real_ms
        },
        session: event.session_usage || prev.session
      }));
    } else if (event.type === 'waiting_user_input') {
      setPendingAskUser({
        askType: event.ask_type,
        questions: event.questions || [],
        session: event.session || streamSessionRef.current || sessionNameRef.current || sessionName
      });
    } else if (event.type === 'error') {
      setMessages(prev => [...prev, { role: 'system', content: 'ERROR: ' + (event.error || '') }]);
    }
  }, [triggerSelfHeal]);

  // Download current session folder as zip
  const downloadSession = useCallback(() => {
    if (!sessionName) return;
    const a = document.createElement('a');
    a.href = '/api/sessions/' + encodeURIComponent(sessionName) + '/download';
    a.download = sessionName + '.zip';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }, [sessionName]);
  const handleAskComplete = useCallback((answers) => {
    const targetSession = pendingAskUser?.session || sessionNameRef.current;
    setPendingAskUser(null);
    if (answers.pptConfig) {
      send(formatPptConfigMessage(answers.pptConfig), targetSession);
      return;
    }
    // Confirmation mode (empty questions, user clicked "确认")
    if (answers.confirmed) {
      const msg = '[用户确认了] ' + (answers.askType || '');
      send(msg, targetSession);
      return;
    }
    // Question mode: format answers as text to send back to AI
    const qs = pendingAskUser?.questions || [];
    const lines = qs.map((q, qi) => {
      const qid = q.id || ('q' + (qi + 1));
      const selected = answers[qid] || [];
      const free = answers[qid + '_free_text'] || '';
      let txt = 'Q' + (qi + 1) + ': ' + (q.question || q.title || '');
      if (selected.length) txt += ' → 选择: ' + selected.join(', ');
      if (free) txt += ' | 补充: ' + free;
      return txt;
    });
    const msg = '[用户已回答了问题]\n' + lines.join('\n');
    send(msg, targetSession);
  }, [pendingAskUser, send]);

  const handleAskSkip = useCallback(() => {
    const targetSession = pendingAskUser?.session || sessionNameRef.current;
    setPendingAskUser(null);
    const msg = '[用户跳过了提问]';
    send(msg, targetSession);
  }, [send, pendingAskUser]);

  const abort = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    setLoading(false);
    // Dedicated stop event: ask the server to kill this session's agent (and
    // its whole subtask process tree). Always sent, even when there is no
    // in-flight SSE stream (e.g. loading restored via /status polling after
    // refresh/switch), so the Stop button never turns into a no-op.
    if (sessionNameRef.current) {
      try {
        fetch('/api/sessions/' + encodeURIComponent(sessionNameRef.current) + '/stop', { method: 'POST' }).catch(() => {});
      } catch {}
    }
  }, []);


  // Sum agent (LLM) time & tokens across this session AND every finished
  // create_subtask nested in its tool results, so the header shows the whole
  // tree, not just the main thread.
  const subtaskAgentStats = useMemo(() => {
    const sum = { prompt_tokens: 0, completion_tokens: 0, duration_ms: 0 };
    const absorbSubtask = (contentStr) => {
      let inner = null;
      try { inner = JSON.parse(contentStr || '{}'); } catch {}
      if (!inner) return;
      if (inner.agent_stats) {
        sum.prompt_tokens += inner.agent_stats.prompt_tokens || 0;
        sum.completion_tokens += inner.agent_stats.completion_tokens || 0;
        sum.duration_ms += inner.agent_stats.duration_ms || 0;
      } else if (Array.isArray(inner.messages)) {
        visit(inner.messages);
      }
    };
    // Fully count a SUBTASK session: its assistant durations/usage plus any
    // nested create_subtask results (used only for nested sessions).
    const visit = (list) => {
      for (const m of list || []) {
        if (!m) continue;
        if (m.role === 'assistant') {
          if (m.usage) {
            sum.prompt_tokens += m.usage.prompt_tokens || 0;
            sum.completion_tokens += m.usage.completion_tokens || 0;
          }
          if (m.duration_ms) sum.duration_ms += m.duration_ms;
          // server-loaded history nests tool results inside contents[]
          if (Array.isArray(m.contents)) {
            for (const blk of m.contents) {
              if (blk && blk.type === 'tool_result' && blk.name === 'create_subtask') {
                absorbSubtask(typeof blk.content === 'string' ? blk.content : JSON.stringify(blk.content || '{}'));
              }
            }
          }
        } else if (m.role === 'tool' && (m.name === 'create_subtask')) {
          absorbSubtask(typeof m.content === 'string' ? m.content : JSON.stringify(m.content || '{}'));
        }
      }
    };
    // Main thread: ONLY absorb subtask stats from create_subtask results.
    // Main-thread durations/tokens already live in stats.session (usage.json),
    // so they must NOT be added here (previously double-counted).
    for (const m of messages || []) {
      if (!m) continue;
      if (m.role === 'assistant' && Array.isArray(m.contents)) {
        for (const blk of m.contents) {
          if (blk && blk.type === 'tool_result' && blk.name === 'create_subtask') {
            absorbSubtask(typeof blk.content === 'string' ? blk.content : JSON.stringify(blk.content || '{}'));
          }
        }
      } else if (m.role === 'tool' && (m.name === 'create_subtask')) {
        absorbSubtask(typeof m.content === 'string' ? m.content : JSON.stringify(m.content || '{}'));
      }
    }
    return sum;
  }, [messages]);

  const speak = useCallback(async (text) => {
    if (voiceMode || muted || !text || !text.trim()) return;
    try {
      const r = await fetch('/voice/api/tts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json; charset=utf-8' },
        body: JSON.stringify({ text: text.trim().slice(0, 200) }),
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
      src.onended = () => { audioSourceRef.current = null; };
      audioSourceRef.current = src;
      src.start();
    } catch (e) { /* voice off is fine */ }
  }, [muted, voiceMode]);

  const sessionStatsTotal = useMemo(() => {
    const base = (stats && stats.session) || {};
    return {
      prompt_tokens: (base.prompt_tokens || 0) + subtaskAgentStats.prompt_tokens,
      completion_tokens: (base.completion_tokens || 0) + subtaskAgentStats.completion_tokens,
      duration_ms: (base.duration_ms || 0) + subtaskAgentStats.duration_ms,
    };
  }, [stats, subtaskAgentStats]);
  return (
    <div className="app">
      {/* <VersionNav mode={mode} onChange={setMode} /> temporarily disabled (results-only view) */}
      <Sidebar
        sessions={sessions}
        sessionName={sessionName}
        onSelect={loadSession}
        onNew={newChat}
        onDelete={delSession}
      />
      <div className={`main${voiceMode ? ' voice-active' : ''}`}>
        <header>
          <select value={selectedModel} onChange={e => setSelectedModel(e.target.value)}>
            {models.map(m => (
              <option key={m.id} value={m.id}>{m.display || m.id}</option>
            ))}
          </select>
          <button className="download-btn" onClick={downloadSession} title="下载本会话文件夹 (zip)">下载</button>
          <button className="voice-toggle" onClick={() => setMuted(m => !m)} title="语音播报开关">{muted ? (
            <>
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M11 5 6 9H2v6h4l5 4V5z"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/></svg>
            </>
            ) : (
            <>
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M11 5 6 9H2v6h4l5 4V5z"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/></svg>
            </>
            )}</button>
          <button className={`voice-enter${voiceMode ? ' active' : ''}`} onClick={() => setVoiceMode(v => !v)} title="退出实时语音对话"><svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/></svg><span>{voiceMode ? '退出语音' : '语音'}</span></button>
          <span className="session-name">{sessionName} · {messages.length} msgs</span>
          <span className="version-tag" title="frontend build">v2.10</span>
          {stats && stats.session && (
            <span className="stats-header">
              ↑{sessionStatsTotal.prompt_tokens ?? 0}  ↓{sessionStatsTotal.completion_tokens ?? 0}  ⏱ agent {sessionStatsTotal.duration_ms ? (sessionStatsTotal.duration_ms/1000).toFixed(1) + 's' : '0s'} (含子任务){sessionRealMs > 0 ? <>  ⏱ real {(sessionRealMs/1000).toFixed(1) + 's'}</> : null}
            </span>
          )}
        </header>
        <SubtaskStreamContext.Provider value={subtaskStreams}>
        <ChatArea key={sessionName || 'new'} messages={messages} loading={loading} mode={mode} stats={stats} />
      </SubtaskStreamContext.Provider>
        <InputBar onSend={send} loading={loading} onAbort={abort} />
        {voiceMode && (
          <VoiceDock
            model={selectedModel}
            sessionName={sessionNameRef.current || sessionName}
            muted={muted}
            onTurnComplete={refreshSessions}
            onSelectSession={name => { setVoiceMode(false); loadSession(name); }}
          />
        )}
        {pendingAskUser && (
          isPPTConfigAsk(pendingAskUser) ? (
            <PPTConfigModal onComplete={handleAskComplete} onSkipAll={handleAskSkip} />
          ) : (
            <AskModal
              questions={pendingAskUser.questions}
              askType={pendingAskUser.askType}
              onComplete={handleAskComplete}
              onSkipAll={handleAskSkip}
            />
          )
        )}
      </div>
    </div>
  );
}
