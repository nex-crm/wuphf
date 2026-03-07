/**
 * Output formatter: json / text / quiet.
 */

import { isTTY, sym, style } from "./tui.js";

export type Format = "json" | "text" | "quiet";

function flattenForText(data: unknown, indent = 0): string {
  if (data === null || data === undefined) return "";
  if (typeof data === "string") return data;
  if (typeof data === "number" || typeof data === "boolean") return String(data);

  const prefix = "  ".repeat(indent);

  if (Array.isArray(data)) {
    if (data.length === 0) return `${prefix}(empty)`;
    return data.map((item, i) => `${prefix}[${i}] ${flattenForText(item, indent + 1).trimStart()}`).join("\n");
  }

  if (typeof data === "object") {
    const obj = data as Record<string, unknown>;
    const entries = Object.entries(obj).filter(([, v]) => v !== undefined && v !== null);
    if (entries.length === 0) return `${prefix}(empty)`;
    return entries.map(([k, v]) => {
      if (typeof v === "object" && v !== null) {
        return `${prefix}${k}:\n${flattenForText(v, indent + 1)}`;
      }
      return `${prefix}${k}: ${v}`;
    }).join("\n");
  }

  return String(data);
}

export function formatOutput(data: unknown, format: Format): string | undefined {
  switch (format) {
    case "json":
      return JSON.stringify(data, null, 2);
    case "text":
      return flattenForText(data);
    case "quiet":
      return undefined;
  }
}

export function printOutput(
  data: unknown,
  format: Format,
  ttyFormatter?: (data: unknown) => string | undefined,
): void {
  if (format === "json") {
    process.stdout.write(JSON.stringify(data, null, 2) + "\n");
    return;
  }
  if (format === "quiet") return;

  // text format: try formatter first (works in both TTY and piped output)
  if (ttyFormatter) {
    const result = ttyFormatter(data);
    if (result !== undefined) {
      process.stdout.write(result + "\n");
      return;
    }
  }

  // fallback to flattenForText
  const output = flattenForText(data);
  if (output) {
    process.stdout.write(output + "\n");
  }
}

export function printError(message: string, hint?: string): void {
  process.stderr.write(`\n${sym.error} ${message}\n`);
  if (hint) {
    process.stderr.write(`  ${style.dim(hint)}\n`);
  }
  process.stderr.write("\n");
}
