import { forBrokerTests } from "@wuphf/credentials/testing";
import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { MacOSCredentialStore } from "../../src/adapters/macos.ts";
import type { Spawner } from "../../src/store.ts";

const agentId = asAgentId("agent_alpha");
const broker = forBrokerTests({ agentId });

describe("MacOSCredentialStore", () => {
  it("passes secrets through stdin and stores agent scope as metadata", async () => {
    const calls: Array<{
      readonly args: readonly string[];
      readonly cmd: string;
      readonly env: NodeJS.ProcessEnv | undefined;
      readonly input?: string | undefined;
    }> = [];
    const spawner: Spawner = async (cmd, args, options) => {
      calls.push({ args, cmd, env: options?.env, input: options?.input });
      return { stdout: "", stderr: "", code: 0 };
    };
    const store = new MacOSCredentialStore({
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await store.write({
      broker,
      agentId,
      scope: asCredentialScope("openai"),
      secret: "fixture-secret-value-do-not-use-0000",
    });

    const call = calls[0];
    expect(call).toBeDefined();
    if (call === undefined) throw new Error("missing security call");
    const account = argAfter(call.args, "-a");

    expect(call.cmd).toBe("/usr/bin/security");
    expect(envValue(call.env, "LC_ALL")).toBe("C");
    expect(account).toMatch(/^cred_/);
    expect(call.args.slice(0, 8)).toEqual([
      "add-generic-password",
      "-U",
      "-a",
      account,
      "-s",
      "wuphf.credentials.test",
      "-l",
      "WUPHF openai credential for agent_alpha",
    ]);
    expect(argAfter(call.args, "-j")).toBe('{"agentId":"agent_alpha","scope":"openai"}');
    expect(call.args.at(-1)).toBe("-w");
    expect(call.args).not.toContain("fixture-secret-value-do-not-use-0000");
    expect(call.input).toBe("fixture-secret-value-do-not-use-0000");
  });
});

function argAfter(args: readonly string[], flag: string): string {
  const index = args.indexOf(flag);
  if (index < 0) throw new Error(`missing ${flag}`);
  const value = args[index + 1];
  if (value === undefined) throw new Error(`missing value for ${flag}`);
  return value;
}

function envValue(env: NodeJS.ProcessEnv | undefined, name: string): string | undefined {
  return env?.[name];
}
