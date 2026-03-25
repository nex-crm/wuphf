import { describe, test, expect } from "bun:test";
import { NexClient } from "../../src/lib/client.ts";
import { AuthError } from "../../src/lib/errors.ts";

describe("NexClient", () => {
  test("isAuthenticated returns false without key", () => {
    const client = new NexClient();
    expect(client.isAuthenticated).toBe(false);
  });

  test("isAuthenticated returns false with empty key", () => {
    const client = new NexClient("");
    expect(client.isAuthenticated).toBe(false);
  });

  test("isAuthenticated returns true with key", () => {
    const client = new NexClient("test-key-123");
    expect(client.isAuthenticated).toBe(true);
  });

  test("setApiKey updates authentication state", () => {
    const client = new NexClient();
    expect(client.isAuthenticated).toBe(false);
    client.setApiKey("new-key");
    expect(client.isAuthenticated).toBe(true);
  });

  test("get throws AuthError when no key", async () => {
    const client = new NexClient();
    expect(() => client.get("/test")).toThrow(AuthError);
  });

  test("post throws AuthError when no key", async () => {
    const client = new NexClient();
    expect(() => client.post("/test", {})).toThrow(AuthError);
  });

  test("put throws AuthError when no key", async () => {
    const client = new NexClient();
    expect(() => client.put("/test", {})).toThrow(AuthError);
  });

  test("delete throws AuthError when no key", async () => {
    const client = new NexClient();
    expect(() => client.delete("/test")).toThrow(AuthError);
  });
});
