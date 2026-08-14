import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";

import { agentExecutablePath } from "./agent_runtime.mjs";
import { resolvePnpmInvocation } from "./dev.mjs";

test("Agent build output is platform-specific but never a repository artifact", () => {
  const repo = path.join(path.sep, "repo");
  assert.equal(agentExecutablePath(repo, "darwin", "arm64"), path.join(repo, ".tools", "agent-loop-darwin-arm64"));
  assert.equal(agentExecutablePath(repo, "linux", "x64"), path.join(repo, ".tools", "agent-loop-linux-x64"));
  assert.equal(agentExecutablePath(repo, "win32", "x64"), path.join(repo, ".tools", "agent-loop-win32-x64.exe"));
});

test("pnpm launcher reuses pnpm's JavaScript entry when available", () => {
  const invocation = resolvePnpmInvocation({ npm_execpath: "/runtime/pnpm.cjs" }, "win32");
  assert.equal(invocation.command, process.execPath);
  assert.deepEqual(invocation.prefix, ["/runtime/pnpm.cjs"]);
});

test("direct launcher fallback uses the platform command", () => {
  assert.deepEqual(resolvePnpmInvocation({}, "darwin"), { command: "pnpm", prefix: [] });
  assert.deepEqual(resolvePnpmInvocation({}, "win32"), { command: "pnpm.cmd", prefix: [] });
});
