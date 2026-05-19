import { existsSync, readFileSync } from "node:fs";
import { dirname, extname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import * as ts from "typescript";
import { describe, expect, it } from "vitest";
import type { Thread } from "../src/browser.ts";
import * as browserProtocol from "../src/browser.ts";

const CURRENT_DIR = dirname(fileURLToPath(import.meta.url));
const PACKAGE_ROOT = resolve(CURRENT_DIR, "..");
const ENTRYPOINT = resolve(PACKAGE_ROOT, "src/browser.ts");
const FORBIDDEN_BARE_SPECIFIERS = new Set(["buffer", "crypto"]);

interface ImportViolation {
  readonly importer: string;
  readonly specifier: string;
}

type ThreadWireForTest = Readonly<
  Record<string, unknown> & { readonly spec: Readonly<Record<string, unknown>> }
>;

describe("@wuphf/protocol/browser", () => {
  it("has a runtime import graph with no Node built-in imports", () => {
    const graph = walkRuntimeImportGraph(ENTRYPOINT);
    const violations: ImportViolation[] = [];

    for (const [importer, specifiers] of graph) {
      for (const specifier of specifiers) {
        if (specifier.startsWith("node:") || FORBIDDEN_BARE_SPECIFIERS.has(specifier)) {
          violations.push({
            importer: relative(PACKAGE_ROOT, importer),
            specifier,
          });
        }
      }
    }

    expect(violations).toEqual([]);
  });

  it("decodes approval decision tokens without a Node Buffer global", () => {
    const bufferDescriptor = Object.getOwnPropertyDescriptor(globalThis, "Buffer");
    const token = signedApprovalTokenWire();

    try {
      Object.defineProperty(globalThis, "Buffer", {
        configurable: true,
        value: undefined,
        writable: true,
      });

      const parsed = browserProtocol.approvalDecisionRequestFromJson({
        schemaVersion: 1,
        decision: "approve",
        token,
        idempotencyKey: "browser-decision-01",
      });

      expect(parsed.token?.signature.credentialId).toBe("Y3JlZGVudGlhbC0wMQ");
      expect(browserProtocol.approvalDecisionRequestToJsonValue(parsed)).toStrictEqual({
        schemaVersion: 1,
        decision: "approve",
        token,
        idempotencyKey: "browser-decision-01",
      });
    } finally {
      if (bufferDescriptor === undefined) {
        Reflect.deleteProperty(globalThis, "Buffer");
      } else {
        Object.defineProperty(globalThis, "Buffer", bufferDescriptor);
      }
    }
  });

  it("round-trips the browser thread wire surface without deriving hashes", () => {
    const thread = threadFixture();
    const wire = browserProtocol.threadToJsonValue(thread) as ThreadWireForTest;
    const forgedHash = browserProtocol.asSha256Hex("b".repeat(64));

    expect(browserProtocol.threadFromJsonValue(wire)).toStrictEqual(thread);
    expect(browserProtocol.threadToJson(thread)).toBe(browserProtocol.canonicalJSON(wire));
    expect(
      browserProtocol.threadFromJsonValue({
        ...wire,
        spec: {
          ...wire.spec,
          content_hash: forgedHash,
        },
      }).spec.contentHash,
    ).toBe(forgedHash);
  });

  it("keeps browser thread validation structural and bounded", () => {
    const thread = threadFixture();

    expect(browserProtocol.validateThread(thread)).toEqual({ ok: true });
    expect(browserProtocol.validateThread(undefined).ok).toBe(false);
    expect(browserProtocol.validateThread({ ...thread, shadow: true }).ok).toBe(false);
    expect(browserProtocol.validateThread({ ...thread, id: "bad" }).ok).toBe(false);
    expect(browserProtocol.validateThread({ ...thread, title: "" }).ok).toBe(false);
    expect(browserProtocol.validateThread({ ...thread, status: "blocked" }).ok).toBe(false);
    expect(browserProtocol.validateThread({ ...thread, closedAt: DATE }).ok).toBe(false);
    expect(
      browserProtocol.validateThread({
        ...thread,
        spec: { ...thread.spec, threadId: OTHER_THREAD_ID },
      }).ok,
    ).toBe(false);
    expect(
      browserProtocol.validateThread({
        ...thread,
        spec: { ...thread.spec, contentHash: "not-a-hash" },
      }).ok,
    ).toBe(false);
    expect(
      browserProtocol.validateThread({
        ...thread,
        spec: { ...thread.spec, content: { bad: () => undefined } },
      }).ok,
    ).toBe(false);
    expect(
      browserProtocol.validateThread({
        ...thread,
        spec: { ...thread.spec, authoredAt: new Date(Number.NaN) },
      }).ok,
    ).toBe(false);
    expect(browserProtocol.validateThread({ ...thread, taskIds: [TASK_ID, TASK_ID] }).ok).toBe(
      false,
    );
    expect(
      browserProtocol.validateThread({
        ...thread,
        externalRefs: { sourceUrls: ["https://example.test/a"], entityIds: ["x", "x"] },
      }).ok,
    ).toBe(false);
    expect(browserProtocol.validateThreadExternalRefs("not-refs").ok).toBe(false);
  });

  it("rejects malformed browser thread wire records", () => {
    const wire = browserProtocol.threadToJsonValue(threadFixture()) as ThreadWireForTest;

    expect(() => browserProtocol.threadFromJsonValue({ ...wire, status: "blocked" })).toThrow(
      /thread\.status/,
    );
    expect(() =>
      browserProtocol.threadFromJsonValue({ ...wire, updated_at: "not-an-instant" }),
    ).toThrow(/updated_at/);
    expect(() =>
      browserProtocol.threadSpecRevisionFromJsonValue({
        ...wire.spec,
        base_revision_id: "bad",
      }),
    ).toThrow(/base_revision_id/);
    expect(() =>
      browserProtocol.threadExternalRefsFromJsonValue({
        source_urls: [42],
        entity_ids: [],
      }),
    ).toThrow(/source_urls\.0/);
  });
});

const THREAD_ID = browserProtocol.asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const OTHER_THREAD_ID = browserProtocol.asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
const TASK_ID = browserProtocol.asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const REVISION_ID = browserProtocol.asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const SIGNER = browserProtocol.asSignerIdentity("renderer@example.com");
const CONTENT_HASH = browserProtocol.asSha256Hex("a".repeat(64));
const DATE = new Date("2026-05-08T18:00:00.000Z");

function threadFixture(): Thread {
  return {
    id: THREAD_ID,
    title: "Browser thread",
    status: "open",
    spec: {
      revisionId: REVISION_ID,
      threadId: THREAD_ID,
      content: { body: "Render route envelopes" },
      contentHash: CONTENT_HASH,
      authoredBy: SIGNER,
      authoredAt: DATE,
    },
    externalRefs: { sourceUrls: ["https://example.test/thread"], entityIds: ["issue:browser"] },
    taskIds: [TASK_ID],
    createdBy: SIGNER,
    createdAt: DATE,
    updatedAt: DATE,
  };
}

function signedApprovalTokenWire(): Readonly<Record<string, unknown>> {
  return {
    schemaVersion: 1,
    tokenId: "01HX6P2D8T4Y7K9M3N5Q1R6S2V",
    claim: {
      schemaVersion: 1,
      claimId: "claim_credential_01",
      kind: "credential_grant_to_agent",
      granteeAgentId: "agent_alpha",
      credentialHandleId: "cred_ipc0123456789ABCDEFGHIJKLMNOP",
      credentialScope: "openai",
    },
    scope: {
      mode: "single_use",
      claimId: "claim_credential_01",
      claimKind: "credential_grant_to_agent",
      role: "host",
      maxUses: 1,
      granteeAgentId: "agent_alpha",
      credentialHandleId: "cred_ipc0123456789ABCDEFGHIJKLMNOP",
    },
    notBefore: Date.UTC(2026, 4, 8, 18, 0, 0, 0),
    expiresAt: Date.UTC(2026, 4, 8, 18, 30, 0, 0),
    issuedTo: "agent_alpha",
    signature: {
      credentialId: "Y3JlZGVudGlhbC0wMQ",
      authenticatorData: "YXV0aGVudGljYXRvci1kYXRh",
      clientDataJson: "Y2xpZW50LWRhdGEtanNvbg",
      signature: "c2lnbmF0dXJl",
      userHandle: "dXNlci0wMQ",
    },
  };
}

function walkRuntimeImportGraph(entrypoint: string): Map<string, readonly string[]> {
  const graph = new Map<string, readonly string[]>();
  const pending = [entrypoint];
  const visited = new Set<string>();

  while (pending.length > 0) {
    const file = pending.pop();
    if (file === undefined || visited.has(file)) continue;
    visited.add(file);

    const source = readFileSync(file, "utf8");
    const sourceFile = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true);
    const specifiers = runtimeModuleSpecifiers(sourceFile);
    graph.set(file, specifiers);

    for (const specifier of specifiers) {
      if (!specifier.startsWith(".")) continue;
      const resolved = resolveRelativeModule(file, specifier);
      if (resolved !== undefined) pending.push(resolved);
    }
  }

  return graph;
}

function runtimeModuleSpecifiers(sourceFile: ts.SourceFile): readonly string[] {
  const specifiers: string[] = [];

  function visit(node: ts.Node): void {
    if (ts.isImportDeclaration(node)) {
      const specifier = moduleSpecifierText(node.moduleSpecifier);
      if (specifier !== undefined && isRuntimeImport(node)) specifiers.push(specifier);
      return;
    }

    if (ts.isExportDeclaration(node)) {
      const specifier = moduleSpecifierText(node.moduleSpecifier);
      if (specifier !== undefined && isRuntimeExport(node)) specifiers.push(specifier);
      return;
    }

    if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      const [argument] = node.arguments;
      if (argument !== undefined && ts.isStringLiteral(argument)) {
        specifiers.push(argument.text);
      }
    }

    ts.forEachChild(node, visit);
  }

  ts.forEachChild(sourceFile, visit);
  return specifiers;
}

function moduleSpecifierText(moduleSpecifier: ts.Expression | undefined): string | undefined {
  return moduleSpecifier !== undefined && ts.isStringLiteral(moduleSpecifier)
    ? moduleSpecifier.text
    : undefined;
}

function isRuntimeImport(node: ts.ImportDeclaration): boolean {
  const importClause = node.importClause;
  return importClause === undefined || !importClause.isTypeOnly;
}

function isRuntimeExport(node: ts.ExportDeclaration): boolean {
  return !node.isTypeOnly;
}

function resolveRelativeModule(importer: string, specifier: string): string | undefined {
  const resolved = resolve(dirname(importer), specifier);
  if (extname(resolved) !== "") {
    return existsSync(resolved) ? resolved : undefined;
  }

  const tsPath = `${resolved}.ts`;
  if (existsSync(tsPath)) return tsPath;

  const jsonPath = `${resolved}.json`;
  return existsSync(jsonPath) ? jsonPath : undefined;
}
