import { createCostLedger, createEventLog, openDatabase, runMigrations } from "@wuphf/broker/cost-ledger";
import { asAgentSlug, asMicroUsd } from "@wuphf/protocol";
import { describe, expect, it, vi } from "vitest";
import {
  BadRequestError,
  createGateway,
  ProviderError,
  type ProviderRequest,
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
  const create = vi.fn(
    async (
      params: AnthropicMessageCreateParams,
      _options?: { readonly headers?: Readonly<Record<string, string>> },
    ) => Promise.resolve(stub(params)),
  );
  return { client: { messages: { create } }, create };
}

describe("Anthropic pricing", () => {
  it("computes cost as integer μUSD from token counts (per-MTok fixed-point)", () => {
    // Sonnet 4.6 = $3/$15 per MTok = 3_000_000 / 15_000_000 μUSD/MTok.
    const cost = estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, "claude-sonnet-4-6", {
      inputTokens: 1_000,
      outputTokens: 500,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
    // (1000*3_000_000 + 500*15_000_000) / 1_000_000 = (3e9 + 7.5e9) / 1e6 = 10_500
    expect(cost as number).toBe(10_500);
  });

  it("accounts for cache read + creation lines (Opus 4.7 = $5/$25/$0.50/$6.25)", () => {
    const cost = estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, "claude-opus-4-7", {
      inputTokens: 100,
      outputTokens: 50,
      cacheReadTokens: 1_000,
      cacheCreationTokens: 100,
    });
    // 100*5e6 + 50*25e6 + 1000*5e5 + 100*6.25e6 = 5e8 + 1.25e9 + 5e8 + 6.25e8
    //   = 2.875e9, /1e6 = 2875.
    expect(cost as number).toBe(2_875);
  });

  it("preserves sub-μUSD/tok precision (Sonnet $0.30/MTok cache reads — B2-2 fix)", () => {
    // 1_000_000 Sonnet cache-read tokens at $0.30/MTok = exactly $0.30 = 300_000 μUSD.
    // The earlier per-tok-rounded design would have stored 0 μUSD.
    const cost = estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, "claude-sonnet-4-6", {
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 1_000_000,
      cacheCreationTokens: 0,
    });
    expect(cost as number).toBe(300_000);
  });

  it("Opus 4.5/4.6/4.7 use the post-2025-11 reduced pricing (B2-1 fix)", () => {
    for (const model of ["claude-opus-4-5", "claude-opus-4-6", "claude-opus-4-7"]) {
      // 1M input tokens at the Opus 4.5+ rate ($5/MTok) = $5 = 5_000_000 μUSD.
      const cost = estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, model, {
        inputTokens: 1_000_000,
        outputTokens: 0,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      });
      expect(cost as number).toBe(5_000_000);
    }
  });

  it("Opus 4.1 retains the legacy $15/MTok rate", () => {
    const cost = estimateAnthropicCostMicroUsd(DEFAULT_ANTHROPIC_PRICING, "claude-opus-4-1", {
      inputTokens: 1_000_000,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
    expect(cost as number).toBe(15_000_000);
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
          inputMicroUsdPerMTok: 8_000_000,
          outputMicroUsdPerMTok: 40_000_000,
          cacheReadMicroUsdPerMTok: 1_000_000,
          cacheCreationMicroUsdPerMTok: 10_000_000,
        },
      },
      "negotiated-opus",
      { inputTokens: 100, outputTokens: 50, cacheReadTokens: 0, cacheCreationTokens: 0 },
    );
    // (100*8e6 + 50*40e6) / 1e6 = 2.8e9 / 1e6 = 2_800.
    expect(cost as number).toBe(2_800);
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
    expect(create).toHaveBeenCalledWith(
      {
        model: "claude-sonnet-4-6",
        max_tokens: 64,
        messages: [{ role: "user", content: "say hi" }],
      },
      // B3-1: Idempotency-Key is now an explicit HTTP header — the SDK's
      // `options.idempotencyKey` shorthand is a silent no-op unless the
      // client's internal `idempotencyHeader` is configured.
      expect.objectContaining({
        headers: expect.objectContaining({
          "Idempotency-Key": expect.stringMatching(/^wuphf-[0-9a-f]{64}$/),
        }),
      }),
    );
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
          inputMicroUsdPerMTok: 1_000_000,
          outputMicroUsdPerMTok: 1_000_000,
          cacheReadMicroUsdPerMTok: 0,
          cacheCreationMicroUsdPerMTok: 0,
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

  it("threads Idempotency-Key as an HTTP header (B3-1: NOT options.idempotencyKey)", async () => {
    const { client, create } = fakeClient(() => emptyMessage());
    const provider = createAnthropicProvider({ client });
    const req: ProviderRequest = {
      model: "claude-sonnet-4-6",
      prompt: "same-payload",
      maxOutputTokens: 32,
    };
    await provider.complete(req);
    await provider.complete(req);
    const firstCall = create.mock.calls[0] as unknown as readonly [
      unknown,
      { readonly headers: Record<string, string> },
    ];
    const secondCall = create.mock.calls[1] as unknown as readonly [
      unknown,
      { readonly headers: Record<string, string> },
    ];
    const k1 = firstCall[1]?.headers["Idempotency-Key"];
    const k2 = secondCall[1]?.headers["Idempotency-Key"];
    expect(k1).toBe(k2);
    expect(k1).toMatch(/^wuphf-[0-9a-f]{64}$/);
  });

  it("uses gateway-supplied requestKey when present (B3-2)", async () => {
    const { client, create } = fakeClient(() => emptyMessage());
    const provider = createAnthropicProvider({ client });
    await provider.complete({
      model: "claude-sonnet-4-6",
      prompt: "same",
      maxOutputTokens: 16,
      requestKey: "ctx-scoped-bbb",
    });
    const call = create.mock.calls[0] as unknown as readonly [
      unknown,
      { readonly headers: Record<string, string> },
    ];
    expect(call[1]?.headers["Idempotency-Key"]).toBe("wuphf-ctx-scoped-bbb");
  });

  it("maps SDK 4xx to BadRequestError (NOT breaker-worthy) — B2-7", async () => {
    const sdkErr = {
      status: 400,
      headers: {},
      error: { error: { type: "invalid_request_error" } },
      requestID: "req_abc123",
      message: "max_tokens must be ≥ 1",
    };
    const provider = createAnthropicProvider({
      client: {
        messages: {
          create: async () => {
            throw sdkErr;
          },
        },
      },
    });
    const promise = provider.complete({
      model: "claude-sonnet-4-6",
      prompt: "p",
      maxOutputTokens: 8,
    });
    await expect(promise).rejects.toBeInstanceOf(BadRequestError);
    await expect(promise).rejects.toMatchObject({
      status: 400,
      requestId: "req_abc123",
      errorType: "invalid_request_error",
    });
  });

  it("maps SDK 429 to ProviderError with retryAfterMs extracted (B2-5)", async () => {
    const sdkErr = {
      status: 429,
      headers: { "retry-after": "12" },
      error: { error: { type: "rate_limit_error" } },
      requestID: "req_rate_1",
      message: "rate limited",
    };
    const provider = createAnthropicProvider({
      client: {
        messages: {
          create: async () => {
            throw sdkErr;
          },
        },
      },
    });
    const promise = provider.complete({
      model: "claude-sonnet-4-6",
      prompt: "p",
      maxOutputTokens: 8,
    });
    await expect(promise).rejects.toBeInstanceOf(ProviderError);
    await expect(promise).rejects.toMatchObject({
      status: 429,
      requestId: "req_rate_1",
      errorType: "rate_limit_error",
      retryAfterMs: 12_000,
    });
  });

  it("pre-rejects maxOutputTokens <= 0 without an SDK call (B2-7)", async () => {
    const { client, create } = fakeClient(() => emptyMessage());
    const provider = createAnthropicProvider({ client });
    await expect(
      provider.complete({ model: "claude-sonnet-4-6", prompt: "p", maxOutputTokens: 0 }),
    ).rejects.toBeInstanceOf(BadRequestError);
    expect(create).not.toHaveBeenCalled();
  });
});

describe("pricing-table validation (B2-6)", () => {
  it("rejects an empty pricing table at construction", () => {
    expect(() =>
      createAnthropicProvider({
        client: fakeClient(() => emptyMessage()).client,
        pricing: {},
      }),
    ).toThrow(/at least one model/);
  });

  it("rejects a non-integer rate at construction", () => {
    expect(() =>
      createAnthropicProvider({
        client: fakeClient(() => emptyMessage()).client,
        pricing: {
          "bad-model": {
            inputMicroUsdPerMTok: 1.5,
            outputMicroUsdPerMTok: 1,
            cacheReadMicroUsdPerMTok: 0,
            cacheCreationMicroUsdPerMTok: 0,
          },
        },
      }),
    ).toThrow(/non-negative safe integer/);
  });

  it("rejects a negative rate at construction", () => {
    expect(() =>
      createAnthropicProvider({
        client: fakeClient(() => emptyMessage()).client,
        pricing: {
          "bad-model": {
            inputMicroUsdPerMTok: -1,
            outputMicroUsdPerMTok: 1,
            cacheReadMicroUsdPerMTok: 0,
            cacheCreationMicroUsdPerMTok: 0,
          },
        },
      }),
    ).toThrow(/non-negative safe integer/);
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
