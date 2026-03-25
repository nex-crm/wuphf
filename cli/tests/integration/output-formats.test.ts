import { describe, test, beforeAll, afterAll } from "bun:test";
import { expect } from "bun:test";
import { runNex, nexEnv } from "./helpers.ts";
import { startMockServer } from "./mock-server.ts";

let env: Record<string, string>;
let closeMock: () => void;

beforeAll(() => {
  const mock = startMockServer();
  env = nexEnv(mock.url);
  closeMock = mock.close;
});

afterAll(() => closeMock());

describe("output formats", () => {
  test("--format json — object list returns valid JSON", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "list", "--format", "json"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.data).toBeTruthy();
  });

  test("--format quiet — produces no stdout", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "list", "--format", "quiet"],
      { env },
    );
    expect(exitCode).toBe(0);
    expect(stdout).toBe("");
  });

  test("--format text — task list includes readable output", async () => {
    const { stdout, exitCode } = await runNex(
      ["task", "list", "--format", "text"],
      { env },
    );
    expect(exitCode).toBe(0);
    expect(stdout.includes("Fix bug")).toBeTruthy();
  });

  test("default format — piped output defaults to JSON", async () => {
    // When stdout is not a TTY (subprocess), default format is JSON
    const { stdout, exitCode } = await runNex(["object", "list"], { env });
    expect(exitCode).toBe(0);
    // Should be valid JSON
    const data = JSON.parse(stdout);
    expect(data.data).toBeTruthy();
  });

  test("--format json — record get returns all fields", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "get", "rec-1", "--format", "json"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBe("rec-1");
    expect(data.attributes).toBeTruthy();
  });

  test("--format json — search returns structured results", async () => {
    const { stdout, exitCode } = await runNex(
      ["search", "acme", "--format", "json"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.results).toBeTruthy();
  });

  test("--format text — remember shows success indicator", async () => {
    const { stdout, exitCode } = await runNex(
      ["remember", "some fact", "--format", "text"],
      { env },
    );
    expect(exitCode).toBe(0);
    expect(stdout.length > 0).toBeTruthy();
  });

  test("--format json — graph returns nodes and edges", async () => {
    const { stdout, exitCode } = await runNex(
      ["graph", "--format", "json", "--no-open"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    // Graph command outputs path + metadata in json mode
    expect(data.path || data.total_nodes !== undefined).toBeTruthy();
  });
});
