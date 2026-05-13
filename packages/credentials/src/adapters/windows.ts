import type { CredentialHandle } from "@wuphf/protocol";

import { KeychainCommandFailed, NotFound } from "../errors.ts";
import { type CredentialHandleParts, credentialAccount, newCredentialHandle } from "../handle.ts";
import type { CredentialStore, CredentialWriteRequest, Spawner } from "../store.ts";

export interface WindowsCredentialStoreOptions {
  readonly serviceName: string;
  readonly spawner: Spawner;
  readonly timeoutMs?: number | undefined;
}

export class WindowsCredentialStore implements CredentialStore {
  constructor(private readonly options: WindowsCredentialStoreOptions) {}

  async write(input: CredentialWriteRequest): Promise<CredentialHandle> {
    const handle = newCredentialHandle(input);
    const target = credentialTarget(this.options.serviceName, handleParts(handle));
    const result = await this.options.spawner(
      "powershell.exe",
      powerShellArgs(writeScript(target)),
      {
        input: input.secret,
        timeoutMs: this.options.timeoutMs,
      },
    );

    if (result.code !== 0) {
      throw new KeychainCommandFailed("powershell CredWrite", result.code, result.stderr);
    }
    return handle;
  }

  async read(handle: CredentialHandle): Promise<string> {
    const target = credentialTarget(this.options.serviceName, handleParts(handle));
    const result = await this.options.spawner(
      "powershell.exe",
      powerShellArgs(readScript(target)),
      {
        timeoutMs: this.options.timeoutMs,
      },
    );

    if (result.code !== 0) {
      if (isWindowsNotFound(result.stderr)) throw new NotFound();
      throw new KeychainCommandFailed("powershell CredRead", result.code, result.stderr);
    }
    if (result.stdout.length === 0) throw new NotFound();
    return result.stdout;
  }

  async delete(handle: CredentialHandle): Promise<void> {
    const target = credentialTarget(this.options.serviceName, handleParts(handle));
    const result = await this.options.spawner(
      "powershell.exe",
      powerShellArgs(deleteScript(target)),
      {
        timeoutMs: this.options.timeoutMs,
      },
    );

    if (result.code !== 0 && !isWindowsNotFound(result.stderr)) {
      throw new KeychainCommandFailed("powershell CredDelete", result.code, result.stderr);
    }
  }
}

function credentialTarget(serviceName: string, parts: CredentialHandleParts): string {
  return `${serviceName}:${credentialAccount(parts)}`;
}

function handleParts(handle: CredentialHandle): CredentialHandleParts {
  return { id: handle.id, agentId: handle.agentId, scope: handle.scope };
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

function writeScript(target: string): string {
  return `${credentialManagerPrelude()}
$target = ${psQuote(target)}
$secret = [Console]::In.ReadToEnd()
$bytes = [Text.Encoding]::Unicode.GetBytes($secret)
$blob = [Runtime.InteropServices.Marshal]::StringToCoTaskMemUni($secret)
try {
  $credential = New-Object CredMan+CREDENTIAL
  $credential.Type = [CredMan]::CRED_TYPE_GENERIC
  $credential.TargetName = $target
  $credential.CredentialBlobSize = $bytes.Length
  $credential.CredentialBlob = $blob
  $credential.Persist = [CredMan]::CRED_PERSIST_LOCAL_MACHINE
  $credential.UserName = "wuphf"
  if (-not [CredMan]::CredWrite([ref] $credential, 0)) {
    $code = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
    throw "CredWrite failed: $code"
  }
} finally {
  [Runtime.InteropServices.Marshal]::ZeroFreeCoTaskMemUnicode($blob)
}`;
}

function readScript(target: string): string {
  return `${credentialManagerPrelude()}
$target = ${psQuote(target)}
$ptr = [IntPtr]::Zero
if (-not [CredMan]::CredRead($target, [CredMan]::CRED_TYPE_GENERIC, 0, [ref] $ptr)) {
  $code = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
  [Console]::Error.Write("CredRead failed: $code")
  exit 2
}
try {
  $credential = [Runtime.InteropServices.Marshal]::PtrToStructure($ptr, [type] [CredMan+CREDENTIAL])
  $secret = [Runtime.InteropServices.Marshal]::PtrToStringUni(
    $credential.CredentialBlob,
    [int] ($credential.CredentialBlobSize / 2)
  )
  [Console]::Out.Write($secret)
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

// TODO(branch-8-followup): RFC v4's Architectural change moves credentials
// from Electron safeStorage into per-agent OS keychain entries. This v1 adapter
// stores per-agent targets in Windows Credential Manager through PowerShell, but
// it does not yet add AppContainer-specific ACL hardening for packaged desktop
// utility processes. Track that as a branch-9/desktop integration follow-up so
// the runner identity model, package SID, and broker-spawn shape are known
// before tightening access control.
