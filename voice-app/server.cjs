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
    return (res.stdout || '').trim();
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
    res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
    res.end('[]');
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
      const text = await new Promise((resolve, reject) => {
        try { resolve(runAgent(message, modelId)); } catch (e) { reject(e); }
      });
      const finalText = text || '抱歉，主人，我这边没有拿到可用回复。';
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