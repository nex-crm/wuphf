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

describe("context commands", () => {
  it("ask — returns AI answer as JSON", async () => {
    const { stdout, exitCode } = await runNex(["ask", "What is the meaning of life?"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.answer, "The answer is 42");
  });

  it("ask — text format shows answer", async () => {
    const { stdout, exitCode } = await runNex(
      ["ask", "What is the meaning of life?", "--format", "text"],
      { env },
    );
    assert.equal(exitCode, 0);
    assert.ok(stdout.includes("42"), "output should contain the answer");
  });

  it("remember — stores context", async () => {
    const { stdout, exitCode } = await runNex(
      ["remember", "Important project context"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.status, "completed");
    assert.equal(data.artifact_id, "art-1");
  });

  it("artifact — retrieves artifact by ID", async () => {
    const { stdout, exitCode } = await runNex(["artifact", "art-1"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.id, "art-1");
    assert.ok(data.content, "should have content");
  });
});
