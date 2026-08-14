// ── API helpers ──
const BASE = '/api';

export async function fetchModels() {
  const r = await fetch(BASE + '/models');
  return r.json();
}

export async function fetchSessions() {
  const r = await fetch(BASE + '/sessions');
  return r.json();
}

export async function fetchSessionMessages(name) {
  const r = await fetch(BASE + '/sessions/' + encodeURIComponent(name) + '/messages');
  return r.json();
}

export async function createSession(name) {
  const r = await fetch(BASE + '/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ name }),
  });
  return r.json();
}

export async function deleteSession(name) {
  const r = await fetch(BASE + '/sessions/' + encodeURIComponent(name), { method: 'DELETE' });
  return r.json();
}

export async function sendChat(message, model, session) {
  const r = await fetch(BASE + '/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ message, model, session, stream: true }),
  });
  return r;
}

// ── Normalize production format → flat messages ──
function parseToolCalls(value) {
  if (!value) return [];
  if (Array.isArray(value)) return value;
  try { return JSON.parse(value); } catch { return []; }
}

export function normalizeServiceMessages(payload) {
  const source = Array.isArray(payload) ? payload :
    (payload && payload.data && Array.isArray(payload.data.messages)) ? payload.data.messages :
    (payload && Array.isArray(payload.messages)) ? payload.messages : [];
  const output = [];

  for (const message of source) {
    if (!Array.isArray(message.contents)) {
      const copy = { ...message };
      if (copy.tool_calls) copy.tool_calls = parseToolCalls(copy.tool_calls);
      output.push(copy);
      continue;
    }

    const blocks = message.contents;
    if (message.role !== 'assistant') {
      for (const block of blocks) {
        if (block.type === 'text') {
          output.push({
            role: message.role,
            content: block.content || '',
            id: block.id, timestamp: block.timestamp,
            internal_type: block.internal_type,
            active_agent: block.active_agent,
            turn_id: message.turn_id,
            message_uuid: message.message_uuid,
            display_tag: message.display_tag
          });
        }
      }
      continue;
    }

    let i = 0;
    while (i < blocks.length) {
      const block = blocks[i];
      if (block.type === 'text') {
        const calls = [];
        const results = [];
        let j = i + 1;
        while (j < blocks.length && blocks[j].type !== 'text') {
          if (blocks[j].type === 'tool_calls') calls.push(...parseToolCalls(blocks[j].tool_calls));
          if (blocks[j].type === 'tool_result') results.push(blocks[j]);
          j++;
        }
        output.push({
          role: 'assistant',
          content: block.content || '',
          tool_calls: calls,
          _pairedResults: results.map(r => ({
            tool_call_id: r.tool_call_id || '',
            name: r.name || '',
            content: r.content || ''
          })),
          is_final: calls.length === 0,
          id: block.id, timestamp: block.timestamp,
          internal_type: block.internal_type,
          active_agent: block.active_agent,
          turn_id: message.turn_id,
          message_uuid: message.message_uuid,
          display_tag: message.display_tag,
          usage: message.usage,
          duration_ms: message.duration_ms,
          real_ms: message.real_ms,
        });
        for (const result of results) {
          output.push({
            role: 'tool',
            content: result.content || '',
            tool_call_id: result.tool_call_id || '',
            name: result.name || '',
            _suppress: true, // already shown via assistant._pairedResults
            id: result.id, timestamp: result.timestamp,
            internal_type: result.internal_type,
            active_agent: result.active_agent
          });
        }
        i = j;
        continue;
      }

      if (block.type === 'tool_calls') {
        const calls = [];
        const results = [];
        let j = i;
        while (j < blocks.length && blocks[j].type !== 'text') {
          if (blocks[j].type === 'tool_calls') calls.push(...parseToolCalls(blocks[j].tool_calls));
          if (blocks[j].type === 'tool_result') results.push(blocks[j]);
          j++;
        }
        if (calls.length) {
          output.push({
            role: 'assistant',
            content: '',
            tool_calls: calls,
          _pairedResults: results.map(r => ({
            tool_call_id: r.tool_call_id || '',
            name: r.name || '',
            content: r.content || ''
          })),
            is_final: false,
            id: blocks[i] && blocks[i].id, timestamp: blocks[i] && blocks[i].timestamp,
            usage: message.usage,
            duration_ms: message.duration_ms,
            real_ms: message.real_ms
          });

        }
        for (const result of results) {
          output.push({
            role: 'tool',
            content: result.content || '',
            tool_call_id: result.tool_call_id || '',
            name: result.name || '',
            _suppress: true // already shown via assistant._pairedResults
          });
        }
        i = j;
        continue;
      }
      i++;
    }
  }
  return output;
}

// ── Content extraction helpers ──
export function extractAssistantContent(text) {
  if (!text) return text || '';
  const process = text.match(/<process(?:\s[^>]*)?>([\s\S]*?)<\/process>/i);
  if (process) {
    const msg = process[1].match(/<message(?:\s[^>]*)?>([\s\S]*?)<\/message>/i);
    return (msg ? msg[1] : process[1]).trim();
  }
  const behavior = text.match(/<behavior(?:\s[^>]*)?>([\s\S]*?)<\/behavior>/i);
  if (behavior) {
    const des = behavior[1].match(/<des(?:\s[^>]*)?>([\s\S]*?)<\/des>/i);
    return (des ? des[1] : behavior[1]).trim();
  }
  const report = text.match(/<report(?:\s[^>]*)?>([\s\S]*?)<\/report>/i);
  if (report) return report[1].trim();
  const legacy = text.match(/<response(?:\s[^>]*)?>([\s\S]*?)<\/response>/i);
  return legacy ? legacy[1].trim() : text;
}

export function hasBehaviorTag(text) {
  return /<behavior(?:\s[^>]*)?>/i.test(text);
}


export function hasProcessTag(text) {
  return /<process(?:\s[^>]*)?>/i.test(text);
}

export function hasReportTag(text) {
  return /<report(?:\s[^>]*)?>/i.test(text);
}

export function getBehaviorDes(text) {
  const m = text.match(/<des(?:\s[^>]*)?>([\s\S]*?)<\/des>/i);
  return m ? m[1].trim() : null;
}

export function hasSummaryTag(text) {
  return /<summary\b/i.test(text || '');
}

// Extract readable text from a <summary> message. Handles nested
// <summary><summary> wrapping plus inner tags like <key_knowledge> /
// <recent_actions>: converts block tags into headings and strips the rest.
export function extractSummaryContent(text) {
  if (!text) return '';
  let out = String(text);
  // Unwrap all summary layers.
  out = out.replace(/<\/?summary[^>]*>/gi, '');
  // Promote known inner sections to readable headings.
  out = out
    .replace(/<\/?key_knowledge\s*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n## 关键知识\n'))
    .replace(/<\/?recent_actions\s*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n## 最近操作\n'))
    .replace(/<\/?primary[^>]*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n### 主要需求\n'))
    .replace(/<\/?hard[^>]*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n### 硬性约束\n'))
    .replace(/<\/?pending[^>]*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n### 待办\n'))
    .replace(/<\/?current[^>]*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n### 当前进度\n'))
    .replace(/<\/?optional[^>]*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n### 可选下一步\n'))
    .replace(/<\/?user_directives[^>]*>/gi, (m) => (m.indexOf('/') >= 0 ? '\n' : '\n### 用户指示\n'));
  // Strip any remaining XML tags, collapse blank lines.
  out = out.replace(/<[^>]+>/g, ' ').replace(/[ \t]+/g, ' ').replace(/\n{3,}/g, '\n\n').trim();
  return out;
}
export function getProcessMessage(text) {
  const m = text.match(/<message(?:\s[^>]*)?>([\s\S]*?)<\/message>/i);
  return m ? m[1].trim() : null;
}

