import { describe, expect, it } from "vitest";

import {
  type AgentProviderRouting,
  agentProviderRoutingEntryFromJson,
  agentProviderRoutingEntryToJsonValue,
  agentProviderRoutingFromJson,
  agentProviderRoutingReadRequestFromJson,
  agentProviderRoutingToJsonValue,
  agentProviderRoutingWriteRequestFromJson,
  agentProviderRoutingWriteResponseFromJson,
  asAgentId,
  asCredentialScope,
  asProviderKind,
  MAX_AGENT_PROVIDER_ROUTES,
} from "../src/index.ts";

const VALID_AGENT_ID = asAgentId("agent_alice_001");

function entry(kind: "claude-cli" | "codex-cli" | "openai-compat") {
  if (kind === "claude-cli") {
    return {
      kind: "claude-cli" as const,
      credentialScope: asCredentialScope("anthropic"),
      providerKind: asProviderKind("anthropic"),
    };
  }
  if (kind === "codex-cli") {
    return {
      kind: "codex-cli" as const,
      credentialScope: asCredentialScope("openai"),
      providerKind: asProviderKind("openai"),
    };
  }
  return {
    kind: "openai-compat" as const,
    credentialScope: asCredentialScope("openai-compat"),
    providerKind: asProviderKind("openai-compat"),
  };
}

describe("agentProviderRoutingFromJson", () => {
  it("round-trips a valid routing config", () => {
    const config: AgentProviderRouting = {
      agentId: VALID_AGENT_ID,
      routes: [entry("claude-cli"), entry("codex-cli")],
    };
    const json = agentProviderRoutingToJsonValue(config);
    const parsed = agentProviderRoutingFromJson(json);
    expect(parsed.agentId).toBe(VALID_AGENT_ID);
    expect(parsed.routes).toHaveLength(2);
    expect(parsed.routes[0]?.kind).toBe("claude-cli");
    expect(parsed.routes[1]?.kind).toBe("codex-cli");
  });

  it("normalizes route order regardless of input order", () => {
    const config = agentProviderRoutingFromJson({
      agentId: VALID_AGENT_ID,
      routes: [
        agentProviderRoutingEntryToJsonValue(entry("codex-cli")),
        agentProviderRoutingEntryToJsonValue(entry("claude-cli")),
      ],
    });
    expect(config.routes.map((r) => r.kind)).toEqual(["claude-cli", "codex-cli"]);
  });

  it("rejects duplicate routes for the same kind", () => {
    expect(() =>
      agentProviderRoutingFromJson({
        agentId: VALID_AGENT_ID,
        routes: [
          agentProviderRoutingEntryToJsonValue(entry("claude-cli")),
          agentProviderRoutingEntryToJsonValue(entry("claude-cli")),
        ],
      }),
    ).toThrow(/duplicate route for kind "claude-cli"/);
  });

  it("rejects more than MAX_AGENT_PROVIDER_ROUTES entries", () => {
    const tooMany = Array.from({ length: MAX_AGENT_PROVIDER_ROUTES + 1 }, () =>
      agentProviderRoutingEntryToJsonValue(entry("claude-cli")),
    );
    expect(() =>
      agentProviderRoutingFromJson({ agentId: VALID_AGENT_ID, routes: tooMany }),
    ).toThrow(/exceeds 16 entries/);
  });

  it("rejects unknown top-level keys", () => {
    expect(() =>
      agentProviderRoutingFromJson({
        agentId: VALID_AGENT_ID,
        routes: [],
        extra: "nope",
      }),
    ).toThrow(/is not allowed/);
  });

  it("rejects an unknown RunnerKind in an entry", () => {
    expect(() =>
      agentProviderRoutingEntryFromJson(
        {
          kind: "not-a-runner",
          credentialScope: "anthropic",
          providerKind: "anthropic",
        },
        "$entry",
      ),
    ).toThrow(/not a supported RunnerKind/);
  });

  it("rejects an unknown ProviderKind in an entry", () => {
    expect(() =>
      agentProviderRoutingEntryFromJson(
        {
          kind: "claude-cli",
          credentialScope: "anthropic",
          providerKind: "made-up-provider",
        },
        "$entry",
      ),
    ).toThrow(/not a supported ProviderKind/);
  });

  it("rejects non-array routes field", () => {
    expect(() =>
      agentProviderRoutingFromJson({
        agentId: VALID_AGENT_ID,
        routes: "claude-cli",
      }),
    ).toThrow(/must be an array/);
  });
});

describe("agentProviderRoutingReadRequestFromJson", () => {
  it("round-trips", () => {
    const parsed = agentProviderRoutingReadRequestFromJson({ agentId: VALID_AGENT_ID });
    expect(parsed.agentId).toBe(VALID_AGENT_ID);
  });

  it("rejects missing agentId", () => {
    expect(() => agentProviderRoutingReadRequestFromJson({})).toThrow(/agentId.*is required/);
  });
});

describe("agentProviderRoutingWriteRequestFromJson", () => {
  it("round-trips with an empty routes list (idempotent clear)", () => {
    const parsed = agentProviderRoutingWriteRequestFromJson({
      agentId: VALID_AGENT_ID,
      routes: [],
    });
    expect(parsed.routes).toHaveLength(0);
  });

  it("rejects unknown keys", () => {
    expect(() =>
      agentProviderRoutingWriteRequestFromJson({
        agentId: VALID_AGENT_ID,
        routes: [],
        foo: "bar",
      }),
    ).toThrow(/is not allowed/);
  });
});

describe("agentProviderRoutingWriteResponseFromJson", () => {
  it("requires applied:true", () => {
    expect(agentProviderRoutingWriteResponseFromJson({ applied: true })).toEqual({
      applied: true,
    });
    expect(() => agentProviderRoutingWriteResponseFromJson({ applied: false })).toThrow(
      /applied: must be true/,
    );
  });
});
