import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { submitJoinInvite } from "./joinInvite";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("submitJoinInvite", () => {
  it("posts the trimmed token + display_name with same-origin credentials", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        ok: true,
        redirect: "/#/channels/general",
        display_name: "Maya",
      }),
    );

    const result = await submitJoinInvite({
      token: "abc/def",
      displayName: "Maya",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [path, init] = fetchMock.mock.calls[0];
    expect(path).toBe("/join/abc%2Fdef");
    expect(init?.method).toBe("POST");
    expect(init?.credentials).toBe("same-origin");
    expect(JSON.parse(init?.body as string)).toEqual({ display_name: "Maya" });
    expect(result).toEqual({
      ok: true,
      redirect: "/#/channels/general",
      display_name: "Maya",
    });
  });

  it("normalises a known server error code", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(410, {
        error: "invite_expired_or_used",
        message: "This invite is no longer valid.",
      }),
    );

    const result = await submitJoinInvite({
      token: "expired",
      displayName: "Maya",
    });

    expect(result).toEqual({
      ok: false,
      code: "invite_expired_or_used",
      message: "This invite is no longer valid.",
    });
  });

  it("falls back to 'unknown' when the server returns an unexpected error code", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(418, { error: "teapot", message: "I am a teapot." }),
    );

    const result = await submitJoinInvite({
      token: "anything",
      displayName: "Maya",
    });

    expect(result).toEqual({
      ok: false,
      code: "unknown",
      message: "I am a teapot.",
    });
  });

  it("returns a network failure when fetch itself rejects", async () => {
    fetchMock.mockRejectedValue(new Error("offline"));

    const result = await submitJoinInvite({
      token: "abc",
      displayName: "Maya",
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("network");
      expect(result.message).toContain("offline");
    }
  });

  it("falls back to 'unknown' when the server returns a non-JSON or malformed response", async () => {
    fetchMock.mockResolvedValue(
      new Response("upstream rejected", {
        status: 502,
        headers: { "Content-Type": "text/plain" },
      }),
    );

    const result = await submitJoinInvite({
      token: "abc",
      displayName: "Maya",
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("unknown");
    }
  });

  it("tells the joiner to reload when the server returns 200 with an unreadable body", async () => {
    // The broker may have already set the session cookie before the body
    // got mangled — the user is potentially in, but cannot be told yes.
    fetchMock.mockResolvedValue(
      new Response("not json", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await submitJoinInvite({
      token: "abc",
      displayName: "Maya",
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("unknown");
      expect(result.message).toMatch(/reload/i);
    }
  });
});
