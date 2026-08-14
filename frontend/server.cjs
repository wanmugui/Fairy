const http = require("http");
const fs = require("fs");
const path = require("path");
const { spawn } = require("child_process");
const { buildAgentProcessOptions, resolveAgentExecutable } = require("./agent_executable.cjs");
const { createSessionArchive } = require("./session_archive.cjs");

const REPO = path.resolve(__dirname, "..");
const SESSIONS = path.join(__dirname, "sessions");
if (!fs.existsSync(SESSIONS)) fs.mkdirSync(SESSIONS, { recursive: true });

function readStripped(fp) {
  try { return fs.readFileSync(fp, "utf-8").replace(/^\uFEFF/, ""); }
  catch { return null; }
}

const MODELS = {
  "minimax-text-01": { display: "MiniMax-Text-01", kind: "real", config: path.join(REPO, "config", "config.minimax.json"), override: null },
  "deepseek-chat": { display: "DeepSeek-Chat", kind: "real", config: path.join(REPO, "config", "config.deepseek.json"), override: null },
};

// Reverse map: actual API model name -> frontend model id (sessions store the
// API model name written by the agent, e.g. "gateway_qn_claude_opus_47").
const apiModelToUi = {};
for (const [id, v] of Object.entries(MODELS)) {
  if (v.override) {
    apiModelToUi[v.override] = id;
  } else {
    try {
      const cfg = JSON.parse(readStripped(v.config) || "{}");
      if (cfg.api && cfg.api.model) apiModelToUi[cfg.api.model] = id;
    } catch {}
  }
}

function defaultSessionName() {
  const d = new Date();
  const pad = n => String(n).padStart(2, "0");
  return "chat-" + d.getFullYear() + pad(d.getMonth() + 1) + pad(d.getDate()) + "-" + pad(d.getHours()) + pad(d.getMinutes()) + pad(d.getSeconds());
}

function listSessions() {
  const items = [];
  try {
    const dirs = fs.readdirSync(SESSIONS).filter(f => {
      try { return fs.statSync(path.join(SESSIONS, f)).isDirectory(); } catch { return false; }
    });
    for (const d of dirs) {
      try {
        const fp = path.join(SESSIONS, d, d + '.json');
        if (!fs.existsSync(fp)) continue;
        const stat = fs.statSync(fp);
        const data = JSON.parse(readStripped(fp));
        const msgs = data.messages || [];
        const firstUser = msgs.find(m => m.role === 'user');
        let preview = (firstUser && firstUser.content) || '';
        preview = String(preview).replace(/\n/g, ' ').slice(0, 60);
        items.push({ name: d, modified: stat.mtime.toISOString().replace('T', ' ').slice(0, 19), message_count: msgs.length, preview, model: data.model || null });
      } catch {}
    }
    items.sort((a, b) => String(b.modified).localeCompare(String(a.modified)));
  } catch {}
  return items;
}
function getSession(name) {
  const fp = path.join(SESSIONS, name, name + '.json');
  if (!fs.existsSync(fp)) return [];
  try {
    const msgs = JSON.parse(readStripped(fp)).messages || [];
    const patched = patchMissingSubtaskResults(name, msgs);
    return enrichSubtaskStats(name, patched);
  }
  catch { return []; }
}

// For historical sessions the create_subtask tool result carries a compact
// message snapshot (budget-trimmed), so summing it under-counts the subtask's
// real LLM time/tokens. The result also carries `session` = the full child
// session file; read it and inject complete agent_stats so the frontend shows
// true totals (new sessions get agent_stats straight from the tool already).
function enrichSubtaskStats(sessionName, msgs) {
  const subtasksDir = path.join(SESSIONS, sessionName, 'subtasks');
  const out = msgs.map(m => {
    if (!(m && m.role === 'tool' && m.name === 'create_subtask')) return m;
    let content = null;
    try { content = JSON.parse(m.content || ''); } catch {}
    if (!content) return m;
    const sessRef = content.session || '';
    const base = sessRef ? path.basename(sessRef) : '';
    const fp = base ? path.join(subtasksDir, base) : '';
    if (!fp || !fs.existsSync(fp)) return m;
    try {
      const sub = JSON.parse(readStripped(fp));
      const msgsArr = (sub && sub.messages) || [];
      let durMs = 0, pt = 0, ct = 0;
      for (const sm of msgsArr) {
        if (!sm || sm.role !== 'assistant') continue;
        if (typeof sm.duration_ms === 'number') durMs += sm.duration_ms;
        if (sm.usage) {
          pt += Number(sm.usage.prompt_tokens) || 0;
          ct += Number(sm.usage.completion_tokens) || 0;
        }
      }
      // Re-inject the FULL subtask process for the frontend card (the tool
      // result itself stays slim so the main-thread context is not flooded).
      const fullMsgs = msgsArr.filter(sm => sm && sm.role !== 'system');
      const clone = {
        ...content,
        agent_stats: { duration_ms: durMs, prompt_tokens: pt, completion_tokens: ct },
        messages: fullMsgs.length ? fullMsgs : content.messages,
      };
      const updated = { ...m, content: JSON.stringify(clone) };
      return updated;
    } catch { return m; }
  });
  return out;
}

// Live SSE path: when a create_subtask finishes, enrich its slim result with
// the full subtask process from the persisted session file, so the frontend
// card shows the whole run immediately (no page refresh needed). Mirrors
// enrichSubtaskStats used on history load.
function enrichCreateSubtaskResult(content, sessionName) {
  try {
    const parsed = JSON.parse(content);
    if (!parsed || typeof parsed !== 'object' || !parsed.session) return content;
    const base = path.basename(parsed.session);
    const fp = path.join(SESSIONS, sessionName, 'subtasks', base);
    if (!fp || !fs.existsSync(fp)) return content;
    const sub = JSON.parse(readStripped(fp));
    const msgsArr = (sub && sub.messages) || [];
    let durMs = 0, pt = 0, ct = 0;
    for (const sm of msgsArr) {
      if (!sm || sm.role !== 'assistant') continue;
      if (typeof sm.duration_ms === 'number') durMs += sm.duration_ms;
      if (sm.usage) {
        pt += Number(sm.usage.prompt_tokens) || 0;
        ct += Number(sm.usage.completion_tokens) || 0;
      }
    }
    const fullMsgs = msgsArr.filter(sm => sm && sm.role !== 'system');
    const clone = {
      ...parsed,
      agent_stats: { duration_ms: durMs, prompt_tokens: pt, completion_tokens: ct },
      messages: fullMsgs.length ? fullMsgs : parsed.messages,
    };
    return JSON.stringify(clone);
  } catch { return content; }
}

// When the main thread crashed while a create_subtask child was still running,
// the tool result was never written back to the main session. Recover it from
// the persisted child session (<chat>/subtasks/<title>-<nano>.json) so the
// frontend can render the finished subtask instead of a stuck "running" card.
function patchMissingSubtaskResults(sessionName, msgs) {
  const subtasksDir = path.join(SESSIONS, sessionName, 'subtasks');
  let files = [];
  try { files = fs.readdirSync(subtasksDir).filter(f => f.endsWith('.json')); } catch { return msgs; }
  if (!files.length) return msgs;

  const out = [...msgs];
  let inserted = 0;
  for (let i = 0; i < out.length; i++) {
    const m = out[i];
    if (m.role !== 'assistant' || !Array.isArray(m.tool_calls)) continue;
    const cs = m.tool_calls.filter(tc => tc.function && tc.function.name === 'create_subtask');
    if (!cs.length) continue;
    for (const tc of cs) {
      const callId = tc.id || tc.tool_call_id || '';
      const hasResp = out.slice(i + 1).some(t => t.role === 'tool' && (t.tool_call_id === callId || (t.name === 'create_subtask' && !callId)));
      if (hasResp) continue;
      let title = '';
      try { title = (JSON.parse(tc.function.arguments || '{}').title) || ''; } catch {}
      if (!title) continue;
      const safe = String(title).replace(/[\\/:*?"<>|]/g, '_').replace(/\s+/g, '_');
      let best = null;
      for (const f of files) {
        if (!f.startsWith(safe)) continue;
        const fp2 = path.join(subtasksDir, f);
        try {
          const st = fs.statSync(fp2);
          if (!best || st.mtimeMs > best.mtimeMs) best = { f, fp: fp2, mtime: st.mtimeMs };
        } catch {}
      }
      if (!best) continue;
      try {
        const child = JSON.parse(readStripped(best.fp));
        const childMsgs = child.messages || [];
        const BUDGET = 30000;
        const all = [];
        let total = 0;
        for (const cm of childMsgs) {
          if (cm.role === 'system') continue;
          const cc = typeof cm.content === 'string' ? cm.content : '';
          const isReport = /<report\b/i.test(cc);
          all.push({ ...cm, content: (!isReport && cc.length > 4000) ? cc.slice(0, 4000) + '...[truncated]' : cc });
          if (!isReport) total += cc.length;
        }
        let compact = all;
        if (total > BUDGET) {
          // Keep <report> deliverables and the newest tail; omit the middle.
          const kept = [];
          let keptLen = 0;
          let reportSeen = false;
          for (let i = all.length - 1; i >= 0; i--) {
            const cc = typeof all[i].content === 'string' ? all[i].content : '';
            const isReport = /<report\b/i.test(cc);
            if (isReport && !reportSeen) {
              reportSeen = true;
              kept.unshift(all[i]);
              keptLen += cc.length;
              continue;
            }
            if (keptLen + cc.length > BUDGET) continue;
            kept.unshift(all[i]);
            keptLen += cc.length;
          }
          const skipped = all.length - kept.length;
          if (skipped > 0) kept.unshift({ role: 'assistant', content: '...[中间省略 ' + skipped + ' 条消息，完整内容见子任务 session 文件]...' });
          compact = kept;
        }

        const resultContent = JSON.stringify({
          ok: true, title, session: best.fp,
          messages: compact,
          recovered: true,
        });
        out.splice(i + 1 + inserted, 0, { role: 'tool', content: resultContent, tool_call_id: callId, name: 'create_subtask' });
        inserted++;
      } catch {}
    }
  }
  return out;
}


let _counter = 0;
function nextId() { return ++_counter; }
function nowTs() { return Date.now(); }


// Extract user-visible text from assistant messages with XML protocol tags
function extractDisplayText(content) {
  if (!content) return content;
  // Strip internal protocol tags (reflection, etc) that should not reach frontend
  content = content.replace(/<reflection[\s\S]*?<\/reflection>/gi, '');
  // <process><message>text</message></process> -> only show message text (tool thought/preview)
  var m = content.match(/<message[^>]*>([\s\S]*?)<\/message>/);
  if (m) return m[1].trim();
  // <behavior><des>text</des></behavior> -> only show des text (backward compat)
  m = content.match(/<des[^>]*>([\s\S]*?)<\/des>/);
  if (m) return m[1].trim();
  // <report> is NOT stripped - frontend needs it for report card formatting
  return content;
}
function convertToProductionFormat(rawMessages, model = null) {
  if (!rawMessages || !rawMessages.length) {
    return { code: 0, message: "success", data: { messages: [], paging: { offset: 0, limit: 20, total: 0 }, model } };
  }
  const production = [];
  let current = null;
  let turnIndex = 0;
  _counter = 0;
  const { randomUUID } = require("crypto");

  for (const msg of rawMessages) {
    const role = msg.role || "";
    let content = msg.content || "";
    if (typeof content !== "string") content = String(content);

    if (role === "user") {
      if (current) { production.push(current); current = null; }
      turnIndex++;
      const userMsg = {
        role: "user",
        contents: [{ id: nextId(), timestamp: nowTs(), type: "text", internal_type: "text", content: extractDisplayText(content), active_agent: "main" }],
        turn_id: String(turnIndex),
        version_id: randomUUID(),
        message_uuid: randomUUID(),
        display_tag: "response",
      };
      production.push(userMsg);
      if (msg.usage) userMsg.usage = msg.usage;
      if (msg.duration_ms) userMsg.duration_ms = msg.duration_ms;
    } else if (role === "assistant") {
      if (current) production.push(current);
      turnIndex++;
      current = {
        role: "assistant",
        contents: [],
        turn_id: String(turnIndex),
        version_id: randomUUID(),
        message_uuid: randomUUID(),
        display_tag: "response",
        usage: msg.usage,
        duration_ms: msg.duration_ms,
      };
      if (content) {
        current.contents.push({ id: nextId(), timestamp: nowTs(), type: "text", internal_type: "text", content: extractDisplayText(content), active_agent: "main" });
      }
      // Pass through usage and duration_ms for per-turn stats
      if (msg.usage) current.usage = msg.usage;
      if (msg.duration_ms !== undefined && msg.duration_ms !== null) current.duration_ms = msg.duration_ms;
      if (msg.real_ms !== undefined && msg.real_ms !== null) current.real_ms = msg.real_ms;
      const tc = msg.tool_calls;
      if (tc && tc.length) {
        const tcStr = typeof tc === "string" ? tc : JSON.stringify(tc);
        current.contents.push({ id: nextId(), timestamp: nowTs(), type: "tool_calls", internal_type: "tool_calls", tool_calls: tcStr, active_agent: "main" });
      }
    } else if (role === "tool") {
      if (!current) {
        turnIndex++;
        current = { role: "assistant", contents: [], turn_id: String(turnIndex), version_id: randomUUID(), message_uuid: randomUUID(), display_tag: "response" };
      }
      current.contents.push({ id: nextId(), timestamp: nowTs(), type: "tool_result", internal_type: "tool_result", content, tool_call_id: msg.tool_call_id || "", name: msg.name || "", active_agent: "main" });
    }
  }
  if (current) production.push(current);

  return { code: 0, message: "success", data: { messages: production, paging: { offset: 0, limit: 20, total: production.length }, model } };
}

function appendErrorToSession(sessionFile, err) {
  try {
    let session = { messages: [] };
    if (fs.existsSync(sessionFile)) {
      const raw = readStripped(sessionFile);
      if (raw) session = JSON.parse(raw);
    }
    session.messages.push({ role: "system", content: "SYSTEM ERROR: " + err });
    fs.writeFileSync(sessionFile, JSON.stringify(session, null, 2), "utf-8");
  } catch {}
}

// Track chats with an in-flight agent run so the frontend can show
// "thinking" state after switching sessions or refreshing.
const runningChats = new Map(); // sessionName -> { startedAt, child }

// Kill an agent run and its whole process tree. create_subtask/run.exe and
// nested subtask agent-loop.exe are children of the main agent process, so
// killing only the main process would leave orphaned subtasks still generating
// output (PPT would keep building after the user pressed Stop).
function killProcessTree(pid, signal) {
  if (!pid) return;
  const cp = require("child_process");
  if (process.platform === "win32") {
    // taskkill /T kills the whole tree rooted at pid (node is not a child, so
    // the server itself is never touched).
    try { cp.spawnSync("taskkill", ["/PID", String(pid), "/T", "/F"], { timeout: 15000 }); } catch {}
    return;
  }
  // POSIX: recursively collect descendants via pgrep -P, then signal them.
  try {
    const out = cp.spawnSync("pgrep", ["-P", String(pid)], { encoding: "utf8", timeout: 5000 });
    if (out.status === 0 && out.stdout) {
      for (const line of out.stdout.split("\n")) {
        const c = parseInt(line.trim(), 10);
        if (c && c !== pid) killProcessTree(c, signal);
      }
    }
  } catch {}
  try { process.kill(pid, signal); } catch {}
}

// Persist a turn's real wait time (measured client-side) onto the newest
// assistant message(s) of the session so history load still shows ⏱ real.
// Persist a turn's real wait time (measured client-side) onto the newest
// assistant message(s) of the session AND into usage.json (per-turn +
// cumulative real_ms) so it survives refresh/restart instead of living
// only in localStorage.
function attachRealMsToSession(name, realMs) {
  const fp = path.join(SESSIONS, name, name + '.json');
  let matchedIndex = -1;
  try {
    const raw = readStripped(fp);
    if (raw) {
      const session = JSON.parse(raw);
      const msgs = session.messages || [];
      // attach to the last assistant message that has duration_ms (the final reply)
      for (let i = msgs.length - 1; i >= 0; i--) {
        const m = msgs[i];
        if (m.role === 'assistant' && m.duration_ms) {
          m.real_ms = realMs;
          matchedIndex = i;
          break;
        }
      }
      fs.writeFileSync(fp, JSON.stringify(session, null, 2), 'utf-8');
    }
  } catch {}

  // Persist real time into usage.json too (per-turn + cumulative).
  const usageFp = path.join(SESSIONS, name, 'usage.json');
  try {
    const uRaw = readStripped(usageFp);
    if (!uRaw) return;
    const usage = JSON.parse(uRaw);
    const turns = usage.turns || [];
    if (turns.length) {
      let target = null;
      if (matchedIndex >= 0) target = turns.find(t => t && t.message_index === matchedIndex);
      if (!target) target = turns[turns.length - 1];
      if (target) target.real_ms = realMs;
    }
    usage.real_ms = turns.reduce((s, t) => s + (Number(t && t.real_ms) || 0), 0);
    usage.turns = turns;
    fs.writeFileSync(usageFp, JSON.stringify(usage, null, 2), 'utf-8');
  } catch {}
}

const PORT = parseInt(process.argv[2]) || 8081;
const server = http.createServer((req, res) => {
  const url = new URL(req.url, "http://localhost");
  const pathname = url.pathname;
  const method = req.method;

  res.setHeader("Access-Control-Allow-Origin", "*");
  res.setHeader("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS");
  res.setHeader("Access-Control-Allow-Headers", "Content-Type");
  if (method === "OPTIONS") { res.writeHead(204); res.end(); return; }

  const sendJson = (obj, status = 200) => {
    res.writeHead(status, { "Content-Type": "application/json; charset=utf-8" });
    res.end(JSON.stringify(obj));
  };

  if (method === "GET") {
    if (pathname === "/api/models") {
      sendJson(Object.entries(MODELS).map(([id, v]) => ({ id, ...v })));
      return;
    }
    if (pathname === "/api/sessions") {
      sendJson(listSessions());
      return;
    }
    const parts = pathname.split("/").filter(Boolean);
    if (parts.length >= 3 && parts[0] === "api" && parts[1] === "sessions") {
      const name = decodeURIComponent(parts[2]);
      if (parts.length === 4 && parts[3] === "messages") {
        const raw = getSession(name);
        let savedModel = null;
        let savedUsage = null;
        try {
          const sessRaw = readStripped(path.join(SESSIONS, name, name + '.json'));
          if (sessRaw) {
            const sessData = JSON.parse(sessRaw);
            savedModel = sessData.model || null;
            if (savedModel && apiModelToUi[savedModel]) savedModel = apiModelToUi[savedModel];
 try { const uf = readStripped(path.join(SESSIONS, name, 'usage.json')); if (uf) { savedUsage = JSON.parse(uf); } } catch {}
            ;
          }
        } catch {}
        const prodData = convertToProductionFormat(raw, savedModel);
        if (savedUsage) prodData.data.usage = savedUsage;
        sendJson(prodData);
        return;
      }
      if (parts.length === 4 && parts[3] === 'status') {
        const run = runningChats.get(name);
        sendJson({
          ok: true,
          running: !!run,
          started_at: run ? run.startedAt : null,
          elapsed_ms: run ? (Date.now() - run.startedAt) : 0,
        });
        return;
      }
      if (parts.length === 4 && parts[3] === 'download') {
        // Download the whole session folder (chat-xxx/) as a zip archive.
        const dir = path.join(SESSIONS, name);
        if (!fs.existsSync(dir)) { sendJson({ error: 'session not found: ' + name }, 404); return; }
        let data;
        try { data = createSessionArchive(dir); }
        catch (error) { sendJson({ error: 'zip failed: ' + error.message }, 500); return; }
        res.writeHead(200, {
          "Content-Type": "application/zip",
          "Content-Disposition": 'attachment; filename="' + name + '.zip"',
          "Content-Length": data.length,
        });
        res.end(data);
        return;
      }

    }
    sendJson({ error: "not found", path: pathname }, 404);
    return;
  }

  if (method === "POST") {
    const parts = pathname.split("/").filter(Boolean);
    if (parts.length === 4 && parts[0] === "api" && parts[1] === "sessions" && parts[3] === "real") {
      const name = decodeURIComponent(parts[2]);
      let body = '';
      req.on('data', chunk => { body += chunk; });
      req.on('end', () => {
        try {
          const payload = JSON.parse(body || '{}');
          const realMs = Number(payload.real_ms) || 0;
          if (realMs > 0) attachRealMsToSession(name, realMs);
          sendJson({ ok: true });
        } catch (e) {
          sendJson({ ok: false, error: e.message }, 400);
        }
      });
      return;
    }
    if (parts.length === 4 && parts[0] === "api" && parts[1] === "sessions" && parts[3] === "stop") {
      // Dedicated stop event: kill the running agent for this session AND its
      // whole process tree (create_subtask/run.exe + subtask agent-loop.exe)
      // so stopping actually halts PPT generation instead of leaving orphans.
      const name = decodeURIComponent(parts[2]);
      const run = runningChats.get(name);
      let stopped = false;
      if (run && run.child) {
        run.stoppedByUser = true;
        try {
          killProcessTree(run.child.pid, "SIGTERM");
          stopped = true;
        } catch {
          try { run.child.kill(); stopped = true; } catch {}
        }
      }
      sendJson({ ok: true, stopped });
      return;
    }
    // not the real-ms route; fall through to /api/chat below
  }

  if (method === "DELETE") {
    const parts = pathname.split("/").filter(Boolean);
    if (parts.length === 3 && parts[0] === "api" && parts[1] === "sessions") {
      const name = decodeURIComponent(parts[2]);
      const fp = path.join(SESSIONS, name);
      try { if (fs.existsSync(fp)) fs.rmSync(fp, { recursive: true, force: true }); } catch {}
      sendJson({ deleted: true });
      return;
    }
    sendJson({ error: "not found" }, 404);
    return;
  }

  if (method === "POST" && pathname === "/api/sessions") {
    // Create a blank session so it shows up in the history list immediately.
    let body = "";
    req.on("data", chunk => body += chunk);
    req.on("end", () => {
      try {
        const data = JSON.parse(body || "{}");
        const name = String(data.name || "").trim();
        if (!name) { sendJson({ ok: false, error: "name required" }, 400); return; }
        const dir = path.join(SESSIONS, name);
        fs.mkdirSync(dir, { recursive: true });
        const fp = path.join(dir, name + ".json");
        if (!fs.existsSync(fp)) {
          fs.writeFileSync(fp, JSON.stringify({ messages: [], model: null }, null, 2), "utf-8");
        }
        sendJson({ ok: true, name });
      } catch (e) { sendJson({ ok: false, error: e.message }, 400); }
    });
    return;
  }

  if (method === "POST" && pathname === "/api/chat") {
    let body = "";
    req.on("data", chunk => body += chunk);
    req.on("end", () => {
      let data;
      try { data = JSON.parse(body || "{}"); } catch (e) {
        sendJson({ ok: false, error: "bad json: " + e.message }, 400);
        return;
      }
      const msg = (data.message || "").trim();
      const modelId = data.model || "";
      const sessionName = data.session || defaultSessionName();
      if (!msg) { sendJson({ ok: false, error: "empty message" }, 400); return; }
      if (!MODELS[modelId]) { sendJson({ ok: false, error: "unknown model: " + modelId }, 400); return; }

      const sessionFile = path.join(SESSIONS, sessionName, sessionName + '.json');

      const sessionDir = path.join(SESSIONS, sessionName);
      if (!fs.existsSync(sessionDir)) fs.mkdirSync(sessionDir, { recursive: true });
      if (data.stream) {
        res.writeHead(200, { "Content-Type": "text/event-stream; charset=utf-8", "Cache-Control": "no-cache" });
        const sse = (event, data) => res.write((event ? "event: " + event + "\n" : "") + "data: " + JSON.stringify(data) + "\n\n");

        const m = MODELS[modelId];
        let configPath = m.config;
        let tmpCfg = null, userFile = null;
        try {
          if (m.override) {
            const cfg = JSON.parse(readStripped(configPath) || "{}");
            cfg.api.model = m.override;
            tmpCfg = path.join(require("os").tmpdir(), "cfg_" + Date.now() + ".json");
            fs.writeFileSync(tmpCfg, JSON.stringify(cfg, null, 2));
            configPath = tmpCfg;
          }
          userFile = path.join(require("os").tmpdir(), "user_msg_" + Date.now() + ".txt");
          fs.writeFileSync(userFile, msg, "utf-8");

          const agentExe = resolveAgentExecutable(REPO);
                    // Poll subtask .stream files and forward new lines as SSE events so
          // the frontend can render subtask execution live.
          const subtaskTails = new Map(); // file -> { title, offset }
          const subtaskTimer = setInterval(() => {
            const stDir = path.join(SESSIONS, sessionName, "subtasks");
            let files = [];
            try { files = fs.readdirSync(stDir).filter(f => f.endsWith(".stream")); } catch { return; }
            for (const f of files) {
              const fp2 = path.join(stDir, f);
              try {
                const st = fs.statSync(fp2);
                let t = subtaskTails.get(f) || { title: null, offset: 0 };
                if (st.size < t.offset) t = { title: t.title, offset: 0 }; // truncated/recreated
                if (st.size <= t.offset) continue;
                const fd = fs.openSync(fp2, "r");
                const buf = Buffer.alloc(st.size - t.offset);
                fs.readSync(fd, buf, 0, buf.length, t.offset);
                fs.closeSync(fd);
                t.offset = st.size;
                const text = buf.toString("utf-8");
                for (const line of text.split("\n")) {
                  const s = line.trim();
                  if (!s) continue;
                  try {
                    const ev = JSON.parse(s);
                    if (ev.type === "subtask_start") t.title = ev.title;
                    if (ev.type && t.title) sse(null, { type: "subtask_event", title: t.title, event: ev });
                  } catch {}
                }
                subtaskTails.set(f, t);
              } catch {}
            }
          }, 800);

          const child = spawn(agentExe, ["-ConfigPath", configPath, "-UseMock", String(m.kind === "mock"), "-UserOverrideFile", userFile, "-SessionFile", sessionFile], buildAgentProcessOptions(REPO, { stdio: ["ignore", "pipe", "pipe"] }));
          runningChats.set(sessionName, { startedAt: Date.now(), child });

          let buf = "";
          let errBuf = "";
          child.stdout.on("data", chunk => {
            buf += chunk.toString("utf-8");
            const lines = buf.split("\n");
            buf = lines.pop() || "";
            for (const line of lines) {
              const t = line.trim();
              if (!t) continue;
              try {
                const ev = JSON.parse(t);
                if (ev.type) {
                  // Real-time subtask card enrichment: replace the slim
                  // create_subtask result with the full process from the
                  // session file so the card shows everything immediately.
                  if (ev.type === 'tool_call' && ev.status === 'end' && ev.tool === 'create_subtask' && ev.result) {
                    ev.result = enrichCreateSubtaskResult(ev.result, sessionName);
                  } else if (ev.type === 'tool_result' && ev.name === 'create_subtask' && ev.result) {
                    ev.result = enrichCreateSubtaskResult(ev.result, sessionName);
                  }
                  ev.session = sessionName;
                  sse(null, ev);
                }
              } catch {}
            }
          });

          child.stderr.on("data", chunk => {
            errBuf += chunk.toString("utf-8");
          });

          child.on("close", code => {
            const runInfo = runningChats.get(sessionName);
            const stoppedByUser = !!(runInfo && runInfo.stoppedByUser);
            runningChats.delete(sessionName);
            if (buf.trim()) {
              try { const ev = JSON.parse(buf.trim()); if (ev.type) { ev.session = sessionName; sse(null, ev); } } catch {}
            }
            if (code !== 0) {
              if (stoppedByUser) {
                // user pressed Stop: not an error, end the stream cleanly.
                sse("done", { type: "done", session: sessionName });
              } else {
                const errText = errBuf.slice(0, 500);
                const errMsg = "harness exited " + code + ": " + errText;
                appendErrorToSession(sessionFile, errMsg);
                sse("error", { type: "error", session: sessionName, error: errMsg });
              }
            } else if (fs.existsSync(sessionFile)) {
              try {
                const raw = readStripped(sessionFile);
                const session = JSON.parse(raw);
                if (!session.model) {
                  session.model = modelId;
                  try { fs.writeFileSync(sessionFile, JSON.stringify(session, null, 2), "utf-8"); } catch {}
                }
                const usageFile = path.join(SESSIONS, sessionName, 'usage.json');
                let sessUsage = null;
                try { if (fs.existsSync(usageFile)) { sessUsage = JSON.parse(readStripped(usageFile)); } } catch {}
                sse('done', { type: 'done', session: sessionName, messages: convertToProductionFormat(session.messages || [], session.model || modelId), usage: sessUsage, session_usage: sessUsage, total_usage: sessUsage });
              } catch { sse('done', { type: 'done', session: sessionName }); }
            } else {
              sse("done", { type: "done", session: sessionName });
            }
                        clearInterval(subtaskTimer);
res.write("data: [DONE]\n\n");
            res.end();
            if (tmpCfg) try { fs.unlinkSync(tmpCfg); } catch {}
            if (userFile) try { fs.unlinkSync(userFile); } catch {}
          });

          child.on("error", err => {
            sse("error", { type: "error", session: sessionName, error: err.message });
            res.write("data: [DONE]\n\n");
            res.end();
            if (tmpCfg) try { fs.unlinkSync(tmpCfg); } catch {}
            if (userFile) try { fs.unlinkSync(userFile); } catch {}
          });
        } catch (e) {
          sse("error", { type: "error", session: sessionName, error: e.message });
          res.write("data: [DONE]\n\n");
          res.end();
          if (tmpCfg) try { fs.unlinkSync(tmpCfg); } catch {}
          if (userFile) try { fs.unlinkSync(userFile); } catch {}
        }
      } else {
        const m = MODELS[modelId];
        let configPath = m.config;
        let tmpCfg = null, userFile = null;
        try {
          if (m.override) {
            const cfg = JSON.parse(readStripped(configPath) || "{}");
            cfg.api.model = m.override;
            tmpCfg = path.join(require("os").tmpdir(), "cfg_" + Date.now() + ".json");
            fs.writeFileSync(tmpCfg, JSON.stringify(cfg, null, 2));
            configPath = tmpCfg;
          }
          userFile = path.join(require("os").tmpdir(), "user_msg_" + Date.now() + ".txt");
          fs.writeFileSync(userFile, msg, "utf-8");

          const agentExe = resolveAgentExecutable(REPO);
          const proc = require("child_process").spawnSync(agentExe, ["-ConfigPath", configPath, "-UseMock", String(m.kind === "mock"), "-UserOverrideFile", userFile, "-SessionFile", sessionFile], buildAgentProcessOptions(REPO, { encoding: "utf-8", maxBuffer: 50 * 1024 * 1024 }));

          if (proc.error) { sendJson({ ok: false, error: "spawn error: " + proc.error.message }); return; }
          if (proc.status !== 0) {
            const err = "harness exited " + proc.status + ": " + (proc.stderr || "").slice(0, 2000);
            appendErrorToSession(sessionFile, err);
            sendJson({ ok: false, error: err }); return;
          }
          if (!fs.existsSync(sessionFile)) { sendJson({ ok: false, error: "no session file" }); return; }
          const raw = readStripped(sessionFile);
          const session = JSON.parse(raw);
          if (!session.model) {
            session.model = modelId;
            try { fs.writeFileSync(sessionFile, JSON.stringify(session, null, 2), "utf-8"); } catch {}
          }
          sendJson(convertToProductionFormat(session.messages || [], session.model || modelId));
        } finally {
          if (tmpCfg) try { fs.unlinkSync(tmpCfg); } catch {}
          if (userFile) try { fs.unlinkSync(userFile); } catch {}
        }
      }
    });
    return;
  }

  sendJson({ error: "not found" }, 404);
});

server.listen(PORT, 'localhost', () => {
  console.log('[server] Node.js API server on http://localhost:' + PORT + '/');
  console.log('[server] Ctrl+C to stop.');
});
