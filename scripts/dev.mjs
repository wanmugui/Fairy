import { spawn, spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { ensureAgent, repositoryRoot } from "./agent_runtime.mjs";
import { resolvePythonInvocation, withAgentPythonEnvironment } from "./python_runtime.mjs";

const scriptFile = fileURLToPath(import.meta.url);

export function resolvePnpmInvocation(env = process.env, platform = process.platform) {
  // pnpm sets npm_execpath when this script is invoked via `pnpm dev`.
  // Running that JS entry with Node avoids Windows .cmd/Powershell handling.
  if (env.npm_execpath) {
    return { command: process.execPath, prefix: [env.npm_execpath] };
  }
  return { command: platform === "win32" ? "pnpm.cmd" : "pnpm", prefix: [] };
}

function run(command, args, options) {
  const result = spawnSync(command, args, { stdio: "inherit", ...options });
  if (result.error) {
    throw new Error(`cannot start ${command}: ${result.error.message}`);
  }
  if (result.status !== 0) {
    throw new Error(`${command} exited with code ${result.status ?? 1}`);
  }
}

function start(command, args, options) {
  const child = spawn(command, args, { stdio: "inherit", ...options });
  child.on("error", error => {
    console.error(`[dev] cannot start ${command}: ${error.message}`);
  });
  return child;
}

function runPnpm(pnpm, args, options) {
  run(pnpm.command, [...pnpm.prefix, ...args], options);
}

function startPnpm(pnpm, args, options) {
  return start(pnpm.command, [...pnpm.prefix, ...args], options);
}

function ensureFrontendDependencies(repoRoot, pnpm) {
  const frontendDir = path.join(repoRoot, "frontend");
  if (existsSync(path.join(frontendDir, "node_modules"))) {
    console.log("[2/3] Frontend dependencies already installed");
    return;
  }
  console.log("[2/3] Installing frontend dependencies");
  runPnpm(pnpm, ["--dir", frontendDir, "install", "--frozen-lockfile"], { cwd: repoRoot });
}

export async function main() {
  const repoRoot = repositoryRoot();
  const frontendDir = path.join(repoRoot, "frontend");
  const pnpm = resolvePnpmInvocation();
  console.log("[1/3] Preparing native Agent");
  const agentPath = ensureAgent(repoRoot, process.env, message => console.log(`[1/3] ${message}`));
  ensureFrontendDependencies(repoRoot, pnpm);

  const env = {
    ...process.env,
    AGENT_LOOP_PATH: agentPath,
    AGENT_REPO_ROOT: repoRoot,
  };
  try {
    Object.assign(env, withAgentPythonEnvironment(env, resolvePythonInvocation(env)));
  } catch (error) {
    console.warn(`[dev] ${error.message}; Python tools will be unavailable until pnpm setup:python succeeds.`);
  }
  const apiPort = process.env.AGENT_API_PORT || "8081";
  const host = process.env.AGENT_DEV_HOST || "127.0.0.1";
  const viteArgs = process.argv.slice(2);

  console.log(`[3/3] Starting API server on http://localhost:${apiPort}`);
  const api = start(process.execPath, [path.join(frontendDir, "server.cjs"), apiPort], { cwd: repoRoot, env });
  console.log(`Frontend: http://${host}:5173`);
  const vite = startPnpm(pnpm, ["--dir", frontendDir, "exec", "vite", "--host", host, ...viteArgs], { cwd: repoRoot, env });

  let shuttingDown = false;
  const stop = (exitCode = 0) => {
    if (shuttingDown) return;
    shuttingDown = true;
    for (const child of [vite, api]) {
      if (child.exitCode === null && !child.killed) child.kill();
    }
    process.exitCode = exitCode;
  };

  api.on("exit", code => {
    if (!shuttingDown) {
      console.error(`[dev] API server exited unexpectedly (${code ?? "signal"})`);
      stop(code || 1);
    }
  });
  vite.on("exit", code => stop(code || 0));
  process.once("SIGINT", () => stop(0));
  process.once("SIGTERM", () => stop(0));
}

if (path.resolve(process.argv[1] || "") === scriptFile) {
  main().catch(error => {
    console.error(`[dev] ${error.message}`);
    process.exitCode = 1;
  });
}
