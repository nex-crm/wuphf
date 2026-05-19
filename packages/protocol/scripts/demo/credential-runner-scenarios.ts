import { inspect } from "node:util";

import {
  asAgentId,
  asApprovalRole,
  asCredentialHandleId,
  asCredentialScope,
  createCredentialHandle,
  credentialHandleFromJson,
  credentialHandleToJson,
  isApprovalRole,
  isRunnerFailureCode,
  RUNNER_FAILURE_CODE_VALUES,
  runnerEventFromJson,
  runnerEventToJsonValue,
  runnerSpawnRequestFromJson,
  runnerSpawnRequestToJsonValue,
} from "../../src/index.ts";
import { expectEqual, expectThrows, header } from "./harness.ts";

export function runCredentialRunnerScenarios(): void {
  // ────────────────────────────────────────────────────────────────────────
  header(29, "CredentialHandle is opaque and redacted");
  // ────────────────────────────────────────────────────────────────────────
  const credentialHandle = createCredentialHandle({
    id: asCredentialHandleId("cred_0123456789ABCDEFGHIJKLMNOPQRSTUV"),
    agentId: asAgentId("agent_alpha"),
    scope: asCredentialScope("openai"),
  });
  const credentialFixtureSecret = "fixture-secret-value-do-not-use-0000";
  const credentialJson = JSON.stringify(credentialHandle);
  expectEqual(
    "CredentialHandle JSON carries only versioned id",
    credentialJson,
    '{"version":1,"id":"cred_0123456789ABCDEFGHIJKLMNOPQRSTUV"}',
  );
  expectEqual(
    "credentialHandleToJson returns the wire shape",
    credentialHandleToJson(credentialHandle),
    {
      version: 1,
      id: asCredentialHandleId("cred_0123456789ABCDEFGHIJKLMNOPQRSTUV"),
    },
  );
  expectEqual(
    "CredentialHandle JSON omits secret",
    credentialJson.includes(credentialFixtureSecret),
    false,
  );
  expectEqual(
    "CredentialHandle toString is redacted",
    String(credentialHandle),
    "CredentialHandle(<redacted>)",
  );
  expectEqual(
    "CredentialHandle inspect is redacted",
    inspect(credentialHandle),
    "CredentialHandle { id: <redacted> }",
  );
  expectEqual(
    "structuredClone(CredentialHandle) loses handle capability",
    structuredClone(credentialHandle),
    {},
  );
  expectThrows(
    () =>
      credentialHandleFromJson(JSON.parse(credentialJson), {
        broker: {} as never,
        agentId: asAgentId("agent_alpha"),
        scope: asCredentialScope("openai"),
      }),
    /BrokerIdentity is required/,
  );

  header(30, "RunnerSpawnRequest and RunnerEvent reject drift at the broker boundary");
  const runnerSpawnJson = {
    schemaVersion: 1,
    kind: "claude-cli",
    agentId: "agent_alpha",
    credential: { version: 1, id: "cred_runner0123456789ABCDEFGHIJKLMN" },
    providerRoute: {
      credentialScope: "anthropic",
      providerKind: "anthropic",
    },
    options: {
      kind: "claude-cli",
      extraArgs: ["--max-turns", "3"],
    },
    prompt: "Summarize the task state.",
    model: "claude-sonnet-4-7",
    taskId: "01ARZ3NDEKTSV4RRFFQ69G5FAW",
    costCeilingMicroUsd: 2_500_000,
  };
  const runnerSpawn = runnerSpawnRequestFromJson(runnerSpawnJson);
  expectEqual(
    "runner spawn request round-trips",
    runnerSpawnRequestToJsonValue(runnerSpawn),
    runnerSpawnJson,
  );
  expectThrows(
    () => runnerSpawnRequestFromJson({ ...runnerSpawnJson, extra: true }),
    /runnerSpawnRequest\/extra: is not allowed/,
  );
  expectThrows(
    () => runnerSpawnRequestFromJson({ ...runnerSpawnJson, schemaVersion: 999 }),
    /unsupported schemaVersion/,
  );
  const openAICompatSpawnJson = {
    schemaVersion: 1,
    kind: "openai-compat",
    agentId: "agent_alpha",
    credential: { version: 1, id: "cred_runner0123456789ABCDEFGHIJKLMN" },
    options: {
      kind: "openai-compat",
      endpoint: "https://api.openai.com/v1/chat/completions",
      headers: { "OpenAI-Beta": "assistants=v2" },
      timeoutMs: 60_000,
    },
    prompt: "Summarize the task state.",
  };
  expectEqual(
    "openai-compatible runner options round-trip through the wire shape",
    runnerSpawnRequestToJsonValue(runnerSpawnRequestFromJson(openAICompatSpawnJson)),
    openAICompatSpawnJson,
  );
  expectThrows(
    () =>
      runnerSpawnRequestFromJson({
        ...openAICompatSpawnJson,
        options: { kind: "openai-compat" },
      }),
    /runnerSpawnRequest.options.endpoint: is required/,
  );
  const runnerCostEvent = runnerEventFromJson({
    schemaVersion: 1,
    kind: "cost",
    runnerId: "run_0123456789ABCDEFGHIJKLMNOPQRSTUV",
    entry: {
      agentSlug: "agent_alpha",
      providerKind: "anthropic",
      model: "claude-sonnet-4-7",
      amountMicroUsd: 2048,
      units: {
        inputTokens: 1536,
        outputTokens: 512,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      },
      occurredAt: "2026-05-08T18:00:02.000Z",
    },
    at: "2026-05-08T18:00:02.000Z",
  });
  expectEqual(
    "runner cost event emits canonical JSON values",
    runnerEventToJsonValue(runnerCostEvent),
    {
      schemaVersion: 1,
      kind: "cost",
      runnerId: "run_0123456789ABCDEFGHIJKLMNOPQRSTUV",
      entry: {
        agentSlug: "agent_alpha",
        providerKind: "anthropic",
        model: "claude-sonnet-4-7",
        amountMicroUsd: 2048,
        units: {
          inputTokens: 1536,
          outputTokens: 512,
          cacheReadTokens: 0,
          cacheCreationTokens: 0,
        },
        occurredAt: "2026-05-08T18:00:02.000Z",
      },
      at: "2026-05-08T18:00:02.000Z",
    },
  );
  expectThrows(
    () =>
      runnerEventFromJson({
        schemaVersion: 1,
        kind: "stdout",
        runnerId: "run_0123456789ABCDEFGHIJKLMNOPQRSTUV",
        chunk: "ok",
        at: "2026-05-08T18:00:02Z",
      }),
    /ISO8601 UTC millisecond/,
  );
  expectEqual(
    "runner failure code registry exposes stable machine codes",
    RUNNER_FAILURE_CODE_VALUES.includes("runner_input_buffer_overflow"),
    true,
  );
  expectEqual(
    "runner failure code guard accepts stable machine codes",
    isRunnerFailureCode("runner_input_buffer_overflow"),
    true,
  );
  expectEqual(
    "runner failure code guard rejects unknown values",
    isRunnerFailureCode("bogus"),
    false,
  );
  const runnerFailedEvent = runnerEventFromJson({
    schemaVersion: 1,
    kind: "failed",
    runnerId: "run_0123456789ABCDEFGHIJKLMNOPQRSTUV",
    error: "usage exceeded configured cost ceiling",
    code: "cost_ceiling_exceeded",
    at: "2026-05-08T18:00:03.000Z",
  });
  expectEqual(
    "runner failed event carries stable code",
    runnerEventToJsonValue(runnerFailedEvent),
    {
      schemaVersion: 1,
      kind: "failed",
      runnerId: "run_0123456789ABCDEFGHIJKLMNOPQRSTUV",
      error: "usage exceeded configured cost ceiling",
      code: "cost_ceiling_exceeded",
      at: "2026-05-08T18:00:03.000Z",
    },
  );
  expectThrows(
    () =>
      runnerEventFromJson({
        schemaVersion: 1,
        kind: "failed",
        runnerId: "run_0123456789ABCDEFGHIJKLMNOPQRSTUV",
        error: "provider returned a bad response",
        code: "not_a_stable_code",
        at: "2026-05-08T18:00:03.000Z",
      }),
    /RunnerFailureCode/,
  );

  header(31, "ApprovalRole helpers keep the closed enum importable at runtime");
  expectEqual("asApprovalRole accepts a known role", asApprovalRole("approver"), "approver");
  expectEqual("isApprovalRole rejects unknown roles", isApprovalRole("custom"), false);
  expectThrows(() => asApprovalRole("custom"), /ApprovalRole/);
}
