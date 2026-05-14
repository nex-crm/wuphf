import { forBrokerTests } from "@wuphf/credentials/testing";
import { asAgentId, asCredentialHandleId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { WindowsCredentialStore } from "../../src/adapters/windows.ts";
import type { Spawner } from "../../src/store.ts";

const agentId = asAgentId("agent_alpha");
const broker = forBrokerTests({ agentId });

describe("WindowsCredentialStore", () => {
  it("uses PowerShell Credential Manager APIs without putting the secret in args", async () => {
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
    const store = new WindowsCredentialStore({
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
    if (call === undefined) throw new Error("missing powershell call");

    expect(call.cmd).toMatch(/\\System32\\WindowsPowerShell\\v1\.0\\powershell\.exe$/);
    expect(envValue(call.env, "LC_ALL")).toBe("C");
    expect(call.args.join(" ")).not.toContain("fixture-secret-value-do-not-use-0000");
    expect(call.input).toBe("fixture-secret-value-do-not-use-0000");

    const script = decodePowerShellScript(call.args);
    expect(script).toContain("$target = 'wuphf.credentials.test:cred_");
    expect(script).toContain('$comment = \'{"agentId":"agent_alpha","scope":"openai"}\'');
    expect(script).toContain("[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)");
    expect(script).toContain("[Text.Encoding]::UTF8.GetBytes([Console]::In.ReadToEnd())");
  });

  it("writes read output as explicit UTF-8 bytes", async () => {
    let readScript = "";
    const spawner: Spawner = async (_cmd, args) => {
      const script = decodePowerShellScript(args);
      if (script.includes("CredRead($target")) readScript = script;
      return { stdout: "fixture-こんにちは-secret", stderr: "", code: 0 };
    };
    const store = new WindowsCredentialStore({
      enforceTrustedCommand: false,
      serviceName: "wuphf.credentials.test",
      spawner,
    });

    await store.read({
      broker,
      agentId,
      handleId: asCredentialHandleId("cred_0123456789ABCDEFGHIJKLMNOPQRSTUV"),
    });

    expect(readScript).toContain(
      "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)",
    );
    expect(readScript).toContain("[Text.Encoding]::UTF8.GetString($bytes)");
    expect(readScript).toContain("[Console]::OpenStandardOutput().Write($out, 0, $out.Length)");
  });
});

function envValue(env: NodeJS.ProcessEnv | undefined, name: string): string | undefined {
  return env?.[name];
}

function decodePowerShellScript(args: readonly string[]): string {
  const index = args.indexOf("-EncodedCommand");
  if (index < 0) throw new Error("missing -EncodedCommand");
  const encoded = args[index + 1];
  if (encoded === undefined) throw new Error("missing encoded command");
  return Buffer.from(encoded, "base64").toString("utf16le");
}
