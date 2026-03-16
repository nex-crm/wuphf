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

describe("task commands", () => {
  test("task list — returns tasks", async () => {
    const { stdout, exitCode } = await runNex(["task", "list"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.data).toBeTruthy();
    expect(data.data.length).toBe(1);
    expect(data.data[0].title).toBe("Fix bug");
  });

  test("task get — returns a single task", async () => {
    const { stdout, exitCode } = await runNex(["task", "get", "task-1"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBe("task-1");
    expect(data.title).toBe("Fix bug");
  });

  test("task create — creates a task", async () => {
    const { stdout, exitCode } = await runNex(
      ["task", "create", "--title", "Ship feature"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBeTruthy();
    expect(data.title).toBe("Ship feature");
  });

  test("task update — updates a task", async () => {
    const { stdout, exitCode } = await runNex(
      ["task", "update", "task-1", "--title", "Fixed bug"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.title).toBe("Fixed bug");
  });

  test("task delete — deletes a task", async () => {
    const { stdout, exitCode } = await runNex(["task", "delete", "task-1"], { env });
    expect(exitCode).toBe(0);
  });
});
