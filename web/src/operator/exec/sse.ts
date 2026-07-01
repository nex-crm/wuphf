// readEventStream — parse a streaming SSE Response body, calling onFrame for
// each `data:` JSON payload. Frames are separated by a blank line; the closing
// `event: end` / `data: {}` boundary is skipped. Shared by the live exec and
// observe clients so they parse the broker's SSE the same way.

export async function readEventStream(
  res: Response,
  onFrame: (data: unknown) => void,
): Promise<void> {
  if (!res.body) return;
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  const emitFrame = (frame: string): void => {
    for (const line of frame.split("\n")) {
      // Trim strips any trailing "\r" from a CRLF-delimited line, so both LF
      // and CRLF field lines parse identically.
      const trimmed = line.trim();
      if (!trimmed.startsWith("data:")) continue;
      const payload = trimmed.slice(5).trim();
      if (!payload || payload === "{}") continue;
      try {
        onFrame(JSON.parse(payload));
      } catch {
        // A malformed frame is non-fatal — skip it and keep streaming.
      }
    }
  };

  // Find the next event boundary, recognizing both LF (`\n\n`) and CRLF
  // (`\r\n\r\n`) delimiters. Returns the boundary offset and its length so the
  // consumed separator is sliced off cleanly. A stream that uses CRLF would
  // otherwise never match `\n\n` and sit in the buffer until close.
  const nextBoundary = (buf: string): { index: number; length: number } => {
    const lf = buf.indexOf("\n\n");
    const crlf = buf.indexOf("\r\n\r\n");
    if (lf === -1 && crlf === -1) return { index: -1, length: 0 };
    if (crlf === -1 || (lf !== -1 && lf < crlf)) {
      return { index: lf, length: 2 };
    }
    return { index: crlf, length: 4 };
  };

  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      // The stream may close before a trailing blank line arrives (network
      // hiccup, aborted stream, or a final chunk that is not blank-line
      // terminated). Flush any leftover buffer so the last frame is not lost.
      const trailing = buffer.trim();
      if (trailing) emitFrame(trailing);
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    for (;;) {
      const { index, length } = nextBoundary(buffer);
      if (index === -1) break;
      const frame = buffer.slice(0, index);
      buffer = buffer.slice(index + length);
      emitFrame(frame);
    }
  }
}
