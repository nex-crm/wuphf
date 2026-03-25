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

describe("record commands", () => {
  test("record list — returns records for an object", async () => {
    const { stdout, exitCode } = await runNex(["record", "list", "company"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.data).toBeTruthy();
    expect(data.data.length).toBe(2);
    expect(data.data[0].id).toBe("rec-1");
  });

  test("record get — returns a single record", async () => {
    const { stdout, exitCode } = await runNex(["record", "get", "rec-1"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBe("rec-1");
    expect(data.type).toBe("company");
  });

  test("record create — creates a record", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "create", "company", "--data", '{"name":"TestCo"}'],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBeTruthy();
    expect(data.type).toBe("company");
  });

  test("record upsert — upserts a record", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "upsert", "company", "--match", "domains", "--data", '{"name":"Acme","domains":"acme.com"}'],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBeTruthy();
  });

  test("record update — updates a record", async () => {
    const { stdout, exitCode } = await runNex(
      ["record", "update", "rec-1", "--data", '{"name":"Updated Corp"}'],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBe("rec-1");
  });

  test("record delete — deletes a record", async () => {
    const { stdout, exitCode } = await runNex(["record", "delete", "rec-1"], { env });
    expect(exitCode).toBe(0);
  });

  test("record timeline — returns timeline events", async () => {
    const { stdout, exitCode } = await runNex(["record", "timeline", "rec-1"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.events).toBeTruthy();
    expect(data.events.length).toBe(1);
    expect(data.events[0].summary).toBe("Record created");
  });
});
