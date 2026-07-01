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
    let sep: number;
    while ((sep = buffer.indexOf("\n\n")) !== -1) {
      const frame = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      emitFrame(frame);
    }
  }
}
