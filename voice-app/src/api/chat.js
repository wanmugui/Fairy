// ── Voice app API helpers ──
const BASE = '/api';

export async function fetchModels() {
  const r = await fetch(BASE + '/models');
  return r.json();
}

export async function fetchSessions() {
  const r = await fetch(BASE + '/sessions');
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
    for (const block of message.contents) {
      if (block.type === 'text') {
        output.push({
          role: message.role,
          content: block.content || '',
          id: block.id, timestamp: block.timestamp,
          internal_type: block.internal_type,
          active_agent: block.active_agent,
          turn_id: message.turn_id,
          message_uuid: message.message_uuid,
          display_tag: message.display_tag,
        });
      }
    }
  }
  return output;
}

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