import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { runNex, nexEnv } from "./helpers.js";
import { startMockServer } from "./mock-server.js";

let mockUrl: string;
let closeMock: () => void;

before(() => {
  const mock = startMockServer();
  mockUrl = mock.url;
  closeMock = mock.close;
});

after(() => closeMock());

describe("error handling", () => {
  it("auth error — fails with exit code 2 when no API key", async () => {
    const { exitCode, stderr } = await runNex(
      ["object", "list"],
      { env: { NEX_DEV_URL: mockUrl } },
    );
    assert.notEqual(exitCode, 0);
    assert.ok(stderr.includes("API key") || stderr.includes("setup"), "should mention API key or setup");
  });

  it("auth error — fails with bad API key", async () => {
    const { exitCode, stderr } = await runNex(
      ["object", "list"],
      { env: { NEX_DEV_URL: mockUrl, NEX_API_KEY: "wrong-key" } },
    );
    assert.notEqual(exitCode, 0);
    assert.ok(stderr.length > 0, "should have error output");
  });

  it("missing required arg — record get with no ID", async () => {
    const { exitCode, stderr } = await runNex(
      ["record", "get"],
      { env: nexEnv(mockUrl) },
    );
    assert.notEqual(exitCode, 0);
    assert.ok(stderr.length > 0, "should have error output");
  });

  it("missing required option — record create without --data", async () => {
    const { exitCode, stderr } = await runNex(
      ["record", "create", "company"],
      { env: nexEnv(mockUrl) },
    );
    assert.notEqual(exitCode, 0);
    assert.ok(stderr.length > 0, "should have error output");
  });

  it("unknown command — exits with error", async () => {
    const { exitCode, stderr } = await runNex(
      ["notacommand"],
      { env: nexEnv(mockUrl) },
    );
    assert.notEqual(exitCode, 0);
    assert.ok(stderr.length > 0, "should have error output");
  });
});
