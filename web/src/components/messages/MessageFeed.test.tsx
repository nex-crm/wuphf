import { describe, expect, it } from "vitest";

import type { Message } from "../../api/client";
import { messagesAfterClearMarker } from "./MessageFeed";

describe("messagesAfterClearMarker", () => {
  const messages = [
    { id: "msg-1" },
    { id: "msg-2" },
    { id: "msg-3" },
  ] as Message[];

  it("keeps all messages when there is no clear marker", () => {
    expect(messagesAfterClearMarker(messages, null)).toBe(messages);
  });

  it("returns messages after the clear marker", () => {
    expect(messagesAfterClearMarker(messages, "msg-2")).toEqual([
      { id: "msg-3" },
    ]);
  });

  it("keeps messages when the marker is not in the current page", () => {
    expect(messagesAfterClearMarker(messages, "missing")).toBe(messages);
  });
});
