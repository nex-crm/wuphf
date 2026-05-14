import { MAX_RUNNER_STDIO_CHUNK_BYTES } from "@wuphf/protocol";

export const DEFAULT_MAX_RUNNER_INPUT_BUFFER_BYTES = 16 * 1024 * 1024;

export class RunnerInputBufferOverflow extends Error {
  override readonly name = "RunnerInputBufferOverflow";

  constructor(readonly maxBytes: number) {
    super(`runner input buffer exceeded ${maxBytes} bytes`);
  }
}

export class BoundedLineBuffer {
  readonly #maxBytes: number;
  #buffer = "";
  #bufferedBytes = 0;

  constructor(maxBytes: number) {
    if (!Number.isSafeInteger(maxBytes) || maxBytes <= 0) {
      throw new Error("BoundedLineBuffer maxBytes must be a positive safe integer");
    }
    this.#maxBytes = maxBytes;
  }

  push(input: string): readonly string[] {
    const lines: string[] = [];
    for (const char of input) {
      if (char === "\n") {
        lines.push(this.#buffer);
        this.#buffer = "";
        this.#bufferedBytes = 0;
        continue;
      }
      this.#buffer += char;
      this.#bufferedBytes += Buffer.byteLength(char, "utf8");
      if (this.#bufferedBytes > this.#maxBytes) {
        throw new RunnerInputBufferOverflow(this.#maxBytes);
      }
    }
    return lines;
  }

  flush(): readonly string[] {
    if (this.#buffer.length === 0) return [];
    const line = this.#buffer;
    this.#buffer = "";
    this.#bufferedBytes = 0;
    return [line];
  }
}

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
