/**
 * Subprocess test helpers for nex CLI integration tests.
 */

import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const CLI_DIR = resolve(__dirname, "../..");

export interface RunResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

/**
 * Run the nex CLI as a subprocess and capture output.
 */
export function runNex(
  args: string[],
  opts: { env?: Record<string, string> } = {},
): Promise<RunResult> {
  return new Promise((res) => {
    const proc = spawn("node", ["--import", "tsx", "src/index.ts", ...args], {
      cwd: CLI_DIR,
      env: {
        ...process.env,
        ...opts.env,
        NODE_NO_WARNINGS: "1",
      },
    });

    let stdout = "";
    let stderr = "";
    proc.stdout.on("data", (d: Buffer) => { stdout += d; });
    proc.stderr.on("data", (d: Buffer) => { stderr += d; });
    proc.on("close", (code) => {
      res({ stdout: stdout.trim(), stderr: stderr.trim(), exitCode: code ?? 1 });
    });
  });
}

/**
 * Standard env vars pointing at the mock server.
 */
export function nexEnv(mockUrl: string): Record<string, string> {
  return {
    NEX_DEV_URL: mockUrl,
    NEX_API_KEY: "test-key",
  };
}
