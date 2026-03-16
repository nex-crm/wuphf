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

describe("task commands", () => {
  it("task list — returns tasks", async () => {
    const { stdout, exitCode } = await runNex(["task", "list"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.data, "should have data array");
    assert.equal(data.data.length, 1);
    assert.equal(data.data[0].title, "Fix bug");
  });

  it("task get — returns a single task", async () => {
    const { stdout, exitCode } = await runNex(["task", "get", "task-1"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.id, "task-1");
    assert.equal(data.title, "Fix bug");
  });

  it("task create — creates a task", async () => {
    const { stdout, exitCode } = await runNex(
      ["task", "create", "--title", "Ship feature"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.id, "should have an id");
    assert.equal(data.title, "Ship feature");
  });

  it("task update — updates a task", async () => {
    const { stdout, exitCode } = await runNex(
      ["task", "update", "task-1", "--title", "Fixed bug"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.title, "Fixed bug");
  });

  it("task delete — deletes a task", async () => {
    const { stdout, exitCode } = await runNex(["task", "delete", "task-1"], { env });
    assert.equal(exitCode, 0);
  });
});
