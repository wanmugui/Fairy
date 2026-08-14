import { spawnSync } from "node:child_process";
import { mkdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));

export function repositoryRoot() {
  return path.resolve(scriptDir, "..");
}

export function agentExecutablePath(repoRoot, platform = process.platform, arch = process.arch) {
  const suffix = platform === "win32" ? ".exe" : "";
  return path.join(repoRoot, ".tools", `agent-loop-${platform}-${arch}${suffix}`);
}

function isRegularFile(filePath) {
  try {
    return statSync(filePath).isFile();
  } catch {
    return false;
  }
}

export function ensureAgent(repoRoot, env = process.env, report = console.error) {
  const output = env.AGENT_LOOP_PATH || agentExecutablePath(repoRoot);
  if (env.AGENT_LOOP_PATH && !isRegularFile(output)) {
    throw new Error(`AGENT_LOOP_PATH does not point to a file: ${output}`);
  }
  if (env.AGENT_LOOP_PATH) {
    report(`Using configured Agent: ${output}`);
    return output;
  }
  mkdirSync(path.dirname(output), { recursive: true });
  report(`Building native Agent: ${path.basename(output)}`);
  const result = spawnSync("go", ["build", "-o", output], {
    cwd: path.join(repoRoot, "agent"),
    stdio: "inherit",
  });
  if (result.error) {
    throw new Error(`cannot start go: ${result.error.message}`);
  }
  if (result.status !== 0) {
    throw new Error(`go build exited with code ${result.status ?? 1}`);
  }
  if (!isRegularFile(output)) {
    throw new Error(`Agent build did not produce a regular file: ${output}`);
  }
  return output;
}
