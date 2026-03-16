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

describe("output formats", () => {
  it("--format json — object list returns valid JSON", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "list", "--format", "json"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.data, "should have data");
  });

  it("--format quiet — produces no stdout", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "list", "--format", "quiet"],
      { env },
    );
    assert.equal(exitCode, 0);
    assert.equal(stdout, "", "quiet format should produce no output");
  });

  it("--format text — task list includes readable output", async () => {
    const { stdout, exitCode } = await runNex(
      ["task", "list", "--format", "text"],
      { env },
    );
    assert.equal(exitCode, 0);
    assert.ok(stdout.includes("Fix bug"), "text output should include task title");
  });

  it("default format — piped output defaults to JSON", async () => {
    // When stdout is not a TTY (subprocess), default format is JSON
    const { stdout, exitCode } = await runNex(["object", "list"], { env });
    assert.equal(exitCode, 0);
    // Should be valid JSON
    const data = JSON.parse(stdout);
    assert.ok(data.data, "default piped output should be JSON");
  });

  it("--format json — record get returns all fields", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "get", "rec-1", "--format", "json"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.equal(data.id, "rec-1");
    assert.ok(data.attributes, "should include attributes");
  });

  it("--format json — search returns structured results", async () => {
    const { stdout, exitCode } = await runNex(
      ["search", "acme", "--format", "json"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    assert.ok(data.results, "should have results");
  });

  it("--format text — remember shows success indicator", async () => {
    const { stdout, exitCode } = await runNex(
      ["remember", "some fact", "--format", "text"],
      { env },
    );
    assert.equal(exitCode, 0);
    assert.ok(stdout.length > 0, "text output should not be empty");
  });

  it("--format json — graph returns nodes and edges", async () => {
    const { stdout, exitCode } = await runNex(
      ["graph", "--format", "json", "--no-open"],
      { env },
    );
    assert.equal(exitCode, 0);
    const data = JSON.parse(stdout);
    // Graph command outputs path + metadata in json mode
    assert.ok(data.path || data.total_nodes !== undefined, "should have graph data");
  });
});
