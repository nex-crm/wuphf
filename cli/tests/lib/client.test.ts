import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { NexClient } from "../../src/lib/client.js";
import { AuthError } from "../../src/lib/errors.js";

describe("NexClient", () => {
  it("isAuthenticated returns false without key", () => {
    const client = new NexClient();
    assert.equal(client.isAuthenticated, false);
  });

  it("isAuthenticated returns false with empty key", () => {
    const client = new NexClient("");
    assert.equal(client.isAuthenticated, false);
  });

  it("isAuthenticated returns true with key", () => {
    const client = new NexClient("test-key-123");
    assert.equal(client.isAuthenticated, true);
  });

  it("setApiKey updates authentication state", () => {
    const client = new NexClient();
    assert.equal(client.isAuthenticated, false);
    client.setApiKey("new-key");
    assert.equal(client.isAuthenticated, true);
  });

  it("get throws AuthError when no key", async () => {
    const client = new NexClient();
    await assert.rejects(() => client.get("/test"), AuthError);
  });

  it("post throws AuthError when no key", async () => {
    const client = new NexClient();
    await assert.rejects(() => client.post("/test", {}), AuthError);
  });

  it("put throws AuthError when no key", async () => {
    const client = new NexClient();
    await assert.rejects(() => client.put("/test", {}), AuthError);
  });

  it("delete throws AuthError when no key", async () => {
    const client = new NexClient();
    await assert.rejects(() => client.delete("/test"), AuthError);
  });
});
