/**
 * Parse a command input string into an array of tokens.
 * Handles double quotes, single quotes, and whitespace splitting.
 */
export function parseInput(input: string): string[] {
  const tokens: string[] = [];
  const trimmed = input.trim();
  if (!trimmed) return tokens;

  let current = "";
  let inDouble = false;
  let inSingle = false;

  for (let i = 0; i < trimmed.length; i++) {
    const ch = trimmed[i];

    if (ch === '"' && !inSingle) {
      inDouble = !inDouble;
      continue;
    }

    if (ch === "'" && !inDouble) {
      inSingle = !inSingle;
      continue;
    }

    if (ch === " " && !inDouble && !inSingle) {
      if (current.length > 0) {
        tokens.push(current);
        current = "";
      }
      continue;
    }

    current += ch;
  }

  if (current.length > 0) {
    tokens.push(current);
  }

  return tokens;
}
