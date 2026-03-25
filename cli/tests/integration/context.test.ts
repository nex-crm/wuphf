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

describe("context commands", () => {
  test("ask — returns AI answer as JSON", async () => {
    const { stdout, exitCode } = await runNex(["ask", "What is the meaning of life?"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.answer).toBe("The answer is 42");
  });

  test("ask — text format shows answer", async () => {
    const { stdout, exitCode } = await runNex(
      ["ask", "What is the meaning of life?", "--format", "text"],
      { env },
    );
    expect(exitCode).toBe(0);
    expect(stdout.includes("42")).toBeTruthy();
  });

  test("remember — stores context", async () => {
    const { stdout, exitCode } = await runNex(
      ["remember", "Important project context"],
      { env },
    );
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.status).toBe("completed");
    expect(data.artifact_id).toBe("art-1");
  });

  test("artifact — retrieves artifact by ID", async () => {
    const { stdout, exitCode } = await runNex(["artifact", "art-1"], { env });
    expect(exitCode).toBe(0);
    const data = JSON.parse(stdout);
    expect(data.id).toBe("art-1");
    expect(data.content).toBeTruthy();
  });
});
