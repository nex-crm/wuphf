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

describe("object commands", () => {
  test("object list — returns all objects", async () => {
    const { stdout, exitCode } = await runNex(["object", "list"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.data).toBeTruthy();
    expect(data.data.length).toBe(2);
    expect(data.data[0].slug).toBe("company");
    expect(data.data[1].slug).toBe("person");
  });

  test("object get — returns a single object", async () => {
    const { stdout, exitCode } = await runNex(["object", "get", "company"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.slug).toBe("company");
    expect(data.name).toBe("Company");
  });

  test("object create — creates an object", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "create", "--name", "Deal", "--slug", "deal", "--type", "deal"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBeTruthy();
    expect(data.slug).toBe("deal");
  });

  test("object update — updates an object", async () => {
    const { stdout, exitCode } = await runNex(
      ["object", "update", "company", "--name", "Companies Updated"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.slug).toBe("company");
  });

  test("object delete — deletes an object", async () => {
    const { stdout, exitCode } = await runNex(["object", "delete", "company"], { env });
    expect(exitCode).toBe(0);
  });
});
