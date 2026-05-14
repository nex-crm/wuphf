import { forBrokerTests } from "@wuphf/credentials/testing";
import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { MacOSCredentialStore } from "../../src/adapters/macos.ts";
import type { Spawner } from "../../src/store.ts";

const describeOnDarwin = process.platform === "darwin" ? describe : describe.skip;
const agentId = asAgentId("agent_alpha");
const broker = forBrokerTests({ agentId });

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
      broker,
      agentId,
      scope: asCredentialScope("openai"),
      secret: "fixture-secret-value-do-not-use-0000",
    });

    const call = calls[0];
    expect(call).toBeDefined();
    expect(call?.args.join(" ")).toContain("cred_");
    expect(call?.args.at(-1)).toBe("-w");
    expect(call?.args).not.toContain("fixture-secret-value-do-not-use-0000");
    expect(call?.input).toBe("fixture-secret-value-do-not-use-0000");
  });
});
