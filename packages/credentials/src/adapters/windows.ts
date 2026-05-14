import type { CredentialHandle } from "@wuphf/protocol";

import { NotFound } from "../errors.ts";
import {
  type CredentialHandleParts,
  credentialAccount,
  credentialHandleParts,
  newCredentialHandle,
} from "../internal/handle.ts";
import {
  assertBrokerIdentityForAgent,
  assertCredentialOwnership,
  assertValidCredentialPayload,
  type CredentialDeleteRequest,
  type CredentialReadRequest,
  type CredentialReadWithOwnershipRequest,
  type CredentialReadWithOwnershipResult,
  type CredentialStore,
  type CredentialWriteRequest,
  keychainCommandFailure,
  operationTimeoutMs,
  parseCredentialOwnershipMetadata,
  resolveTrustedCommand,
  runKeychainCommand,
  type Spawner,
  type TrustedCommand,
} from "../store.ts";

export interface WindowsCredentialStoreOptions {
  readonly serviceName: string;
  readonly spawner: Spawner;
  readonly enforceTrustedCommand?: boolean | undefined;
  readonly timeoutMs?: number | undefined;
}

export class WindowsCredentialStore implements CredentialStore {
  private readonly powershell: TrustedCommand;

  constructor(private readonly options: WindowsCredentialStoreOptions) {
    this.powershell = resolveTrustedCommand({
      candidates: [windowsPowerShellPath()],
      commandName: "powershell.exe",
      enforce: options.enforceTrustedCommand ?? true,
      platform: "win32",
      recoveryHint:
        "Ensure Windows PowerShell exists under %SystemRoot%\\System32\\WindowsPowerShell\\v1.0",
      requireWindowsAdministratorsOwner: true,
    });
  }

  async write(input: CredentialWriteRequest): Promise<CredentialHandle> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    assertValidCredentialPayload(input.secret);
    const handle = newCredentialHandle(input);
    const parts = credentialHandleParts(handle, input);
    const target = credentialTarget(this.options.serviceName, { handleId: parts.id });
    const result = await runKeychainCommand(
      this.options.spawner,
      this.powershell,
      powerShellArgs(writeScript(target, credentialComment(parts))),
      {
        action: "write",
        commandName: "powershell CredWrite",
        input: input.secret,
        platform: "win32",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0) {
      throw keychainCommandFailure("powershell CredWrite", result, {
        action: "write",
        platform: "win32",
      });
    }
    return handle;
  }

  async read(input: CredentialReadRequest): Promise<string> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    const target = credentialTarget(this.options.serviceName, { handleId: input.handleId });
    const result = await runKeychainCommand(
      this.options.spawner,
      this.powershell,
      powerShellArgs(readScript(target)),
      {
        action: "read",
        commandName: "powershell CredRead",
        platform: "win32",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0) {
      if (isWindowsNotFound(result.stderr)) throw new NotFound();
      throw keychainCommandFailure("powershell CredRead", result, {
        action: "read",
        platform: "win32",
      });
    }
    if (result.stdout.length === 0) throw new NotFound();
    return result.stdout;
  }

  async readWithOwnership(
    input: CredentialReadWithOwnershipRequest,
  ): Promise<CredentialReadWithOwnershipResult> {
    assertBrokerIdentityForAgent(input.broker, input.expectedAgentId);
    const target = credentialTarget(this.options.serviceName, { handleId: input.handleId });
    const result = await runKeychainCommand(
      this.options.spawner,
      this.powershell,
      powerShellArgs(readWithMetadataScript(target)),
      {
        action: "read-with-ownership",
        commandName: "powershell CredRead",
        platform: "win32",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0) {
      if (isWindowsNotFound(result.stderr)) throw new NotFound();
      throw keychainCommandFailure("powershell CredRead", result, {
        action: "read-with-ownership",
        platform: "win32",
      });
    }
    const payload = parseWindowsCredentialReadPayload(result.stdout);
    const ownership = parseCredentialOwnershipMetadata(payload.comment);
    assertCredentialOwnership(ownership, {
      agentId: input.expectedAgentId,
      scope: input.expectedScope,
    });
    return { secret: payload.secret, ...ownership };
  }

  async delete(input: CredentialDeleteRequest): Promise<void> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    const target = credentialTarget(this.options.serviceName, { handleId: input.handleId });
    const result = await runKeychainCommand(
      this.options.spawner,
      this.powershell,
      powerShellArgs(deleteScript(target)),
      {
        action: "delete",
        commandName: "powershell CredDelete",
        platform: "win32",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0 && !isWindowsNotFound(result.stderr)) {
      throw keychainCommandFailure("powershell CredDelete", result, {
        action: "delete",
        platform: "win32",
      });
    }
  }
}

function windowsPowerShellPath(): string {
  const systemRoot = processEnvValue("SystemRoot") ?? "C:\\Windows";
  return `${systemRoot}\\System32\\WindowsPowerShell\\v1.0\\powershell.exe`;
}

function processEnvValue(name: string): string | undefined {
  return process.env[name];
}

function credentialTarget(
  serviceName: string,
  parts: { readonly handleId: CredentialHandleParts["id"] },
): string {
  return `${serviceName}:${credentialAccount(parts)}`;
}

function credentialComment(parts: CredentialHandleParts): string {
  return JSON.stringify({ agentId: parts.agentId, scope: parts.scope });
}

function powerShellArgs(script: string): string[] {
  return [
    "-NoProfile",
    "-NonInteractive",
    "-ExecutionPolicy",
    "Bypass",
    "-EncodedCommand",
    Buffer.from(script, "utf16le").toString("base64"),
  ];
}

function writeScript(target: string, comment: string): string {
  return `${credentialManagerPrelude()}
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
$target = ${psQuote(target)}
$comment = ${psQuote(comment)}
$bytes = [Text.Encoding]::UTF8.GetBytes([Console]::In.ReadToEnd())
$blob = [IntPtr]::Zero
if ($bytes.Length -gt 0) {
  $blob = [Runtime.InteropServices.Marshal]::AllocHGlobal($bytes.Length)
  [Runtime.InteropServices.Marshal]::Copy($bytes, 0, $blob, $bytes.Length)
}
try {
  $credential = New-Object CredMan+CREDENTIAL
  $credential.Type = [CredMan]::CRED_TYPE_GENERIC
  $credential.TargetName = $target
  $credential.CredentialBlobSize = $bytes.Length
  $credential.CredentialBlob = $blob
  $credential.Persist = [CredMan]::CRED_PERSIST_LOCAL_MACHINE
  $credential.UserName = "wuphf"
  $credential.Comment = $comment
  if (-not [CredMan]::CredWrite([ref] $credential, 0)) {
    $code = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
    throw "CredWrite failed: $code"
  }
} finally {
  if ($blob -ne [IntPtr]::Zero) {
    $zero = New-Object byte[] $bytes.Length
    [Runtime.InteropServices.Marshal]::Copy($zero, 0, $blob, $bytes.Length)
    [Runtime.InteropServices.Marshal]::FreeHGlobal($blob)
  }
}`;
}

function readScript(target: string): string {
  return `${credentialManagerPrelude()}
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$target = ${psQuote(target)}
$ptr = [IntPtr]::Zero
if (-not [CredMan]::CredRead($target, [CredMan]::CRED_TYPE_GENERIC, 0, [ref] $ptr)) {
  $code = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
  [Console]::Error.Write("CredRead failed: $code")
  exit 2
}
try {
  $credential = [Runtime.InteropServices.Marshal]::PtrToStructure($ptr, [type] [CredMan+CREDENTIAL])
  $bytes = New-Object byte[] $credential.CredentialBlobSize
  if ($bytes.Length -gt 0) {
    [Runtime.InteropServices.Marshal]::Copy($credential.CredentialBlob, $bytes, 0, $bytes.Length)
  }
  $secret = [Text.Encoding]::UTF8.GetString($bytes)
  $out = [Text.Encoding]::UTF8.GetBytes($secret)
  [Console]::OpenStandardOutput().Write($out, 0, $out.Length)
} finally {
  [CredMan]::CredFree($ptr)
}`;
}

function readWithMetadataScript(target: string): string {
  return `${credentialManagerPrelude()}
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$target = ${psQuote(target)}
$ptr = [IntPtr]::Zero
if (-not [CredMan]::CredRead($target, [CredMan]::CRED_TYPE_GENERIC, 0, [ref] $ptr)) {
  $code = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
  [Console]::Error.Write("CredRead failed: $code")
  exit 2
}
try {
  $credential = [Runtime.InteropServices.Marshal]::PtrToStructure($ptr, [type] [CredMan+CREDENTIAL])
  $bytes = New-Object byte[] $credential.CredentialBlobSize
  if ($bytes.Length -gt 0) {
    [Runtime.InteropServices.Marshal]::Copy($credential.CredentialBlob, $bytes, 0, $bytes.Length)
  }
  $payload = @{
    secretB64 = [Convert]::ToBase64String($bytes)
    comment = $credential.Comment
  } | ConvertTo-Json -Compress
  $out = [Text.Encoding]::UTF8.GetBytes($payload)
  [Console]::OpenStandardOutput().Write($out, 0, $out.Length)
} finally {
  [CredMan]::CredFree($ptr)
}`;
}

function deleteScript(target: string): string {
  return `${credentialManagerPrelude()}
$target = ${psQuote(target)}
if (-not [CredMan]::CredDelete($target, [CredMan]::CRED_TYPE_GENERIC, 0)) {
  $code = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
  [Console]::Error.Write("CredDelete failed: $code")
  exit 2
}`;
}

function credentialManagerPrelude(): string {
  return `$ErrorActionPreference = "Stop"
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public static class CredMan {
  public const UInt32 CRED_TYPE_GENERIC = 1;
  public const UInt32 CRED_PERSIST_LOCAL_MACHINE = 2;

  [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
  public struct CREDENTIAL {
    public UInt32 Flags;
    public UInt32 Type;
    public string TargetName;
    public string Comment;
    public System.Runtime.InteropServices.ComTypes.FILETIME LastWritten;
    public UInt32 CredentialBlobSize;
    public IntPtr CredentialBlob;
    public UInt32 Persist;
    public UInt32 AttributeCount;
    public IntPtr Attributes;
    public string TargetAlias;
    public string UserName;
  }

  [DllImport("advapi32.dll", EntryPoint = "CredWriteW", CharSet = CharSet.Unicode, SetLastError = true)]
  public static extern bool CredWrite(ref CREDENTIAL userCredential, UInt32 flags);

  [DllImport("advapi32.dll", EntryPoint = "CredReadW", CharSet = CharSet.Unicode, SetLastError = true)]
  public static extern bool CredRead(string target, UInt32 type, UInt32 reservedFlag, out IntPtr credentialPtr);

  [DllImport("advapi32.dll", EntryPoint = "CredDeleteW", CharSet = CharSet.Unicode, SetLastError = true)]
  public static extern bool CredDelete(string target, UInt32 type, UInt32 flags);

  [DllImport("advapi32.dll", SetLastError = false)]
  public static extern void CredFree(IntPtr buffer);
}
"@
`;
}

function psQuote(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

function isWindowsNotFound(stderr: string): boolean {
  return /Cred(Read|Delete) failed: 1168|not found|element not found/i.test(stderr);
}

function parseWindowsCredentialReadPayload(stdout: string): {
  readonly secret: string;
  readonly comment: string;
} {
  let parsed: unknown;
  try {
    parsed = JSON.parse(stdout);
  } catch {
    throw new NotFound();
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new NotFound();
  }
  const record = parsed as Readonly<{
    comment?: unknown;
    secretB64?: unknown;
  }>;
  if (typeof record.secretB64 !== "string" || typeof record.comment !== "string") {
    throw new NotFound();
  }
  return {
    secret: Buffer.from(record.secretB64, "base64").toString("utf8"),
    comment: record.comment,
  };
}

// TODO(#847): RFC v4's Architectural change moves credentials
// from Electron safeStorage into per-agent OS keychain entries. This v1 adapter
// stores per-agent targets in Windows Credential Manager through PowerShell, but
// it does not yet add AppContainer-specific ACL hardening for packaged desktop
// utility processes. Track that as a branch-9/desktop integration follow-up so
// the runner identity model, package SID, and broker-spawn shape are known
// before tightening access control.
