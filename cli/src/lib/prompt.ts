/**
 * Minimal interactive prompt helpers using readline.
 */

import { createInterface } from "node:readline";
import { isTTY, style, sym } from "./tui.js";

export async function confirm(message: string, defaultYes = true): Promise<boolean> {
  const suffix = defaultYes ? "[Y/n]" : "[y/N]";
  const rl = createInterface({ input: process.stdin, output: process.stderr });

  return new Promise((resolve) => {
    rl.question(`${message} ${suffix} `, (answer) => {
      rl.close();
      const trimmed = answer.trim().toLowerCase();
      if (!trimmed) {
        resolve(defaultYes);
      } else {
        resolve(trimmed === "y" || trimmed === "yes");
      }
    });
  });
}

export async function choose(message: string, options: string[]): Promise<number> {
  if (!isTTY) {
    // Non-TTY fallback: numbered list
    const rl = createInterface({ input: process.stdin, output: process.stderr });
    for (let i = 0; i < options.length; i++) {
      process.stderr.write(`  ${i + 1}) ${options[i]}\n`);
    }
    return new Promise((resolve) => {
      const prompt = () => {
        rl.question(`${message} `, (answer) => {
          const num = parseInt(answer.trim(), 10);
          if (num >= 1 && num <= options.length) {
            rl.close();
            resolve(num - 1);
            return;
          }
          process.stderr.write(`  Please enter 1-${options.length}\n`);
          prompt();
        });
      };
      prompt();
    });
  }

  // TTY: arrow-key selection
  return new Promise((resolve) => {
    let selected = 0;
    const totalLines = options.length + 2; // title + blank + options

    const draw = (initial = false) => {
      if (!initial) {
        process.stderr.write(`\x1b[${totalLines}A\x1b[J`);
      }
      process.stderr.write(`  ${message}\n\n`);
      for (let i = 0; i < options.length; i++) {
        const isSelected = i === selected;
        const pointer = isSelected ? sym.pointer : " ";
        const label = isSelected ? style.bold(options[i]) : options[i];
        process.stderr.write(`  ${pointer} ${label}\n`);
      }
    };

    process.stdin.setRawMode(true);
    process.stdin.resume();
    process.stdin.setEncoding("utf-8");

    draw(true);

    const onData = (key: string) => {
      if (key === "\x03") {
        // Ctrl+C
        cleanup();
        process.exit(130);
        return;
      }
      if (key === "\r") {
        // Enter
        cleanup();
        // Clear the selection UI
        process.stderr.write(`\x1b[${totalLines}A\x1b[J`);
        process.stderr.write(`  ${message}\n`);
        process.stderr.write(`  ${sym.success} ${options[selected]}\n\n`);
        resolve(selected);
        return;
      }
      if (key === "\x1b[A") {
        selected = Math.max(0, selected - 1);
      } else if (key === "\x1b[B") {
        selected = Math.min(options.length - 1, selected + 1);
      }
      draw();
    };

    const cleanup = () => {
      process.stdin.setRawMode(false);
      process.stdin.removeListener("data", onData);
      process.stdin.pause();
    };

    process.stdin.on("data", onData);
  });
}

export async function ask(message: string, required = false): Promise<string> {
  const rl = createInterface({ input: process.stdin, output: process.stderr });

  return new Promise((resolve) => {
    const prompt = () => {
      rl.question(`${message} `, (answer) => {
        const trimmed = answer.trim();
        if (required && !trimmed) {
          prompt();
          return;
        }
        rl.close();
        resolve(trimmed);
      });
    };
    prompt();
  });
}
