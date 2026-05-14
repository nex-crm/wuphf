import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { MacOSCredentialStore } from "../../src/adapters/macos.ts";
import type { Spawner } from "../../src/index.ts";

describe("MacOSCredentialStore", () => {
  it("passes secrets through stdin and includes agent scope in the account", async () => {
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
      agentId: asAgentId("agent_alpha"),
      scope: asCredentialScope("openai"),
      secret: "fixture-secret-value-do-not-use-0000",
    });

    const call = calls[0];
    expect(call).toBeDefined();
    expect(call?.cmd).toBe("/usr/bin/security");
    expect(envValue(call?.env, "LC_ALL")).toBe("C");
    expect(call?.args).toContain("agent:agent_alpha:scope:openai");
    expect(call?.args.slice(0, 8)).toEqual([
      "add-generic-password",
      "-U",
      "-a",
      "agent:agent_alpha:scope:openai",
      "-s",
      "wuphf.credentials.test",
      "-l",
      "WUPHF openai credential for agent_alpha",
    ]);
    expect(call?.args.at(-1)).toBe("-w");
    expect(call?.args).not.toContain("fixture-secret-value-do-not-use-0000");
    expect(call?.input).toBe("fixture-secret-value-do-not-use-0000");
  });
});

function envValue(env: NodeJS.ProcessEnv | undefined, name: string): string | undefined {
  return env?.[name];
}
