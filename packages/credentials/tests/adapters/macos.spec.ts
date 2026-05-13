import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { MacOSCredentialStore } from "../../src/adapters/macos.ts";
import type { Spawner } from "../../src/index.ts";

const describeOnDarwin = process.platform === "darwin" ? describe : describe.skip;

describeOnDarwin("MacOSCredentialStore", () => {
  it("passes secrets through stdin and includes agent scope in the account", async () => {
    const calls: Array<{ readonly args: readonly string[]; readonly input?: string | undefined }> =
      [];
    const spawner: Spawner = async (_cmd, args, options) => {
      calls.push({ args, input: options?.input });
      return { stdout: "", stderr: "", code: 0 };
    };
    const store = new MacOSCredentialStore({
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
    expect(call?.args).toContain("agent:agent_alpha:scope:openai");
    expect(call?.args.at(-1)).toBe("-w");
    expect(call?.args).not.toContain("fixture-secret-value-do-not-use-0000");
    expect(call?.input).toBe("fixture-secret-value-do-not-use-0000");
  });
});
