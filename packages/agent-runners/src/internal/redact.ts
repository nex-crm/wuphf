const REDACTION = "<redacted>";

export interface StreamingRedactor {
  redact(text: string): string;
  flush(): string;
}

export function createStreamingRedactor(secrets: readonly string[]): StreamingRedactor {
  const needles = [...new Set(secrets.filter((secret) => secret.length > 0))].sort(
    (left, right) => right.length - left.length,
  );
  return new StatefulStreamingRedactor(needles);
}

export function createSecretStreamingRedactor(secret: string): StreamingRedactor {
  return createStreamingRedactor(secretFragments(secret));
}

class StatefulStreamingRedactor implements StreamingRedactor {
  readonly #maxCarryLength: number;
  readonly #needles: readonly string[];
  #carry = "";
  #carryPrefixChar: string | undefined;

  constructor(needles: readonly string[]) {
    this.#needles = needles;
    this.#maxCarryLength = Math.max(0, ...needles.map((needle) => needle.length - 1));
  }

  redact(text: string): string {
    if (this.#needles.length === 0) return text;
    const combined = this.#carry + text;
    const carryLength = longestNeedlePrefixSuffixLength(
      combined,
      this.#needles,
      this.#maxCarryLength,
    );
    const emitUntil = completeOverlappingMatchEnd(
      combined,
      this.#needles,
      combined.length - carryLength,
    );
    const safePrefix = combined.slice(0, emitUntil);
    const prefixChar = this.#carryPrefixChar;
    const carryPrefixChar = emitUntil > 0 ? combined[emitUntil - 1] : this.#carryPrefixChar;
    this.#carry = combined.slice(emitUntil);
    this.#carryPrefixChar = carryPrefixChar;
    return redactKnownSecrets(safePrefix, this.#needles, prefixChar);
  }

  flush(): string {
    const text = redactKnownSecrets(this.#carry, this.#needles, this.#carryPrefixChar);
    this.#carry = "";
    this.#carryPrefixChar = undefined;
    return text;
  }
}

function completeOverlappingMatchEnd(
  text: string,
  needles: readonly string[],
  initialEmitUntil: number,
): number {
  let emitUntil = initialEmitUntil;
  let changed = true;
  while (changed) {
    changed = false;
    for (const needle of needles) {
      let match = text.indexOf(needle);
      while (match >= 0) {
        const end = match + needle.length;
        if (match < emitUntil && end > emitUntil) {
          emitUntil = end;
          changed = true;
          break;
        }
        match = text.indexOf(needle, match + 1);
      }
      if (changed) break;
    }
  }
  return emitUntil;
}

function longestNeedlePrefixSuffixLength(
  text: string,
  needles: readonly string[],
  maxCarryLength: number,
): number {
  let longest = 0;
  const maxLength = Math.min(text.length, maxCarryLength);
  for (let length = 1; length <= maxLength; length += 1) {
    const suffix = text.slice(text.length - length);
    if (needles.some((needle) => needle.startsWith(suffix))) {
      longest = length;
    }
  }
  return longest;
}

function redactKnownSecrets(text: string, needles: readonly string[], prefixChar?: string): string {
  let output = "";
  let index = 0;
  const fullNeedleLength = needles[0]?.length ?? 0;
  while (index < text.length) {
    const match = needles.find(
      (needle) =>
        text.startsWith(needle, index) &&
        (needle.length === fullNeedleLength ||
          hasTokenBoundary(text, index, needle.length, prefixChar)),
    );
    if (match !== undefined) {
      output += REDACTION;
      index += match.length;
      continue;
    }
    output += text[index] ?? "";
    index += 1;
  }
  return output;
}

function hasTokenBoundary(
  text: string,
  start: number,
  length: number,
  prefixChar?: string,
): boolean {
  const previousChar = start === 0 ? prefixChar : text[start - 1];
  return !isTokenChar(previousChar) && !isTokenChar(text[start + length]);
}

function isTokenChar(char: string | undefined): boolean {
  return char !== undefined && /^[A-Za-z0-9_-]$/.test(char);
}

function secretFragments(secret: string): readonly string[] {
  if (secret.length === 0) return [];
  const fragments = new Set<string>([secret]);
  const bytes = Buffer.from(secret, "utf8");
  addUtf8Fragment(fragments, bytes.subarray(0, Math.min(8, bytes.length)));
  addUtf8Fragment(fragments, bytes.subarray(0, Math.min(12, bytes.length)));
  if (bytes.length > 4) {
    addUtf8Fragment(fragments, bytes.subarray(0, bytes.length - 4));
    addUtf8Fragment(fragments, bytes.subarray(bytes.length - 4));
  }
  return [...fragments].filter((fragment) => fragment.length > 0);
}

function addUtf8Fragment(fragments: Set<string>, bytes: Buffer): void {
  const fragment = bytes.toString("utf8");
  if (fragment.length > 0 && !fragment.includes("\uFFFD")) {
    fragments.add(fragment);
  }
}
