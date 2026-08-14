import React, { useRef, useEffect, useMemo } from 'react';
import MessageBubble from './MessageBubble';
import { hasReportTag } from '../../api/chat';

// In one user interaction the agent may emit several <report> messages (e.g.
// an interim report then the final one). Keep only the last report per user
// turn; earlier report messages are suppressed so the UI shows one report.
function suppressDuplicateReports(messages) {
  const out = messages.map(m => ({ ...m, _suppressReport: false }));
  let lastUserIdx = -1;
  for (let i = 0; i < out.length; i++) {
    if (out[i].role === 'user') {
      lastUserIdx = i;
      continue;
    }
    if (out[i].role !== 'assistant' || !hasReportTag(out[i].content || '')) continue;
    // find the last assistant report message after lastUserIdx
    let lastReport = -1;
    for (let j = i; j < out.length; j++) {
      if (out[j].role === 'user') break;
      if (out[j].role === 'assistant' && hasReportTag(out[j].content || '')) lastReport = j;
    }
    if (lastReport > i) out[i]._suppressReport = true;
    i = lastReport > i ? lastReport - 1 : i;
  }
  return out;
}

export default function ChatArea({ messages, loading, mode, stats }) {
  const endRef = useRef(null);
  useEffect(() => {
    endRef.current ? endRef.current.scrollIntoView({ behavior: 'smooth' }) : null;
  }, [messages]);
  const displayMsgs = useMemo(() => {
    // Dedupe by message id: every session's production ids restart from 1, so
    // any foreign-session content that slips through would collide and render
    // as duplicate keys / mixed messages. Keep the first occurrence only.
    const seen = new Set();
    const deduped = [];
    for (const m of messages) {
      if (m && m.id != null) {
        const k = String(m.id);
        if (seen.has(k)) continue;
        seen.add(k);
      }
      deduped.push(m);
    }
    return suppressDuplicateReports(deduped);
  }, [messages]);
  return (
    <div className="messages" id="messages">
      {displayMsgs.length === 0 && <div className="empty">输入消息开始对话</div>}
      {displayMsgs.map((msg, i) => <MessageBubble key={msg.id != null ? 'id-' + String(msg.id) : 'i-' + i} msg={msg} mode={mode} />)}
      {loading && (
        <div className="msg msg-assistant">
          <div className="bubble assistant-text"><em>思考中...</em></div>
          <div className="stats-thinking">⏳ 计算中...</div>
        </div>
      )}
      {!loading && stats && stats.current && (
        <div className="stats-per-request">
          ↑ {stats.current.usage ? (stats.current.usage.prompt_tokens ?? '?') : '?'} in &nbsp;
          ↓ {stats.current.usage ? (stats.current.usage.completion_tokens ?? '?') : '?'} out &nbsp;
          ⏱ agent {stats.current.duration_ms ? (stats.current.duration_ms / 1000).toFixed(1) + 's' : '?'}
        </div>
      )}

      <div ref={endRef} />
    </div>
  );
}
