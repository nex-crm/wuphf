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

describe("note commands", () => {
  it("note list — returns notes", async () => {
    const { stdout, exitCode } = await runNex(["note", "list"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.data, "should have data array");
    assert.equal(data.data.length, 1);
    assert.equal(data.data[0].id, "note-1");
  });

  it("note get — returns a single note", async () => {
    const { stdout, exitCode } = await runNex(["note", "get", "note-1"], { env });
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.id, "note-1");
    assert.ok(data.body, "should have body");
  });

  it("note create — creates a note", async () => {
    const { stdout, exitCode } = await runNex(
      ["note", "create", "--title", "New observation"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.id, "should have an id");
  });

  it("note update — updates a note", async () => {
    const { stdout, exitCode } = await runNex(
      ["note", "update", "note-1", "--title", "Updated note"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.title, "Updated note");
  });

  it("note delete — deletes a note", async () => {
    const { stdout, exitCode } = await runNex(["note", "delete", "note-1"], { env });
    assert.equal(exitCode, 0);
  });
});
