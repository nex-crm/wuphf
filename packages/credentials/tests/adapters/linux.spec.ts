import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  assertEncryptedLibsecretCollection,
  LinuxCredentialStore,
} from "../../src/adapters/linux.ts";
import {
  BasicTextRejected,
  KeychainCommandFailed,
  NoKeyringAvailable,
  NotFound,
} from "../../src/errors.ts";
import type { Spawner } from "../../src/index.ts";

describe("LinuxCredentialStore", () => {
  it("rejects libsecret basic_text before storing", async () => {
    const calls: string[] = [];
    const spawner: Spawner = async (cmd, args) => {
      expect(cmd).toBe("/usr/bin/secret-tool");
      calls.push(args[0] ?? "");
      if (args[0] === "--version") return { stdout: "secret-tool 0.21.4\n", stderr: "", code: 0 };
      if (args[0] === "search") {
        return { stdout: "collection backend: basic_text\n", stderr: "", code: 0 };
      }
      return { stdout: "", stderr: "store must not be called", code: 1 };
    };
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await expect(
      store.write({
        agentId: asAgentId("agent_alpha"),
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
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await expect(
      store.write({
        agentId: asAgentId("agent_alpha"),
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

  it("falls through from an empty failed probe to the positive gdbus check", async () => {
    const calls: Array<{ readonly args: readonly string[]; readonly cmd: string }> = [];
    const spawner: Spawner = async (cmd, args) => {
      calls.push({ args, cmd });
      if (args[0] === "--version") return ok("secret-tool 0.21.4\n");
      if (args[0] === "search") return { stdout: "", stderr: "", code: 1 };
      if (cmd === "/usr/bin/gdbus") return ok("@a{sv} {}");
      if (args[0] === "store") return ok("");
      if (args[0] === "lookup") return ok("fixture-secret-value-do-not-use-0000\n");
      return { stdout: "", stderr: `unexpected ${String(args[0])}`, code: 1 };
    };
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await expect(
      store.write({
        agentId: asAgentId("agent_alpha"),
        scope: asCredentialScope("openai"),
        secret: "fixture-secret-value-do-not-use-0000",
      }),
    ).resolves.toMatchObject({ agentId: "agent_alpha", scope: "openai" });

    expect(calls.map((call) => call.args[0])).toEqual([
      "--version",
      "search",
      "call",
      "store",
      "lookup",
    ]);
  });

  it("rejects basic_text warnings even when the probe exits nonzero", async () => {
    const calls: string[] = [];
    const spawner: Spawner = async (_cmd, args) => {
      calls.push(args[0] ?? "");
      if (args[0] === "--version") return ok("secret-tool 0.21.4\n");
      if (args[0] === "search") return { stdout: "", stderr: "backend basic_text", code: 1 };
      return { stdout: "", stderr: `unexpected ${String(args[0])}`, code: 1 };
    };
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await expect(
      store.write({
        agentId: asAgentId("agent_alpha"),
        scope: asCredentialScope("openai"),
        secret: "fixture-secret-value-do-not-use-0000",
      }),
    ).rejects.toBeInstanceOf(BasicTextRejected);
    expect(calls).toEqual(["--version", "search"]);
  });

  it("clears a just-written entry when post-write readback sees basic_text", async () => {
    const calls: string[] = [];
    const spawner: Spawner = async (cmd, args) => {
      calls.push(args[0] ?? "");
      if (args[0] === "--version") return ok("secret-tool 0.21.4\n");
      if (args[0] === "search") return ok("collection: encrypted\n");
      if (cmd === "/usr/bin/gdbus") return ok("@a{sv} {}");
      if (args[0] === "store") return ok("");
      if (args[0] === "lookup") return { stdout: "", stderr: "basic_text backend", code: 0 };
      if (args[0] === "clear") return ok("");
      return { stdout: "", stderr: `unexpected ${String(args[0])}`, code: 1 };
    };
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await expect(
      store.write({
        agentId: asAgentId("agent_alpha"),
        scope: asCredentialScope("openai"),
        secret: "fixture-secret-value-do-not-use-0000",
      }),
    ).rejects.toBeInstanceOf(BasicTextRejected);
    expect(calls).toContain("clear");
  });

  it("classifies lookup failures narrowly instead of returning NotFound for every nonzero exit", async () => {
    const cases = [
      { expected: KeychainCommandFailed, stderr: "" },
      { expected: KeychainCommandFailed, stderr: "Zugriff verweigert" },
      { expected: KeychainCommandFailed, stderr: "permission denied" },
      { expected: NoKeyringAvailable, stderr: "Cannot autolaunch D-Bus without X11" },
      { expected: NoKeyringAvailable, stderr: "D-Bus service unavailable" },
      { expected: NotFound, stderr: "No matching items" },
    ] as const;

    for (const testCase of cases) {
      const store = new LinuxCredentialStore({
        enforceTrustedCommand: false,
        serviceName: "wuphf.credentials.test",
        spawner: readyLinuxSpawner({ lookup: { stdout: "", stderr: testCase.stderr, code: 1 } }),
      });
      const handle = await store.write({
        agentId: asAgentId("agent_alpha"),
        scope: asCredentialScope("openai"),
        secret: "fixture-secret-value-do-not-use-0000",
      });

      await expect(store.read(handle)).rejects.toBeInstanceOf(testCase.expected);
    }
  });

  it("spawns the store command with service and scoped-account attributes", async () => {
    let storeArgs: readonly string[] = [];
    const spawner = readyLinuxSpawner({
      onStore: (args) => {
        storeArgs = args;
      },
    });
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await store.write({
      agentId: asAgentId("agent_alpha"),
      scope: asCredentialScope("openai"),
      secret: "fixture-secret-value-do-not-use-0000",
    });

    expect(storeArgs.slice(0, 3)).toEqual([
      "store",
      "--label",
      "WUPHF openai credential for agent_alpha",
    ]);
    expect(storeArgs).toEqual(
      expect.arrayContaining([
        "wuphf_service",
        "wuphf.credentials.test",
        "wuphf_account",
        "agent:agent_alpha:scope:openai",
      ]),
    );
  });
});

function readyLinuxSpawner(options: {
  readonly lookup?: { readonly code: number; readonly stderr: string; readonly stdout: string };
  readonly onStore?: (args: readonly string[]) => void;
}): Spawner {
  let lookupCount = 0;
  return async (cmd, args) => {
    if (args[0] === "--version") return ok("secret-tool 0.21.4\n");
    if (args[0] === "search") return ok("collection: encrypted\n");
    if (cmd === "/usr/bin/gdbus") return ok("@a{sv} {}");
    if (args[0] === "store") {
      options.onStore?.(args);
      return ok("");
    }
    if (args[0] === "lookup") {
      lookupCount += 1;
      if (lookupCount === 1) return ok("fixture-secret-value-do-not-use-0000\n");
      return options.lookup ?? ok("fixture-secret-value-do-not-use-0000\n");
    }
    if (args[0] === "clear") return ok("");
    return { stdout: "", stderr: `unexpected ${String(args[0])}`, code: 1 };
  };
}

function ok(stdout: string) {
  return { stdout, stderr: "", code: 0 };
}
