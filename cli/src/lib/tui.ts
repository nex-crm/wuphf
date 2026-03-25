/**
 * Zero-dependency TUI primitives for the WUPHF CLI.
 *
 * Respects NO_COLOR, TERM=dumb, and non-TTY pipes.
 * Ephemeral output (spinners) goes to stderr; structured output to stdout.
 */

// --- Environment ---

export const isTTY: boolean =
  !!process.stdout.isTTY &&
  !process.env.NO_COLOR &&
  process.env.TERM !== "dumb";

const ANSI_RE = /\x1b\[[0-9;]*m/g;

export function stripAnsi(s: string): string {
  return s.replace(ANSI_RE, "");
}

export function visibleLength(s: string): number {
  return stripAnsi(s).length;
}

// --- Style primitives ---

function wrap(open: string, close: string): (s: string) => string {
  if (!isTTY) return (s) => s;
  return (s) => `${open}${s}${close}`;
}

export const style = {
  bold: wrap("\x1b[1m", "\x1b[22m"),
  dim: wrap("\x1b[2m", "\x1b[22m"),
  green: wrap("\x1b[32m", "\x1b[39m"),
  red: wrap("\x1b[31m", "\x1b[39m"),
  yellow: wrap("\x1b[33m", "\x1b[39m"),
  cyan: wrap("\x1b[36m", "\x1b[39m"),
};

// --- Symbols ---

const unicode = isTTY && process.platform !== "win32";

export const sym = {
  success: isTTY ? style.green(unicode ? "\u2714" : "\u221A") : "ok",
  error: isTTY ? style.red(unicode ? "\u2716" : "\u00D7") : "error",
  warning: isTTY ? style.yellow(unicode ? "\u26A0" : "!") : "!",
  info: isTTY ? style.cyan(unicode ? "\u2139" : "i") : "i",
  bullet: isTTY ? style.green("\u25CF") : "*",
  bulletDim: isTTY ? style.dim("\u25CB") : "o",
  pointer: isTTY ? style.cyan("\u203A") : ">",
  line: "\u2500",
  corner: "\u2514",
  pipe: "\u2502",
  tee: "\u251C",
};

// --- Exit hint ---

export const exitHint: string = style.dim("(Ctrl+C to exit)");

// --- Helpers ---

function padRight(s: string, len: number): string {
  const vl = visibleLength(s);
  return vl >= len ? s : s + " ".repeat(len - vl);
}

// --- Components ---

export function heading(title: string): string {
  const plain = stripAnsi(title);
  return `  ${style.bold(title)}\n  ${sym.line.repeat(plain.length)}`;
}

export function keyValue(
  entries: [string, string][],
  opts?: { labelWidth?: number },
): string {
  const width = opts?.labelWidth ?? Math.max(...entries.map(([k]) => k.length)) + 2;
  return entries
    .map(([k, v]) => `  ${padRight(style.dim(k), width + (style.dim("").length))}${v}`)
    .join("\n");
}

export function table(opts: {
  headers: string[];
  rows: string[][];
  alignRight?: number[];
}): string {
  const { headers, rows, alignRight = [] } = opts;
  const cols = headers.length;

  // Compute column widths
  const widths: number[] = headers.map((h) => visibleLength(h));
  for (const row of rows) {
    for (let i = 0; i < cols; i++) {
      widths[i] = Math.max(widths[i], visibleLength(row[i] ?? ""));
    }
  }

  const pad = (s: string, i: number): string => {
    const diff = widths[i] - visibleLength(s);
    if (diff <= 0) return s;
    return alignRight.includes(i) ? " ".repeat(diff) + s : s + " ".repeat(diff);
  };

  const headerLine =
    "  " + headers.map((h, i) => style.bold(pad(h, i))).join("  ");
  const sepLine =
    "  " + widths.map((w) => sym.line.repeat(w)).join("  ");
  const dataLines = rows.map(
    (row) => "  " + row.map((cell, i) => pad(cell ?? "", i)).join("  "),
  );

  return [headerLine, sepLine, ...dataLines].join("\n");
}

export function badge(
  label: string,
  variant: "success" | "error" | "warning" | "dim",
): string {
  switch (variant) {
    case "success":
      return `${style.green("\u25CF")} ${style.green(label)}`;
    case "error":
      return `${style.red("\u25CF")} ${style.red(label)}`;
    case "warning":
      return `${style.yellow("\u25CF")} ${style.yellow(label)}`;
    case "dim":
      return `${style.dim("\u25CB")} ${style.dim(label)}`;
  }
}

export function tree(
  items: Array<{ label: string; children?: string[] }>,
): string {
  const lines: string[] = [];
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    const isLast = i === items.length - 1;
    const prefix = isLast ? sym.corner : sym.tee;
    lines.push(`  ${prefix} ${item.label}`);
    if (item.children) {
      const indent = isLast ? "    " : `  ${sym.pipe} `;
      for (let j = 0; j < item.children.length; j++) {
        const childPrefix = j === item.children.length - 1 ? sym.corner : sym.tee;
        lines.push(`  ${indent}${childPrefix} ${item.children[j]}`);
      }
    }
  }
  return lines.join("\n");
}

export function box(title: string, content: string): string {
  const contentLines = content.split("\n");
  const maxContent = Math.max(
    ...contentLines.map((l) => visibleLength(l)),
    visibleLength(title) + 2,
  );
  const innerWidth = maxContent + 4; // 2 padding each side

  const topRule =
    "\u256D\u2500 " +
    style.bold(title) +
    " " +
    "\u2500".repeat(Math.max(0, innerWidth - visibleLength(title) - 3)) +
    "\u256E";

  const emptyLine = "\u2502" + " ".repeat(innerWidth) + "\u2502";

  const bodyLines = contentLines.map((l) => {
    const padding = innerWidth - visibleLength(l) - 2;
    return "\u2502  " + l + " ".repeat(Math.max(0, padding)) + "\u2502";
  });

  const bottomRule = "\u2570" + "\u2500".repeat(innerWidth) + "\u256F";

  return [topRule, emptyLine, ...bodyLines, emptyLine, bottomRule].join("\n");
}

// --- Spinner ---

const SPINNER_FRAMES = ["\u280B", "\u2819", "\u2839", "\u2838", "\u283C", "\u2834", "\u2826", "\u2827", "\u2807", "\u280F"];

export function spinner(message: string): {
  update: (msg: string) => void;
  succeed: (msg: string) => void;
  fail: (msg: string) => void;
  stop: () => void;
} {
  if (!isTTY) {
    // Non-TTY: just print the message
    process.stderr.write(`  ${message}\n`);
    return {
      update: (msg: string) => process.stderr.write(`  ${msg}\n`),
      succeed: (msg: string) => process.stderr.write(`  ${msg}\n`),
      fail: (msg: string) => process.stderr.write(`  ${msg}\n`),
      stop: () => {},
    };
  }

  let frame = 0;
  let currentMsg = message;
  let stopped = false;

  const clear = () => {
    process.stderr.write("\r\x1b[K");
  };

  const render = () => {
    clear();
    process.stderr.write(
      `  ${style.cyan(SPINNER_FRAMES[frame % SPINNER_FRAMES.length])} ${currentMsg}`,
    );
  };

  const interval = setInterval(() => {
    if (stopped) return;
    frame++;
    render();
  }, 80);

  render();

  return {
    update(msg: string) {
      currentMsg = msg;
      render();
    },
    succeed(msg: string) {
      stopped = true;
      clearInterval(interval);
      clear();
      process.stderr.write(`  ${sym.success} ${msg}\n`);
    },
    fail(msg: string) {
      stopped = true;
      clearInterval(interval);
      clear();
      process.stderr.write(`  ${sym.error} ${msg}\n`);
    },
    stop() {
      stopped = true;
      clearInterval(interval);
      clear();
    },
  };
}

// --- Interactive Select ---

export function interactiveSelect<T>(opts: {
  title: string;
  items: T[];
  render: (item: T, selected: boolean) => string;
}): Promise<T | null> {
  const { title, items, render: renderItem } = opts;

  if (!isTTY || items.length === 0) {
    // Non-TTY fallback: print list, return null
    process.stderr.write(`  ${title}\n\n`);
    for (let i = 0; i < items.length; i++) {
      process.stderr.write(`  ${i + 1}) ${renderItem(items[i], false)}\n`);
    }
    return Promise.resolve(null);
  }

  return new Promise((resolve) => {
    let selected = 0;
    const totalLines = items.length + 2; // title + blank + items

    const draw = (initial = false) => {
      if (!initial) {
        // Move cursor up and clear
        process.stderr.write(`\x1b[${totalLines}A\x1b[J`);
      }
      process.stderr.write(`  ${title}  ${exitHint}\n\n`);
      for (let i = 0; i < items.length; i++) {
        const isSelected = i === selected;
        const pointer = isSelected ? `${sym.pointer} ` : "  ";
        process.stderr.write(`  ${pointer}${renderItem(items[i], isSelected)}\n`);
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
        resolve(null);
        return;
      }
      if (key === "\r") {
        // Enter
        cleanup();
        resolve(items[selected]);
        return;
      }
      if (key === "\x1b[A") {
        // Up
        selected = Math.max(0, selected - 1);
      } else if (key === "\x1b[B") {
        // Down
        selected = Math.min(items.length - 1, selected + 1);
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
