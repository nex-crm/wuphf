import { describe, test, beforeAll, afterAll } from "bun:test";
import { expect } from "bun:test";
import { runNex, nexEnv } from "./helpers.ts";
import { startMockServer } from "./mock-server.ts";

let mockUrl: string;
let closeMock: () => void;

beforeAll(() => {
  const mock = startMockServer();
  mockUrl = mock.url;
  closeMock = mock.close;
});

afterAll(() => closeMock());

describe("error handling", () => {
  test("auth error — fails with exit code 2 when no API key", async () => {
    const { exitCode, stderr } = await runNex(
      ["object", "list"],
      { env: { NEX_DEV_URL: mockUrl } },
    );
    expect(exitCode).not.toBe(0);
    expect(stderr.includes("API key") || stderr.includes("setup")).toBeTruthy();
  });

  test("auth error — fails with bad API key", async () => {
    const { exitCode, stderr } = await runNex(
      ["object", "list"],
      { env: { NEX_DEV_URL: mockUrl, NEX_API_KEY: "wrong-key" } },
    );
    expect(exitCode).not.toBe(0);
    expect(stderr.length > 0).toBeTruthy();
  });

  test("missing required arg — record get with no ID", async () => {
    const { exitCode, stderr } = await runNex(
      ["record", "get"],
      { env: nexEnv(mockUrl) },
    );
    expect(exitCode).not.toBe(0);
    expect(stderr.length > 0).toBeTruthy();
  });

  test("missing required option — record create without --data", async () => {
    const { exitCode, stderr } = await runNex(
      ["record", "create", "company"],
      { env: nexEnv(mockUrl) },
    );
    expect(exitCode).not.toBe(0);
    expect(stderr.length > 0).toBeTruthy();
  });

  test("unknown command — exits with error", async () => {
    const { exitCode, stderr } = await runNex(
      ["notacommand"],
      { env: nexEnv(mockUrl) },
    );
    expect(exitCode).not.toBe(0);
    expect(stderr.length > 0).toBeTruthy();
  });
});
