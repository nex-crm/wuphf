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

describe("note commands", () => {
  test("note list — returns notes", async () => {
    const { stdout, exitCode } = await runNex(["note", "list"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.data).toBeTruthy();
    expect(data.data.length).toBe(1);
    expect(data.data[0].id).toBe("note-1");
  });

  test("note get — returns a single note", async () => {
    const { stdout, exitCode } = await runNex(["note", "get", "note-1"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBe("note-1");
    expect(data.body).toBeTruthy();
  });

  test("note create — creates a note", async () => {
    const { stdout, exitCode } = await runNex(
      ["note", "create", "--title", "New observation"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBeTruthy();
  });

  test("note update — updates a note", async () => {
    const { stdout, exitCode } = await runNex(
      ["note", "update", "note-1", "--title", "Updated note"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.title).toBe("Updated note");
  });

  test("note delete — deletes a note", async () => {
    const { stdout, exitCode } = await runNex(["note", "delete", "note-1"], { env });
    expect(exitCode).toBe(0);
  });
});
