import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { runNex, nexEnv } from "./helpers.js";
import { startMockServer } from "./mock-server.js";

let env: Record<string, string>;
let closeMock: () => void;

before(() => {
  const mock = startMockServer();
  env = nexEnv(mock.url);
  closeMock = mock.close;
});

after(() => closeMock());

describe("search command", () => {
  it("search — returns matching results as JSON", async () => {
    const { stdout, exitCode } = await runNex(["search", "acme"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.results, "should have results");
    assert.ok(data.results.length > 0, "should have at least one result");
    assert.equal(data.results[0].primary_value, "Acme Corp");
  });

  it("search — text format includes result name", async () => {
    const { stdout, exitCode } = await runNex(
      ["search", "acme", "--format", "text"],
      { env },
    );
    assert.equal(exitCode, 0);
    assert.ok(stdout.includes("Acme Corp"), "output should contain Acme Corp");
  });
});
