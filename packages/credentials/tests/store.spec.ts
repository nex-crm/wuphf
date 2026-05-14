import path from "node:path";
import { inspect } from "node:util";

import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { LinuxCredentialStore } from "../src/adapters/linux.ts";
import { MacOSCredentialStore } from "../src/adapters/macos.ts";
import { WindowsCredentialStore } from "../src/adapters/windows.ts";
import {
  AdapterNotSupported,
  KeychainCommandFailed,
  KeychainCommandTimedOut,
  NotFound,
} from "../src/errors.ts";
import { credentialHandleFromParts, open, type Spawner } from "../src/index.ts";

const serviceName = "wuphf.credentials.test";
const agentA = asAgentId("agent_alpha");
const agentB = asAgentId("agent_beta");
const scope = asCredentialScope("openai");
const fixtureSecret = "fixture-basic_text-こんにちは-secret\n";

describe("CredentialStore contract", () => {
  const cases = [
    {
      name: "macOS security",
      makeHarness: () => {
        const fake = new SecurityFake();
        return {
          fake,
          store: new MacOSCredentialStore({
            enforceTrustedCommand: false,
            serviceName,
            spawner: fake.spawner,
          }),
        };
      },
    },
    {
      name: "Linux secret-tool",
      makeHarness: () => {
        const fake = new SecretToolFake();
        return {
          fake,
          store: new LinuxCredentialStore({
            enforceTrustedCommand: false,
            serviceName,
            spawner: fake.spawner,
          }),
        };
      },
    },
    {
      name: "Windows Credential Manager",
      makeHarness: () => {
        const fake = new PowerShellCredentialFake();
        return {
          fake,
          store: new WindowsCredentialStore({
            enforceTrustedCommand: false,
            serviceName,
            spawner: fake.spawner,
          }),
        };
      },
    },
  ] as const;

  for (const testCase of cases) {
    it(`${testCase.name} keeps handles opaque and reads via the adapter each time`, async () => {
      const { fake, store } = testCase.makeHarness();
      const handle = await store.write({ agentId: agentA, scope, secret: fixtureSecret });

      expect(JSON.stringify(handle)).not.toContain(fixtureSecret);
      expect(String(handle)).not.toContain(fixtureSecret);
      expect(inspect(handle)).not.toContain(fixtureSecret);

      const readCountAfterWrite = fake.readCount;
      await expect(store.read(handle)).resolves.toBe(fixtureSecret);
      await expect(store.read(handle)).resolves.toBe(fixtureSecret);
      expect(fake.readCount - readCountAfterWrite).toBe(2);
    });

    it(`${testCase.name} scopes credentials by agent id`, async () => {
      const { store } = testCase.makeHarness();
      const handle = await store.write({ agentId: agentA, scope, secret: fixtureSecret });
      const otherAgentHandle = credentialHandleFromParts({
        id: handle.id,
        agentId: agentB,
        scope: handle.scope,
      });

      await expect(store.read(otherAgentHandle)).rejects.toBeInstanceOf(NotFound);
    });

    it(`${testCase.name} rejects NUL secrets before invoking the keychain`, async () => {
      const { store } = testCase.makeHarness();

      await expect(
        store.write({ agentId: agentA, scope, secret: "bad\u0000secret" }),
      ).rejects.toMatchObject({ code: "invalid_credential_payload" });
    });
  }

  it("selects a platform adapter without creating a singleton", () => {
    const fake: Spawner = async () => ({ stdout: "", stderr: "", code: 0 });

    expect(open({ platform: "darwin", serviceName, spawner: fake })).toBeInstanceOf(
      MacOSCredentialStore,
    );
    expect(open({ platform: "linux", serviceName, spawner: fake })).toBeInstanceOf(
      LinuxCredentialStore,
    );
    expect(open({ platform: "win32", serviceName, spawner: fake })).toBeInstanceOf(
      WindowsCredentialStore,
    );
    expect(() =>
      open({ platform: "freebsd" as NodeJS.Platform, serviceName, spawner: fake }),
    ).toThrow(AdapterNotSupported);
  });

  it("passes absolute commands with a sanitized locale-stable environment", async () => {
    const calls: Array<{
      readonly cmd: string;
      readonly env: NodeJS.ProcessEnv | undefined;
    }> = [];
    const fake = new SecretToolFake();
    const spawner: Spawner = async (cmd, args, options) => {
      calls.push({ cmd, env: options?.env });
      return fake.spawner(cmd, args, options);
    };
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName,
      spawner,
    });

    await store.write({ agentId: agentA, scope, secret: fixtureSecret });

    expect(calls.length).toBeGreaterThan(0);
    for (const call of calls) {
      expect(path.posix.isAbsolute(call.cmd)).toBe(true);
      expect(envValue(call.env, "LC_ALL")).toBe("C");
      expect(envValue(call.env, "PATH")).toBe(path.posix.dirname(call.cmd));
      expect(Object.keys(call.env ?? {}).sort()).toEqual(
        expect.arrayContaining(["LC_ALL", "PATH"]),
      );
      expect(
        Object.keys(call.env ?? {}).every((key) =>
          ["HOME", "LC_ALL", "PATH", "USER"].includes(key),
        ),
      ).toBe(true);
    }
  });

  it("times out a hung subprocess with a typed error", async () => {
    const spawner: Spawner = async () => new Promise<never>(() => {});
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName,
      spawner,
      timeoutMs: 20,
    });
    const started = Date.now();

    await expect(
      store.write({ agentId: agentA, scope, secret: fixtureSecret }),
    ).rejects.toBeInstanceOf(KeychainCommandTimedOut);
    expect(Date.now() - started).toBeLessThan(1_000);
  });

  it("maps ENOENT spawn failures to NoKeyringAvailable with a recovery hint", async () => {
    const spawner: Spawner = async () => {
      const error = new Error("spawn ENOENT") as NodeJS.ErrnoException;
      error.code = "ENOENT";
      throw error;
    };
    const store = new LinuxCredentialStore({
      enforceTrustedCommand: false,
      serviceName,
      spawner,
    });

    await expect(
      store.write({ agentId: agentA, scope, secret: fixtureSecret }),
    ).rejects.toMatchObject({
      code: "no_keyring_available",
      recoveryHint: "Install libsecret-tools: sudo apt install libsecret-tools",
    });
  });

  it("sanitizes terminal control bytes from keychain command errors", () => {
    const error = new KeychainCommandFailed(
      "secret-tool lookup",
      1,
      "\u001b[31mpermission\u001b[0m\n\u0085denied\u0000",
    );

    expect(error.message).toContain("permission denied");
    expect(error.message).not.toContain("\u001b");
    expect(error.message).not.toContain("\u0085");
    expect(error.message).not.toContain("\u0000");
  });
});

interface FakeHarness {
  readonly spawner: Spawner;
  readonly readCount: number;
}

class SecurityFake implements FakeHarness {
  readonly secrets = new Map<string, string>();
  readCount = 0;

  spawner: Spawner = async (cmd, args, options) => {
    expect(cmd).toBe("/usr/bin/security");
    expect(envValue(options?.env, "LC_ALL")).toBe("C");
    const action = args[0];
    const account = argAfter(args, "-a");
    if (action === "add-generic-password") {
      this.secrets.set(account, options?.input ?? "");
      return ok();
    }
    if (action === "find-generic-password") {
      this.readCount++;
      const secret = this.secrets.get(account);
      return secret === undefined
        ? { stdout: "", stderr: "The specified item could not be found", code: 44 }
        : { stdout: `${secret}\n`, stderr: "", code: 0 };
    }
    if (action === "delete-generic-password") {
      this.secrets.delete(account);
      return ok();
    }
    return { stdout: "", stderr: `unexpected security action ${String(action)}`, code: 1 };
  };
}

class SecretToolFake implements FakeHarness {
  readonly secrets = new Map<string, string>();
  readCount = 0;

  spawner: Spawner = async (cmd, args, options) => {
    expect(cmd === "/usr/bin/secret-tool" || cmd === "/usr/bin/gdbus").toBe(true);
    expect(envValue(options?.env, "LC_ALL")).toBe("C");
    if (cmd === "/usr/bin/gdbus") return { stdout: "@a{sv} {}", stderr: "", code: 0 };
    const action = args[0];
    if (action === "--version") return { stdout: "secret-tool 0.21.4\n", stderr: "", code: 0 };
    if (action === "search") return { stdout: "collection: encrypted\n", stderr: "", code: 0 };
    if (action === "store") {
      this.secrets.set(secretToolAccount(args), options?.input ?? "");
      return ok();
    }
    if (action === "lookup") {
      this.readCount++;
      const secret = this.secrets.get(secretToolAccount(args));
      return secret === undefined
        ? { stdout: "", stderr: "No such secret", code: 1 }
        : { stdout: `${secret}\n`, stderr: "", code: 0 };
    }
    if (action === "clear") {
      this.secrets.delete(secretToolAccount(args));
      return ok();
    }
    return { stdout: "", stderr: `unexpected secret-tool action ${String(action)}`, code: 1 };
  };
}

class PowerShellCredentialFake implements FakeHarness {
  readonly secrets = new Map<string, string>();
  readCount = 0;

  spawner: Spawner = async (cmd, args, options) => {
    expect(path.win32.isAbsolute(cmd)).toBe(true);
    expect(cmd.toLowerCase()).toMatch(/\\powershell\.exe$/);
    expect(envValue(options?.env, "LC_ALL")).toBe("C");
    const script = decodePowerShellScript(args);
    const target = powerShellTarget(script);
    if (script.includes("[Console]::In.ReadToEnd()")) {
      this.secrets.set(target, options?.input ?? "");
      return ok();
    }
    if (script.includes("CredRead($target")) {
      this.readCount++;
      const secret = this.secrets.get(target);
      return secret === undefined
        ? { stdout: "", stderr: "CredRead failed: 1168", code: 2 }
        : { stdout: secret, stderr: "", code: 0 };
    }
    if (script.includes("CredDelete($target")) {
      this.secrets.delete(target);
      return ok();
    }
    return { stdout: "", stderr: "unexpected PowerShell script", code: 1 };
  };
}

function ok() {
  return { stdout: "", stderr: "", code: 0 };
}

function argAfter(args: readonly string[], flag: string): string {
  const index = args.indexOf(flag);
  if (index < 0) throw new Error(`missing ${flag}`);
  const value = args[index + 1];
  if (value === undefined) throw new Error(`missing value for ${flag}`);
  return value;
}

function secretToolAccount(args: readonly string[]): string {
  const index = args.indexOf("wuphf_account");
  if (index < 0) throw new Error("missing wuphf_account");
  const value = args[index + 1];
  if (value === undefined) throw new Error("missing wuphf_account value");
  return value;
}

function decodePowerShellScript(args: readonly string[]): string {
  const encoded = argAfter(args, "-EncodedCommand");
  return Buffer.from(encoded, "base64").toString("utf16le");
}

function powerShellTarget(script: string): string {
  const match = /\$target = '([^']+)'/.exec(script);
  if (match === null) throw new Error("missing PowerShell target");
  return match[1] ?? "";
}

function envValue(env: NodeJS.ProcessEnv | undefined, name: string): string | undefined {
  return env?.[name];
}
