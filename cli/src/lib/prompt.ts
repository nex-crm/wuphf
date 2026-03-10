/**
 * Minimal interactive prompt helpers using readline.
 */

import { createInterface } from "node:readline";
import { isTTY, style, sym, exitHint } from "./tui.js";

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
      process.stderr.write(`  ${message}  ${exitHint}\n\n`);
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

/**
 * Multi-select chooser with space to toggle, enter to confirm.
 * Returns indices of selected items.
 */
export async function multiSelect(
  message: string,
  options: Array<{ label: string; checked?: boolean; disabled?: boolean }>,
): Promise<number[]> {
  const selected = options.map((o) => !!o.checked);

  if (!isTTY) {
    // Non-TTY fallback: return pre-checked items
    return selected.reduce<number[]>((acc, v, i) => (v ? [...acc, i] : acc), []);
  }

  return new Promise((resolve) => {
    let cursor = 0;
    const totalLines = options.length + 3; // title + hint + blank + options

    const draw = (initial = false) => {
      if (!initial) {
        process.stderr.write(`\x1b[${totalLines}A\x1b[J`);
      }
      process.stderr.write(`  ${message}  ${exitHint}\n`);
      process.stderr.write(`  ${style.dim("(space to toggle, enter to confirm, s to skip all)")}\n\n`);
      for (let i = 0; i < options.length; i++) {
        const isCursor = i === cursor;
        const pointer = isCursor ? sym.pointer : " ";
        const isChecked = selected[i];
        const isDisabled = options[i].disabled;
        const checkbox = isDisabled
          ? style.green("\u25A0")   // filled square for already connected
          : isChecked
            ? style.cyan("\u25A0") // filled square for selected
            : "\u25A1";           // empty square
        const label = isDisabled
          ? style.dim(options[i].label)
          : isCursor
            ? style.bold(options[i].label)
            : options[i].label;
        process.stderr.write(`  ${pointer} ${checkbox} ${label}\n`);
      }
    };

    process.stdin.setRawMode(true);
    process.stdin.resume();
    process.stdin.setEncoding("utf-8");

    draw(true);

    const onData = (key: string) => {
      if (key === "\x03") {
        cleanup();
        process.stderr.write(`\x1b[${totalLines}A\x1b[J`);
        process.stderr.write(`  ${message}\n`);
        process.stderr.write(`  ${style.dim("Skipped")}\n\n`);
        resolve([]);
        return;
      }
      if (key === "s" || key === "S") {
        cleanup();
        process.stderr.write(`\x1b[${totalLines}A\x1b[J`);
        process.stderr.write(`  ${message}\n`);
        process.stderr.write(`  ${style.dim("Skipped")}\n\n`);
        resolve([]);
        return;
      }
      if (key === "\r") {
        cleanup();
        const result = selected.reduce<number[]>((acc, v, i) => {
          if (v && !options[i].disabled) acc.push(i);
          return acc;
        }, []);
        process.stderr.write(`\x1b[${totalLines}A\x1b[J`);
        process.stderr.write(`  ${message}\n`);
        if (result.length > 0) {
          const names = result.map((i) => options[i].label).join(", ");
          process.stderr.write(`  ${sym.success} ${names}\n\n`);
        } else {
          process.stderr.write(`  ${style.dim("None selected")}\n\n`);
        }
        resolve(result);
        return;
      }
      if (key === " ") {
        if (!options[cursor].disabled) {
          selected[cursor] = !selected[cursor];
        }
      } else if (key === "\x1b[A") {
        cursor = Math.max(0, cursor - 1);
      } else if (key === "\x1b[B") {
        cursor = Math.min(options.length - 1, cursor + 1);
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
