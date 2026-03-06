/**
 * Output formatter: json / text / quiet.
 */

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

export function printOutput(data: unknown, format: Format): void {
  const output = formatOutput(data, format);
  if (output !== undefined) {
    process.stdout.write(output + "\n");
  }
}

export function printError(message: string): void {
  process.stderr.write(`Error: ${message}\n`);
}
