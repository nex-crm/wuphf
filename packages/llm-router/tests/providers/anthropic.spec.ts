import { createCostLedger, createEventLog, openDatabase, runMigrations } from "@wuphf/broker";
import { asAgentSlug, asMicroUsd } from "@wuphf/protocol";
import { describe, expect, it, vi } from "vitest";
import {
  createGateway,
  ProviderError,
  type SupervisorContext,
  UnknownModelError,
} from "../../src/index.ts";
import {
  type AnthropicClient,
  type AnthropicMessage,
  type AnthropicMessageCreateParams,
  createAnthropicProvider,
  DEFAULT_ANTHROPIC_PRICING,
  estimateAnthropicCostMicroUsd,
} from "../../src/providers/anthropic.ts";

function fakeClient(stub: (params: AnthropicMessageCreateParams) => AnthropicMessage): {
  readonly client: AnthropicClient;
  readonly create: ReturnType<typeof vi.fn>;
} {
  const create = vi.fn(async (params: AnthropicMessageCreateParams) =>
    Promise.resolve(stub(params)),
  );
  return { client: { messages: { create } }, create };
}

describe("Anthropic pricing", () => {
  it("computes cost as integer μUSD from token counts", () => {
    // Sonnet 4.6 = $3 / $15 input/output per MTok = 3 / 15 μUSD/tok.
    const cost = estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, "claude-sonnet-4-6", {
      inputTokens: 1_000,
      outputTokens: 500,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
    // 1000 * 3 + 500 * 15 = 3000 + 7500 = 10500 μUSD = $0.0105
    expect(cost as number).toBe(10_500);
  });

  it("accounts for cache read + creation lines", () => {
    // Opus rates: input 15, output 75, cache_read 1 (1.5 rounded down),
    // cache_creation 19 (18.75 rounded up).
    const cost = estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, "claude-opus-4-7", {
      inputTokens: 100,
      outputTokens: 50,
      cacheReadTokens: 1_000,
      cacheCreationTokens: 100,
    });
    // 100*15 + 50*75 + 1000*1 + 100*19 = 1500 + 3750 + 1000 + 1900 = 8150
    expect(cost as number).toBe(8_150);
  });

  it("throws UnknownModelError for unrecognized models", () => {
    expect(() =>
      estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, "claude-mystery-99", {
        inputTokens: 1,
        outputTokens: 1,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      }),
    ).toThrow(UnknownModelError);
  });

  it("respects a host-supplied pricing-table override", () => {
    const cost = estimateAnthropicCostMicroUsd(
      {
        "negotiated-opus": {
          inputMicroUsdPerToken: 8, // 8 μUSD/tok = $8/MTok negotiated rate
          outputMicroUsdPerToken: 40,
          cacheReadMicroUsdPerToken: 1,
          cacheCreationMicroUsdPerToken: 10,
        },
      },
      "negotiated-opus",
      { inputTokens: 100, outputTokens: 50, cacheReadTokens: 0, cacheCreationTokens: 0 },
    );
    expect(cost as number).toBe(100 * 8 + 50 * 40);
  });
});

describe("AnthropicProvider", () => {
  it("translates ProviderRequest → Anthropic Messages.create params", async () => {
    const { client, create } = fakeClient(() => ({
      content: [{ type: "text", text: "hello world" }],
      usage: {
        input_tokens: 100,
        output_tokens: 50,
        cache_read_input_tokens: 0,
        cache_creation_input_tokens: 0,
      },
    }));
    const provider = createAnthropicProvider({ client });

    const res = await provider.complete({
      model: "claude-sonnet-4-6",
      prompt: "say hi",
      maxOutputTokens: 64,
    });

    expect(create).toHaveBeenCalledOnce();
    expect(create).toHaveBeenCalledWith({
      model: "claude-sonnet-4-6",
      max_tokens: 64,
      messages: [{ role: "user", content: "say hi" }],
    });
    expect(res.text).toBe("hello world");
    expect(res.usage).toEqual({
      inputTokens: 100,
      outputTokens: 50,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
  });

  it("declares models = pricing table keys for gateway routing", () => {
    const provider = createAnthropicProvider({ client: fakeClient(() => emptyMessage()).client });
    expect(provider.models).toEqual(Object.keys(DEFAULT_ANTHROPIC_PRICING));
    expect(provider.models).toContain("claude-opus-4-7");
    expect(provider.models).toContain("claude-sonnet-4-6");
    expect(provider.models).toContain("claude-haiku-4-5");
  });

  it("uses pricing-override model list when provided", () => {
    const provider = createAnthropicProvider({
      client: fakeClient(() => emptyMessage()).client,
      pricing: {
        "negotiated-x": {
          inputMicroUsdPerToken: 1,
          outputMicroUsdPerToken: 1,
          cacheReadMicroUsdPerToken: 0,
          cacheCreationMicroUsdPerToken: 0,
        },
      },
    });
    expect(provider.models).toEqual(["negotiated-x"]);
  });

  it("flattens multi-block content into one string", async () => {
    const { client } = fakeClient(() => ({
      content: [
        { type: "text", text: "first " },
        { type: "thinking" }, // ignored: not text
        { type: "text", text: "second" },
      ],
      usage: {
        input_tokens: 1,
        output_tokens: 1,
        cache_read_input_tokens: null,
        cache_creation_input_tokens: null,
      },
    }));
    const provider = createAnthropicProvider({ client });
    const res = await provider.complete({
      model: "claude-sonnet-4-6",
      prompt: "p",
      maxOutputTokens: 8,
    });
    expect(res.text).toBe("first second");
    // null cache fields coerce to 0.
    expect(res.usage.cacheReadTokens).toBe(0);
    expect(res.usage.cacheCreationTokens).toBe(0);
  });

  it("wraps SDK errors as ProviderError", async () => {
    const sdkErr = new Error("rate_limited");
    const provider = createAnthropicProvider({
      client: {
        messages: {
          create: async () => {
            throw sdkErr;
          },
        },
      },
    });
    await expect(
      provider.complete({ model: "claude-opus-4-7", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toBeInstanceOf(ProviderError);
  });

  it("rejects models not in the pricing table with UnknownModelError", async () => {
    const provider = createAnthropicProvider({ client: fakeClient(() => emptyMessage()).client });
    await expect(
      provider.complete({ model: "claude-not-real", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toBeInstanceOf(UnknownModelError);
  });
});

describe("AnthropicProvider → Gateway → CostLedger end-to-end (mocked SDK)", () => {
  it("a full Gateway.complete() call writes the right amount to cost_by_agent", async () => {
    const { client } = fakeClient(() => ({
      content: [{ type: "text", text: "ok" }],
      usage: {
        input_tokens: 1_000,
        output_tokens: 500,
        cache_read_input_tokens: 0,
        cache_creation_input_tokens: 0,
      },
    }));
    const provider = createAnthropicProvider({ client });

    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const clock = { now: new Date("2026-05-12T10:00:00.000Z").getTime() };
    const gateway = createGateway({
      ledger,
      providers: [provider],
      nowMs: () => clock.now,
    });
    try {
      const ctx: SupervisorContext = { agentSlug: asAgentSlug("primary") };
      const result = await gateway.complete(ctx, {
        model: "claude-sonnet-4-6",
        prompt: "go",
        maxOutputTokens: 1_024,
      });
      // Sonnet rate: 1000*3 + 500*15 = 10500 μUSD = $0.0105.
      expect(result.costMicroUsd as number).toBe(10_500);
      const agentRow = ledger.getAgentSpend("primary", "2026-05-12");
      expect(agentRow?.totalMicroUsd as number).toBe(10_500);
    } finally {
      db.close();
    }
  });
});

function emptyMessage(): AnthropicMessage {
  return {
    content: [],
    usage: {
      input_tokens: 0,
      output_tokens: 0,
      cache_read_input_tokens: null,
      cache_creation_input_tokens: null,
    },
  };
}

// Keep this around so the `vi` import isn't flagged when the file is
// trimmed in future refactors.
void asMicroUsd;
