import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { FrozenArgs } from "../src/frozen-args.ts";
import {
  asAgentSlug,
  asApprovalId,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asToolCallId,
  isReceiptSnapshot,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  validateReceipt,
} from "../src/receipt.ts";
import { SanitizedString } from "../src/sanitized-string.ts";
import { sha256Hex } from "../src/sha256.ts";

const RECEIPT_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
const TASK_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW";

const REQUIRED_TOP_LEVEL_FIELDS = [
  "id",
  "agentSlug",
  "taskId",
  "triggerKind",
  "triggerRef",
  "startedAt",
  "status",
  "providerKind",
  "model",
  "promptHash",
  "toolManifest",
  "toolCalls",
  "approvals",
  "filesChanged",
  "commits",
  "sourceReads",
  "writes",
  "inputTokens",
  "outputTokens",
  "cacheReadTokens",
  "cacheCreationTokens",
  "costUsd",
  "notebookWrites",
  "wikiWrites",
  "schemaVersion",
] as const satisfies readonly (keyof ReceiptSnapshot)[];

describe("receipt schema", () => {
  it("round-trips a valid receipt through canonical JSON", () => {
    const receipt = validReceiptFixture();

    const roundTripped = receiptFromJson(receiptToJson(receipt));

    expect(roundTripped).toEqual(receipt);
    expect(receiptToJson(roundTripped)).toBe(receiptToJson(receipt));
  });

  it("serializes byte-identical canonical JSON for shuffled field insertion order", () => {
    expect(receiptToJson(validReceiptFixture())).toBe(receiptToJson(shuffledReceiptFixture()));
  });

  it("rejects missing top-level required fields", () => {
    for (const field of REQUIRED_TOP_LEVEL_FIELDS) {
      const missing: Record<string, unknown> = { ...validReceiptFixture() };
      delete missing[field];

      const result = validateReceipt(missing);

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.errors.some((error) => error.path === `/${field}`)).toBe(true);
      }
    }
  });

  it("rejects wrong-type and invalid literal fields", () => {
    const wrongTokens: Record<string, unknown> = {
      ...validReceiptFixture(),
      inputTokens: "not-a-number",
    };
    expect(validateReceipt(wrongTokens)).toEqual({
      ok: false,
      errors: [{ path: "/inputTokens", message: "must be a non-negative integer" }],
    });

    const wrongStatus: Record<string, unknown> = {
      ...validReceiptFixture(),
      status: "done",
    };
    const statusResult = validateReceipt(wrongStatus);
    expect(statusResult.ok).toBe(false);
    if (!statusResult.ok) {
      expect(statusResult.errors).toContainEqual({
        path: "/status",
        message: "must be a valid receipt status",
      });
    }

    const wrongId: Record<string, unknown> = {
      ...validReceiptFixture(),
      id: "not-a-ulid",
    };
    const idResult = validateReceipt(wrongId);
    expect(idResult.ok).toBe(false);
    if (!idResult.ok) {
      expect(idResult.errors).toContainEqual({
        path: "/id",
        message: "must be an uppercase ULID ReceiptId",
      });
    }
  });

  it("never throws for unknown fuzz payloads", () => {
    fc.assert(
      fc.property(fuzzReceiptPayload(), (payload) => {
        const result = validateReceipt(payload);
        if (result.ok) {
          expect(isReceiptSnapshot(payload)).toBe(true);
          return;
        }
        expect(result.errors.length).toBeGreaterThan(0);
      }),
      { numRuns: 1000 },
    );
  });

  it("brands uppercase ULID receipt ids", () => {
    expect(() => asReceiptId("not-a-ulid")).toThrow();
    expect(asReceiptId(RECEIPT_ID)).toBe(RECEIPT_ID);
  });
});

function validReceiptFixture(): ReceiptSnapshot {
  const receiptId = asReceiptId(RECEIPT_ID);
  const taskId = asTaskId(TASK_ID);
  const toolInputs = FrozenArgs.freeze({ action: "summarize", entityId: "contact:1234" });
  const proposedDiff = FrozenArgs.freeze({
    after: { amount: 1500, stage: "qualified" },
    before: { amount: 1000, stage: "lead" },
  });
  const appliedDiff = FrozenArgs.freeze({
    amount: { from: 1000, to: 1500 },
    stage: { from: "lead", to: "qualified" },
  });
  const postWriteVerify = FrozenArgs.freeze({ amount: 1500, stage: "qualified" });
  const approvalToken = {
    signerIdentity: "fran@example.com",
    role: "approver" as const,
    receiptId,
    frozenArgsHash: proposedDiff.hash,
    riskClass: "high" as const,
    expiresAt: new Date("2026-05-08T18:30:00.000Z"),
    webauthnAssertion: "webauthn-assertion",
    brokerVerificationStatus: "valid" as const,
  };

  return {
    id: receiptId,
    agentSlug: asAgentSlug("sam_agent"),
    taskId,
    triggerKind: "human_message",
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    startedAt: new Date("2026-05-08T18:00:00.000Z"),
    finishedAt: new Date("2026-05-08T18:05:00.000Z"),
    status: "ok",
    providerKind: asProviderKind("openai"),
    model: "gpt-5.2",
    promptHash: sha256Hex("prompt:v1"),
    toolManifest: sha256Hex("tool-manifest:v1"),
    toolCalls: [
      {
        tool_id: asToolCallId("tool_01"),
        tool_name: "hubspot.contacts.read",
        inputs: toolInputs,
        output: SanitizedString.fromUnknown("Fetched HubSpot contact #1234"),
        started_at: new Date("2026-05-08T18:00:01.000Z"),
        finished_at: new Date("2026-05-08T18:00:02.000Z"),
        status: "ok",
        error: SanitizedString.fromUnknown(""),
      },
    ],
    approvals: [
      {
        approval_id: asApprovalId("approval_01"),
        role: "approver",
        decision: "approve",
        signed_token: approvalToken,
        decidedAt: new Date("2026-05-08T18:01:00.000Z"),
      },
    ],
    filesChanged: [
      {
        path: "docs/brief.md",
        mode: "modified",
        before_hash: sha256Hex("docs/brief.md:before"),
        after_hash: sha256Hex("docs/brief.md:after"),
        lines_added: 12,
        lines_removed: 3,
      },
    ],
    commits: [
      {
        sha: "abc123def456",
        message: SanitizedString.fromUnknown("docs: update meeting brief"),
        author: "Fran",
        author_email: "fran@example.com",
        parent_sha: "def456abc123",
        signed: true,
      },
    ],
    sourceReads: [
      {
        provider: "hubspot",
        entityType: "contact",
        entityId: "1234",
        fetchedAt: new Date("2026-05-08T18:00:01.000Z"),
        hash: sha256Hex("hubspot-contact-1234"),
        citation: "HubSpot contact #1234 fetched at 2026-05-08T18:00:01.000Z",
        raw_ref: "hubspot://contacts/1234",
      },
    ],
    writes: [
      {
        action: "hubspot.deals.update",
        target: "deal:5678",
        idempotencyKey: "receipt-01ARZ3NDEKTSV4RRFFQ69G5FAV-write-1",
        proposedDiff,
        appliedDiff,
        approvalToken,
        approvedAt: new Date("2026-05-08T18:01:01.000Z"),
        result: "applied",
        postWriteVerify,
      },
    ],
    inputTokens: 1200,
    outputTokens: 345,
    cacheReadTokens: 50,
    cacheCreationTokens: 25,
    costUsd: 0.0425,
    finalMessage: SanitizedString.fromUnknown("Done. Updated the deal and wrote the brief."),
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [
      {
        store: "notebook",
        slug: "meeting-briefs",
        hash: sha256Hex("notebook-write"),
        citation: "Notebook meeting-briefs updated",
      },
    ],
    wikiWrites: [
      {
        store: "wiki",
        slug: "accounts/acme",
        hash: sha256Hex("wiki-write"),
        citation: "Wiki account page updated",
      },
    ],
    worktreePath: "/Users/fd/src/nex/wuphf/.worktrees/task-01",
    gitHeadStart: "abc123def456",
    gitHeadEnd: "fed654cba321",
    schemaVersion: 1,
  };
}

function shuffledReceiptFixture(): ReceiptSnapshot {
  const receiptId = asReceiptId(RECEIPT_ID);
  const taskId = asTaskId(TASK_ID);
  const toolInputs = FrozenArgs.freeze({ entityId: "contact:1234", action: "summarize" });
  const proposedDiff = FrozenArgs.freeze({
    before: { stage: "lead", amount: 1000 },
    after: { stage: "qualified", amount: 1500 },
  });
  const appliedDiff = FrozenArgs.freeze({
    stage: { to: "qualified", from: "lead" },
    amount: { to: 1500, from: 1000 },
  });
  const postWriteVerify = FrozenArgs.freeze({ stage: "qualified", amount: 1500 });
  const approvalToken = {
    brokerVerificationStatus: "valid" as const,
    webauthnAssertion: "webauthn-assertion",
    expiresAt: new Date("2026-05-08T18:30:00.000Z"),
    riskClass: "high" as const,
    frozenArgsHash: proposedDiff.hash,
    receiptId,
    role: "approver" as const,
    signerIdentity: "fran@example.com",
  };

  return {
    schemaVersion: 1,
    gitHeadEnd: "fed654cba321",
    gitHeadStart: "abc123def456",
    worktreePath: "/Users/fd/src/nex/wuphf/.worktrees/task-01",
    wikiWrites: [
      {
        citation: "Wiki account page updated",
        hash: sha256Hex("wiki-write"),
        slug: "accounts/acme",
        store: "wiki",
      },
    ],
    notebookWrites: [
      {
        citation: "Notebook meeting-briefs updated",
        hash: sha256Hex("notebook-write"),
        slug: "meeting-briefs",
        store: "notebook",
      },
    ],
    error: SanitizedString.fromUnknown(""),
    finalMessage: SanitizedString.fromUnknown("Done. Updated the deal and wrote the brief."),
    costUsd: 0.0425,
    cacheCreationTokens: 25,
    cacheReadTokens: 50,
    outputTokens: 345,
    inputTokens: 1200,
    writes: [
      {
        postWriteVerify,
        result: "applied",
        approvedAt: new Date("2026-05-08T18:01:01.000Z"),
        approvalToken,
        appliedDiff,
        proposedDiff,
        idempotencyKey: "receipt-01ARZ3NDEKTSV4RRFFQ69G5FAV-write-1",
        target: "deal:5678",
        action: "hubspot.deals.update",
      },
    ],
    sourceReads: [
      {
        raw_ref: "hubspot://contacts/1234",
        citation: "HubSpot contact #1234 fetched at 2026-05-08T18:00:01.000Z",
        hash: sha256Hex("hubspot-contact-1234"),
        fetchedAt: new Date("2026-05-08T18:00:01.000Z"),
        entityId: "1234",
        entityType: "contact",
        provider: "hubspot",
      },
    ],
    commits: [
      {
        signed: true,
        parent_sha: "def456abc123",
        author_email: "fran@example.com",
        author: "Fran",
        message: SanitizedString.fromUnknown("docs: update meeting brief"),
        sha: "abc123def456",
      },
    ],
    filesChanged: [
      {
        lines_removed: 3,
        lines_added: 12,
        after_hash: sha256Hex("docs/brief.md:after"),
        before_hash: sha256Hex("docs/brief.md:before"),
        mode: "modified",
        path: "docs/brief.md",
      },
    ],
    approvals: [
      {
        decidedAt: new Date("2026-05-08T18:01:00.000Z"),
        signed_token: approvalToken,
        decision: "approve",
        role: "approver",
        approval_id: asApprovalId("approval_01"),
      },
    ],
    toolCalls: [
      {
        error: SanitizedString.fromUnknown(""),
        status: "ok",
        finished_at: new Date("2026-05-08T18:00:02.000Z"),
        started_at: new Date("2026-05-08T18:00:01.000Z"),
        output: SanitizedString.fromUnknown("Fetched HubSpot contact #1234"),
        inputs: toolInputs,
        tool_name: "hubspot.contacts.read",
        tool_id: asToolCallId("tool_01"),
      },
    ],
    toolManifest: sha256Hex("tool-manifest:v1"),
    promptHash: sha256Hex("prompt:v1"),
    model: "gpt-5.2",
    providerKind: asProviderKind("openai"),
    status: "ok",
    finishedAt: new Date("2026-05-08T18:05:00.000Z"),
    startedAt: new Date("2026-05-08T18:00:00.000Z"),
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    triggerKind: "human_message",
    taskId,
    agentSlug: asAgentSlug("sam_agent"),
    id: receiptId,
  };
}

function fuzzReceiptPayload(): fc.Arbitrary<unknown> {
  return fc.oneof(fc.anything(), fc.constant(validReceiptFixture()), corruptReceiptArbitrary());
}

function corruptReceiptArbitrary(): fc.Arbitrary<unknown> {
  return fc.constantFrom("id", "status", "inputTokens").map((field) => {
    const corruptValue = field === "id" ? "not-a-ulid" : field === "status" ? "done" : "many";
    return { ...validReceiptFixture(), [field]: corruptValue };
  });
}
