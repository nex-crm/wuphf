/**
 * Lightweight TUI E2E test harness using node-pty.
 *
 * Launches the TUI in a real PTY, sends keystrokes, and reads
 * the terminal output for assertions. Works in CI (no real terminal needed).
 */

import * as pty from "node-pty";
import { join } from "node:path";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";

const STRIP_ANSI_RE = /\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07/g;

/** Resolve local node_modules/.bin path for tsx */
function localBin(name: string): string {
  return join(process.cwd(), "node_modules", ".bin", name);
}

export function stripAnsi(s: string): string {
  return s.replace(STRIP_ANSI_RE, "");
}

export interface TuiTestOptions {
  /** Command to run (default: "npx") */
  command?: string;
  /** Args (default: ["tsx", "src/index.ts"]) */
  args?: string[];
  /** Working directory */
  cwd?: string;
  /** Extra env vars */
  env?: Record<string, string>;
  /** Terminal columns (default: 120) */
  cols?: number;
  /** Terminal rows (default: 30) */
  rows?: number;
  /** Max time to wait for any assertion (ms, default: 10000) */
  timeout?: number;
}

export class TuiTest {
  private proc: pty.IPty;
  private output = "";
  private timeout: number;

  constructor(opts: TuiTestOptions = {}) {
    const command = opts.command ?? process.execPath; // node binary
    const args = opts.args ?? ["--import", "tsx", "src/index.ts"];
    const cwd = opts.cwd ?? process.cwd();

    this.timeout = opts.timeout ?? 10000;

    this.proc = pty.spawn(command, args, {
      name: "xterm-256color",
      cols: opts.cols ?? 120,
      rows: opts.rows ?? 30,
      cwd,
      env: {
        ...process.env,
        TERM: "xterm-256color",
        NO_COLOR: "", // Allow colors
        // Use temp dir for chat/calendar/session data so E2E tests don't pollute ~/.wuphf/
        NEX_CLI_DATA_DIR: mkdtempSync(join(tmpdir(), "wuphf-e2e-")),
        ...opts.env,
      } as Record<string, string>,
    });

    this.proc.onData((data: string) => {
      this.output += data;
    });
  }

  /** Get the current raw terminal output. */
  raw(): string {
    return this.output;
  }

  /** Get the current terminal output with ANSI codes stripped. */
  text(): string {
    return stripAnsi(this.output);
  }

  /** Clear captured output (useful between assertions). */
  clearOutput(): void {
    this.output = "";
  }

  /** Type a string (sends each character). */
  type(text: string): void {
    this.proc.write(text);
  }

  /** Press Enter. */
  enter(): void {
    this.proc.write("\r");
  }

  /** Press Escape. */
  escape(): void {
    this.proc.write("\x1b");
  }

  /** Press Tab. */
  tab(): void {
    this.proc.write("\t");
  }

  /** Press arrow up. */
  arrowUp(): void {
    this.proc.write("\x1b[A");
  }

  /** Press arrow down. */
  arrowDown(): void {
    this.proc.write("\x1b[B");
  }

  /** Press Ctrl+C. */
  ctrlC(): void {
    this.proc.write("\x03");
  }

  /** Wait for specific text to appear in terminal output. */
  async waitForText(
    text: string,
    timeoutMs?: number,
  ): Promise<boolean> {
    const deadline = Date.now() + (timeoutMs ?? this.timeout);

    while (Date.now() < deadline) {
      if (this.text().includes(text)) return true;
      await sleep(100);
    }

    return false;
  }

  /** Wait for a regex match in terminal output. */
  async waitForMatch(
    pattern: RegExp,
    timeoutMs?: number,
  ): Promise<RegExpMatchArray | null> {
    const deadline = Date.now() + (timeoutMs ?? this.timeout);

    while (Date.now() < deadline) {
      const match = this.text().match(pattern);
      if (match) return match;
      await sleep(100);
    }

    return null;
  }

  /** Wait a fixed amount of time. */
  async wait(ms: number): Promise<void> {
    await sleep(ms);
  }

  /** Kill the process and wait for cleanup. */
  async kill(): Promise<void> {
    try {
      this.proc.kill();
    } catch {
      // Already dead
    }
    // Give the OS time to release PTY file descriptors
    await sleep(150);
  }

  /** Get the exit code (if process has exited). */
  get pid(): number {
    return this.proc.pid;
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
