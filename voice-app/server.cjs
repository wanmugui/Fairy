// Fairy Voice bridge server.
// Serves the built /voice page and backs /api/chat with DeepSeek Harness (dsh)
// headless + the Fairy persona patch. TTS/STT are proxied by the Vite dev
// server (see vite.config.js); in production the gateway handles them.
const http = require('http');
const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const APP_DIR = __dirname;
const REPO = path.resolve(APP_DIR, '..');                 // E:\Fairy-harness
const FAIRY_PATCH = path.join(REPO, 'fairy.patch.yml');
const DIST_DIR = path.join(APP_DIR, 'dist');
const DSH_CMD = process.platform === 'win32' ? 'pnpm' : 'pnpm';
const PORT = Number(process.env.VOICE_PORT || 8081);

const MODELS = [
  { id: 'minimax-text-01', display: 'MiniMax-Text-01', provider: 'minimax', model: 'MiniMax-Text-01' },
  { id: 'deepseek-chat',   display: 'DeepSeek-Chat',   provider: 'deepseek', model: 'deepseek-chat' },
];
const MODEL_BY_ID = Object.fromEntries(MODELS.map(m => [m.id, m]));


// Read provider keys from repo-root key files if not already in the environment.
function readKeyFile(name) {
  try {
    const p = path.join(REPO, name);
    if (fs.existsSync(p)) {
      const line = fs.readFileSync(p, 'utf-8').split('\n').map(s => s.trim()).find(l => l && !l.startsWith('#'));
      return line || undefined;
    }
  } catch {}
  return undefined;
}
function agentEnv() {
  const env = { ...process.env };
  if (!env.MINIMAX_API_KEY) { const k = readKeyFile('MINIMAX_key.txt'); if (k) env.MINIMAX_API_KEY = k; }
  if (!env.DEEPSEEK_API_KEY) { const k = readKeyFile('DEEPSEEK_key.txt'); if (k) env.DEEPSEEK_API_KEY = k; }
  return env;
}
// Session trace helpers: locate the newest dsh session log and turn its
// durable events into a lightweight tool/process trace for the UI.
const ZSTD_MAGIC = 4247762216;
function sessionRootPath() {
  return path.join(os.homedir(), '.dsh', 'sessions', '--' + REPO.replace(/[^A-Za-z0-9]+/g, '-') + '--');
}
function scanZstdFrames(buffer) {
  const frames = [];
  let offset = 0;
  while (offset < buffer.length) {
    const start = offset;
    if (buffer.length - offset < 4) break;
    if (buffer.readUInt32LE(offset) !== ZSTD_MAGIC) break;
    offset += 4;
    if (offset === buffer.length) break;
    const descriptor = buffer.readUInt8(offset);
    offset += 1;
    if ((descriptor & 24) !== 0) break;
    const contentSizeFlag = descriptor >>> 6;
    const singleSegment = (descriptor & 32) !== 0;
    const checksum = (descriptor & 4) !== 0;
    const dictionaryFlag = descriptor & 3;
    const dictionaryBytes = dictionaryFlag === 3 ? 4 : dictionaryFlag;
    const contentSizeBytes = contentSizeFlag === 0 ? (singleSegment ? 1 : 0) : 1 << contentSizeFlag;
    const remainingHeaderBytes = (singleSegment ? 0 : 1) + dictionaryBytes + contentSizeBytes;
    if (buffer.length - offset < remainingHeaderBytes) break;
    offset += remainingHeaderBytes;
    for (;;) {
      if (buffer.length - offset < 3) return { frames };
      const blockHeader = buffer.readUIntLE(offset, 3);
      offset += 3;
      const lastBlock = (blockHeader & 1) !== 0;
      const blockType = (blockHeader >>> 1) & 3;
      const blockSize = blockHeader >>> 3;
      if (blockType === 3) return { frames };
      const payloadBytes = blockType === 1 ? 1 : blockSize;
      if (buffer.length - offset < payloadBytes) return { frames };
      offset += payloadBytes;
      if (lastBlock) break;
    }
    if (checksum) {
      if (buffer.length - offset < 4) return { frames };
      offset += 4;
    }
    frames.push({ start, end: offset });
  }
  return { frames };
}
function decompressSessionLog(filePath) {
  const { zstdDecompressSync } = require('node:zlib');
  const data = fs.readFileSync(filePath);
  const { frames } = scanZstdFrames(data);
  let out = '';
  for (const frame of frames) {
    out += zstdDecompressSync(data.subarray(frame.start, frame.end)).toString('utf8');
  }
  return out;
}
function extractSessionTrace(root) {
  if (!fs.existsSync(root)) return [];
  const dirs = fs.readdirSync(root, { withFileTypes: true })
    .filter(d => d.isDirectory())
    .map(d => path.join(root, d.name));
  let newest = null;
  let newestMtime = 0;
  for (const dir of dirs) {
    const z = path.join(dir, 'session.jsonl.zstd');
    try {
      const st = fs.statSync(z);
      if (st.mtimeMs > newestMtime) { newestMtime = st.mtimeMs; newest = z; }
    } catch {}
  }
  if (!newest) return [];
  let text = '';
  try { text = decompressSessionLog(newest); } catch { return []; }
  const events = text.split('\n').map(l => { try { return JSON.parse(l); } catch { return null; } }).filter(Boolean);
  const trace = [];
  const calls = new Map();
  const hasToolCalls = events.some(e => e.type === 'tool/call');
  for (const ev of events) {
    if (ev.type === 'user/message') {
      const src = (ev.data && ev.data.source) || {};
      let uText = '';
      const uContent = ev.data && ev.data.content;
      if (Array.isArray(uContent)) {
        for (const c of uContent) if (c && c.type === 'text') uText += c.text;
      } else if (typeof uContent === 'string') {
        uText = uContent;
      }
      if (src.kind === 'agent-instructions' || /AGENTS\.md|CLAUDE\.md/.test(uText)) {
        if (!trace.some(i => i.kind === 'context' && i.label === 'AGENTS.md, CLAUDE.md')) {
          trace.push({ kind: 'context', label: 'AGENTS.md, CLAUDE.md' });
        }
      } else if (src.kind === 'plugin' && (String(src.plugin || '').includes('system-prompt') || uText.startsWith('Current runtime context'))) {
        if (!trace.some(i => i.kind === 'context' && i.label === '@deepseek-ai/dsh-system-prompt')) {
          trace.push({ kind: 'context', label: '@deepseek-ai/dsh-system-prompt' });
        }
      }
    } else if (ev.type === 'tool/call') {
      let args = ev.data.arguments || '';
      try { args = JSON.parse(args); } catch {}
      calls.set(ev.data.callId, { name: ev.data.name, arguments: args });
    } else if (ev.type === 'tool/result') {
      const msg = ev.data.message || {};
      let resultText = '';
      let isError = false;
      if (Array.isArray(msg.content)) {
        for (const c of msg.content) {
          if (c.type === 'tool-result') {
            isError = !!c.isError;
            if (Array.isArray(c.content)) {
              for (const t of c.content) if (t.type === 'text') resultText += t.text;
            } else if (typeof c.content === 'string') resultText += c.content;
          } else if (c.type === 'text') resultText += c.text;
        }
      } else if (typeof msg.content === 'string') resultText = msg.content;
      const callId = ev.data.callId || (msg.source && msg.source.callId);
      const call = calls.get(callId) || {};
      trace.push({ kind: 'tool', name: call.name || callId || 'tool', arguments: call.arguments || '', result: resultText.trim().slice(0, 1000), isError });
    } else if (ev.type === 'assistant/message') {
      const content = ev.data.message && ev.data.message.content;
      let msgText = '';
      if (Array.isArray(content)) {
        for (const c of content) if (c.type === 'text') msgText += c.text;
      } else if (typeof content === 'string') msgText = content;
      if (!msgText.trim() || /^\s*<system-reminder>/i.test(msgText)) continue;
      const pm = msgText.match(/<message>([\s\S]*?)<\/message>/i);
      const processMsg = pm ? pm[1].trim() : '';
      let finalText = msgText;
      const procEnd = msgText.lastIndexOf('</process>');
      if (procEnd !== -1) {
        const tail = msgText.slice(procEnd + '</process>'.length).trim();
        finalText = tail || '';
      }
      if (processMsg && hasToolCalls) trace.push({ kind: 'message', text: processMsg, process: true });
      if (finalText.trim() && finalText.trim() !== processMsg) trace.push({ kind: 'message', text: finalText.trim() });
    }
  }
  return trace;
}

// Run one dsh headless turn with the Fairy persona + chosen model.
function runAgent(task, modelId) {
  const m = MODEL_BY_ID[modelId] || MODEL_BY_ID['minimax-text-01'];
  const modelPatch = path.join(os.tmpdir(), `fairy_model_${Date.now()}_${Math.random().toString(36).slice(2)}.yml`);
  fs.writeFileSync(modelPatch, `- id: agent-default-model\n  config:\n    provider: ${m.provider}\n    model: ${m.model}\n`, 'utf-8');
  try {
    const res = spawnSync(DSH_CMD, ['dsh', '--profile', 'headless', '--patch', FAIRY_PATCH, '--patch', modelPatch, task], {
      cwd: REPO,
      encoding: 'utf-8',
      timeout: 300000,
      env: agentEnv(),
      maxBuffer: 64 * 1024 * 1024,
      shell: process.platform === 'win32',
    });
    if (res.error) throw new Error('dsh spawn: ' + res.error.message);
    if (res.status !== 0) {
      const stderr = (res.stderr || '').trim();
      throw new Error('dsh exited ' + res.status + (stderr ? ': ' + stderr.slice(-1500) : ''));
    }
    return { text: (res.stdout || '').trim(), trace: extractSessionTrace(sessionRootPath()) };
  } finally {
    try { fs.unlinkSync(modelPatch); } catch {}
  }
}

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.woff2': 'font/woff2',
  '.map': 'application/json',
};

function serveStatic(req, res, pathname) {
  let file = pathname === '/' ? 'index.html' : pathname.slice(1);
  let abs = path.join(DIST_DIR, file);
  if (!abs.startsWith(DIST_DIR)) { res.writeHead(403); res.end(); return; }
  if (!fs.existsSync(abs) || fs.statSync(abs).isDirectory()) abs = path.join(DIST_DIR, 'index.html');
  if (!fs.existsSync(abs)) { res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('not found'); return; }
  const ext = path.extname(abs).toLowerCase();
  res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream' });
  fs.createReadStream(abs).pipe(res);
}

const server = http.createServer(async (req, res) => {
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
  res.setHeader('Access-Control-Allow-Methods', 'GET,POST,OPTIONS');
  if (req.method === 'OPTIONS') { res.writeHead(204); res.end(); return; }

  const url = new URL(req.url, 'http://localhost');

  if (req.method === 'GET' && url.pathname === '/api/models') {
    res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
    res.end(JSON.stringify(MODELS.map(m => ({ id: m.id, display: m.display }))));
    return;
  }
  if (req.method === 'GET' && url.pathname === '/api/sessions') {
    const sessions = [];
    const root = sessionRootPath();
    if (fs.existsSync(root)) {
      try {
        const dirs = fs.readdirSync(root, { withFileTypes: true });
        for (const dir of dirs) {
          if (dir.isDirectory()) {
            const metaPath = path.join(root, dir.name, 'meta.json');
            if (fs.existsSync(metaPath)) {
              try {
                const meta = JSON.parse(fs.readFileSync(metaPath, 'utf-8'));
                sessions.push({
                  name: dir.name,
                  message_count: meta.message_count || 0,
                  modified: meta.modified || dir.name,
                });
              } catch {}
            }
          }
        }
      } catch {}
    }
    sessions.sort((a, b) => (b.modified || '').localeCompare(a.modified || ''));
    res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
    res.end(JSON.stringify(sessions));
    return;
  }
  if (req.method === 'GET' && url.pathname === '/api/system-prompt') {
    let content = '';
    try { content = fs.readFileSync(FAIRY_PATCH, 'utf-8'); } catch {}
    res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
    res.end(JSON.stringify({ name: 'fairy.patch.yml', content }));
    return;
  }
  if (req.method === 'POST' && url.pathname === '/api/chat') {
    let body = '';
    for await (const chunk of req) body += chunk;
    let data = {};
    try { data = JSON.parse(body || '{}'); } catch {}
    const message = String(data.message || '').trim();
    const modelId = String(data.model || 'minimax-text-01');
    res.writeHead(200, { 'Content-Type': 'text/event-stream; charset=utf-8', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
    const sse = obj => res.write('data: ' + JSON.stringify(obj) + '\n\n');
    if (!message) {
      sse({ type: 'done', messages: [{ role: 'assistant', content: '抱歉，主人，我没有收到你的问题。' }] });
      res.end();
      return;
    }
    sse({ type: 'status', message: 'Fairy 思考中…', step: 1, total_steps: 1 });
    try {
      const result = await new Promise((resolve, reject) => {
        try { resolve(runAgent(message, modelId)); } catch (e) { reject(e); }
      });
      sse({ type: 'user', text: message });
      for (const item of (result.trace || [])) {
        if (item.kind === 'context') {
          sse({ type: 'context', label: item.label });
        } else if (item.kind === 'tool') {
          sse({ type: 'tool', name: item.name, arguments: item.arguments, result: item.result, isError: item.isError });
        } else if (item.kind === 'message') {
          sse({ type: 'message', text: item.text, process: !!item.process });
        }
      }
      const messageReplies = (result.trace || []).filter(i => i.kind === 'message').map(i => i.text);
      let finalText = messageReplies[messageReplies.length - 1] || '';
      if (!finalText && result.text) {
        const plain = result.text.trim();
        if (!/^\s*<system-reminder>/i.test(plain) && !plain.includes('workspace instructions may be relevant') && !plain.includes('DSH File Policy')) {
          finalText = plain;
        }
      }
      if (!finalText) finalText = '抱歉，主人，我这边没有拿到可用回复。';
      sse({ type: 'done', messages: [{ role: 'assistant', content: finalText }] });
    } catch (e) {
      sse({ type: 'done', messages: [{ role: 'assistant', content: '网络或服务出错了：' + (e && e.message ? e.message : e) }] });
    }
    res.end();
    return;
  }

  // Production static serving of the built voice app.
  serveStatic(req, res, url.pathname);
});

server.listen(PORT, () => {
  console.log(`[voice] Fairy bridge server on http://localhost:${PORT}`);
  console.log(`[voice] dsh patch: ${FAIRY_PATCH}`);
});