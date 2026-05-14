import { MAX_RUNNER_STDIO_CHUNK_BYTES } from "@wuphf/protocol";

export function chunkStdio(text: string): readonly string[] {
  if (text.length === 0) return [];
  const chunks: string[] = [];
  let current = "";
  let currentBytes = 0;
  for (const char of text) {
    const charBytes = Buffer.byteLength(char, "utf8");
    if (current.length > 0 && currentBytes + charBytes > MAX_RUNNER_STDIO_CHUNK_BYTES) {
      chunks.push(current);
      current = "";
      currentBytes = 0;
    }
    current += char;
    currentBytes += charBytes;
  }
  if (current.length > 0) chunks.push(current);
  return chunks;
}
