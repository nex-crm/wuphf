import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { WindowsCredentialStore } from "../../src/adapters/windows.ts";
import type { Spawner } from "../../src/index.ts";

const describeOnWindows = process.platform === "win32" ? describe : describe.skip;

describeOnWindows("WindowsCredentialStore", () => {
  it("uses PowerShell Credential Manager APIs without putting the secret in args", async () => {
    const calls: Array<{ readonly args: readonly string[]; readonly input?: string | undefined }> =
      [];
    const spawner: Spawner = async (_cmd, args, options) => {
      calls.push({ args, input: options?.input });
      return { stdout: "", stderr: "", code: 0 };
    };
    const store = new WindowsCredentialStore({
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
    expect(call?.args.join(" ")).not.toContain("fixture-secret-value-do-not-use-0000");
    expect(call?.input).toBe("fixture-secret-value-do-not-use-0000");
    expect(decodePowerShellScript(call?.args ?? [])).toContain(
      "$target = 'wuphf.credentials.test:agent:agent_alpha:scope:openai'",
    );
  });
});

function decodePowerShellScript(args: readonly string[]): string {
  const index = args.indexOf("-EncodedCommand");
  if (index < 0) throw new Error("missing -EncodedCommand");
  const encoded = args[index + 1];
  if (encoded === undefined) throw new Error("missing encoded command");
  return Buffer.from(encoded, "base64").toString("utf16le");
}
