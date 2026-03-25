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

describe("search command", () => {
  test("search — returns matching results as JSON", async () => {
    const { stdout, exitCode } = await runNex(["search", "acme"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.results).toBeTruthy();
    expect(data.results.length > 0).toBeTruthy();
    expect(data.results[0].primary_value).toBe("Acme Corp");
  });

  test("search — text format includes result name", async () => {
    const { stdout, exitCode } = await runNex(
      ["search", "acme", "--format", "text"],
      { env },
    );
    expect(exitCode).toBe(0);
    expect(stdout.includes("Acme Corp")).toBeTruthy();
  });
});
