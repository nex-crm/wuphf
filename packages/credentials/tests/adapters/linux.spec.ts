import { forBrokerTests } from "@wuphf/credentials/testing";
import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  assertEncryptedLibsecretCollection,
  LinuxCredentialStore,
} from "../../src/adapters/linux.ts";
import { BasicTextRejected, NoKeyringAvailable } from "../../src/errors.ts";
import type { Spawner } from "../../src/store.ts";

const agentId = asAgentId("agent_alpha");
const broker = forBrokerTests({ agentId });

describe("LinuxCredentialStore", () => {
  it("rejects libsecret basic_text before storing", async () => {
    const calls: string[] = [];
    const spawner: Spawner = async (_cmd, args) => {
      calls.push(args[0] ?? "");
      if (args[0] === "--version") return { stdout: "secret-tool 0.21.4\n", stderr: "", code: 0 };
      if (args[0] === "search") {
        return { stdout: "collection backend: basic_text\n", stderr: "", code: 0 };
      }
      return { stdout: "", stderr: "store must not be called", code: 1 };
    };
    const store = new LinuxCredentialStore({
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await expect(
      store.write({
        broker,
        agentId,
        scope: asCredentialScope("openai"),
        secret: "fixture-secret-value-do-not-use-0000",
      }),
    ).rejects.toBeInstanceOf(BasicTextRejected);
    expect(calls).toEqual(["--version", "search"]);
  });

  it("throws NoKeyringAvailable when secret-tool is missing", async () => {
    const spawner: Spawner = async () => ({
      stdout: "",
      stderr: "secret-tool: command not found",
      code: 127,
    });
    const store = new LinuxCredentialStore({
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await expect(
      store.write({
        broker,
        agentId,
        scope: asCredentialScope("openai"),
        secret: "fixture-secret-value-do-not-use-0000",
      }),
    ).rejects.toBeInstanceOf(NoKeyringAvailable);
  });

  it("parses plaintext collection reports from unknown subprocess output", () => {
    expect(() =>
      assertEncryptedLibsecretCollection({
        stdout: "collection: default\nbackend: unencrypted plain text\n",
        stderr: "",
      }),
    ).toThrow(BasicTextRejected);
    expect(() =>
      assertEncryptedLibsecretCollection({ stdout: "collection: encrypted" }),
    ).not.toThrow();
  });
});
