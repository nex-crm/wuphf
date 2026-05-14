const REDACTION = "<redacted>";

export type SecretRedactor = (text: string) => string;

export function createSecretRedactor(secret: string): SecretRedactor {
  const needles = [...secretFragments(secret)].sort((left, right) => right.length - left.length);
  return (text) => {
    let redacted = text;
    for (const needle of needles) {
      redacted = redacted.split(needle).join(REDACTION);
    }
    return redacted;
  };
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
