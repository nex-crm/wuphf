import {
  createCostLedger,
  createEventLog,
  openDatabase,
  runMigrations,
} from "@wuphf/broker/cost-ledger";
import { asAgentSlug } from "@wuphf/protocol";
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
  createOpenAIProvider,
  DEFAULT_OPENAI_PRICING,
  estimateOpenAICostMicroUsd,
  type OpenAIChatCompletion,
  type OpenAIChatCompletionCreateParams,
  type OpenAIClient,
} from "../../src/providers/openai.ts";

function fakeClient(stub: (params: OpenAIChatCompletionCreateParams) => OpenAIChatCompletion): {
  readonly client: OpenAIClient;
  readonly create: ReturnType<typeof vi.fn>;
} {
  const create = vi.fn(
    async (
      params: OpenAIChatCompletionCreateParams,
      _options?: { readonly headers?: Readonly<Record<string, string>> },
    ) => Promise.resolve(stub(params)),
  );
  return { client: { chat: { completions: { create } } }, create };
}

describe("OpenAI pricing", () => {
  it("computes cost as integer μUSD from token counts", () => {
    // gpt-5 = $1.25/$10 per MTok = 1_250_000 / 10_000_000 μUSD/MTok.
    const cost = estimateOpenAICostMicroUsd(DEFAULT_OPENAI_PRICING, "gpt-5", {
      inputTokens: 1_000,
      outputTokens: 500,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
    // (1000*1_250_000 + 500*10_000_000) / 1_000_000 = 1.25e9 + 5e9 = 6.25e9 / 1e6 = 6_250
    expect(cost as number).toBe(6_250);
  });

  it("applies cached-input discount for cached prompt tokens", () => {
    // gpt-5: input $1.25/MTok, cached $0.125/MTok (10x discount).
    // 1M cached tokens billed at the cached rate = exactly $0.125 = 125_000 μUSD.
    const cost = estimateOpenAICostMicroUsd(DEFAULT_OPENAI_PRICING, "gpt-5", {
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 1_000_000,
      cacheCreationTokens: 0,
    });
    expect(cost as number).toBe(125_000);
  });

  it("throws UnknownModelError for unrecognized models", () => {
    expect(() =>
      estimateOpenAICostMicroUsd(DEFAULT_OPENAI_PRICING, "gpt-mystery", {
        inputTokens: 1,
        outputTokens: 1,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      }),
    ).toThrow(UnknownModelError);
  });

  it("respects a host-supplied pricing-table override", () => {
    const cost = estimateOpenAICostMicroUsd(
      {
        "negotiated-gpt": {
          inputMicroUsdPerMTok: 800_000,
          outputMicroUsdPerMTok: 4_000_000,
          cachedInputMicroUsdPerMTok: 80_000,
        },
      },
      "negotiated-gpt",
      { inputTokens: 100, outputTokens: 50, cacheReadTokens: 0, cacheCreationTokens: 0 },
    );
    // (100*800_000 + 50*4_000_000) / 1e6 = 2.8e8 / 1e6 = 280.
    expect(cost as number).toBe(280);
  });
});

describe("OpenAIProvider", () => {
  it("translates ProviderRequest → chat.completions.create params", async () => {
    const { client, create } = fakeClient(() => ({
      model: "gpt-5-mini",
      choices: [
        {
          message: { content: "hello world", refusal: null },
          finish_reason: "stop",
        },
      ],
      usage: { prompt_tokens: 100, completion_tokens: 50 },
    }));
    const provider = createOpenAIProvider({ client });

    const res = await provider.complete({
      model: "gpt-5-mini",
      prompt: "say hi",
      maxOutputTokens: 64,
    });

    expect(create).toHaveBeenCalledOnce();
    expect(create).toHaveBeenCalledWith(
      {
        model: "gpt-5-mini",
        // gpt-5* is a reasoning/GPT-5 model — only max_completion_tokens.
        max_completion_tokens: 64,
        messages: [{ role: "user", content: "say hi" }],
      },
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

  it("splits cached_tokens out of prompt_tokens for correct billing", async () => {
    // OpenAI: prompt_tokens=1000 includes cached_tokens=400.
    // The adapter MUST surface 600 fresh input + 400 cached, not
    // 1000 fresh input + 400 cached (which would double-bill).
    const { client } = fakeClient(() => ({
      model: "gpt-5",
      choices: [{ message: { content: "ok", refusal: null }, finish_reason: "stop" }],
      usage: {
        prompt_tokens: 1_000,
        completion_tokens: 0,
        prompt_tokens_details: { cached_tokens: 400 },
      },
    }));
    const provider = createOpenAIProvider({ client });
    const res = await provider.complete({
      model: "gpt-5",
      prompt: "p",
      maxOutputTokens: 8,
    });
    expect(res.usage).toEqual({
      inputTokens: 600, // 1000 - 400
      outputTokens: 0,
      cacheReadTokens: 400,
      cacheCreationTokens: 0,
    });
  });

  it("surfaces a refusal in the separate refusal field — NOT folded into text (B3-3)", async () => {
    const { client } = fakeClient(() => ({
      model: "gpt-5",
      choices: [
        {
          message: { content: null, refusal: "I can't help with that." },
          finish_reason: "content_filter",
        },
      ],
      usage: { prompt_tokens: 5, completion_tokens: 5 },
    }));
    const provider = createOpenAIProvider({ client });
    const res = await provider.complete({
      model: "gpt-5",
      prompt: "p",
      maxOutputTokens: 8,
    });
    // Refusal lives in its own field; text is empty so a caller that
    // ignores `refusal` doesn't accidentally treat the refusal prose
    // as a successful completion.
    expect(res.text).toBe("");
    expect(res.refusal).toBe("I can't help with that.");
    expect(res.finishReason).toBe("content_filter");
  });

  it("surfaces finishReason for normal completions (B3-3)", async () => {
    const { client } = fakeClient(() => ({
      model: "gpt-5",
      choices: [{ message: { content: "ok", refusal: null }, finish_reason: "stop" }],
      usage: { prompt_tokens: 5, completion_tokens: 5 },
    }));
    const provider = createOpenAIProvider({ client });
    const res = await provider.complete({
      model: "gpt-5",
      prompt: "p",
      maxOutputTokens: 8,
    });
    expect(res.text).toBe("ok");
    expect(res.refusal).toBeUndefined();
    expect(res.finishReason).toBe("stop");
  });

  it("classifies missing usage as ProviderError (B3-5)", async () => {
    const provider = createOpenAIProvider({
      client: {
        chat: {
          completions: {
            create: async () => ({
              model: "gpt-5",
              choices: [{ message: { content: "ok", refusal: null }, finish_reason: "stop" }],
              // No usage field at all — SDK marks it optional.
            }),
          },
        },
      },
    });
    await expect(
      provider.complete({ model: "gpt-5", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toBeInstanceOf(ProviderError);
  });

  it("clamps cached_tokens to prompt_tokens (B3-6)", async () => {
    // Malformed: cached_tokens > prompt_tokens. Adapter should clamp
    // cached to prompt_tokens, not pass through unchanged.
    const { client } = fakeClient(() => ({
      model: "gpt-5",
      choices: [{ message: { content: "ok", refusal: null }, finish_reason: "stop" }],
      usage: {
        prompt_tokens: 100,
        completion_tokens: 10,
        prompt_tokens_details: { cached_tokens: 9999 },
      },
    }));
    const provider = createOpenAIProvider({ client });
    const res = await provider.complete({
      model: "gpt-5",
      prompt: "p",
      maxOutputTokens: 8,
    });
    expect(res.usage.cacheReadTokens).toBe(100); // clamped to prompt_tokens
    expect(res.usage.inputTokens).toBe(0); // 100 - 100
  });

  it("uses max_tokens (NOT max_completion_tokens) for GPT-4.1 (B3-4)", async () => {
    const { client, create } = fakeClient(() => ({
      model: "gpt-4.1",
      choices: [{ message: { content: "ok", refusal: null }, finish_reason: "stop" }],
      usage: { prompt_tokens: 5, completion_tokens: 5 },
    }));
    const provider = createOpenAIProvider({ client });
    await provider.complete({ model: "gpt-4.1", prompt: "p", maxOutputTokens: 64 });
    const params = (create.mock.calls[0] as unknown as readonly [Record<string, unknown>])[0];
    expect(params).toMatchObject({ max_tokens: 64 });
    expect(params).not.toHaveProperty("max_completion_tokens");
  });

  it("declares models = pricing table keys for gateway routing", () => {
    const provider = createOpenAIProvider({ client: fakeClient(() => emptyCompletion()).client });
    expect(provider.models).toEqual(Object.keys(DEFAULT_OPENAI_PRICING));
    expect(provider.models).toContain("gpt-5");
    expect(provider.models).toContain("gpt-4.1-nano");
  });

  it("threads Idempotency-Key as an HTTP header (B3-1: NOT via options.idempotencyKey)", async () => {
    const { client, create } = fakeClient(() => emptyCompletion());
    const provider = createOpenAIProvider({ client });
    const req: ProviderRequest = {
      model: "gpt-5",
      prompt: "same-payload",
      maxOutputTokens: 16,
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

  it("uses gateway-supplied requestKey when present (B3-2: context-scoped dedup)", async () => {
    const { client, create } = fakeClient(() => emptyCompletion());
    const provider = createOpenAIProvider({ client });
    await provider.complete({
      model: "gpt-5",
      prompt: "same-payload",
      maxOutputTokens: 16,
      requestKey: "ctx-scoped-hash-aaa",
    });
    const call = create.mock.calls[0] as unknown as readonly [
      unknown,
      { readonly headers: Record<string, string> },
    ];
    expect(call[1]?.headers["Idempotency-Key"]).toBe("wuphf-ctx-scoped-hash-aaa");
  });

  it("maps SDK 400 to BadRequestError (NOT breaker-worthy)", async () => {
    const sdkErr = {
      status: 400,
      headers: {},
      error: { type: "invalid_request_error", code: "max_tokens_too_large" },
      request_id: "req_abc",
      message: "max_tokens too large",
    };
    const provider = createOpenAIProvider({
      client: {
        chat: {
          completions: {
            create: async () => {
              throw sdkErr;
            },
          },
        },
      },
    });
    const promise = provider.complete({ model: "gpt-5", prompt: "p", maxOutputTokens: 8 });
    await expect(promise).rejects.toBeInstanceOf(BadRequestError);
    await expect(promise).rejects.toMatchObject({
      status: 400,
      requestId: "req_abc",
      errorType: "invalid_request_error",
    });
  });

  it("maps SDK 429 to ProviderError with retryAfterMs extracted", async () => {
    const sdkErr = {
      status: 429,
      headers: { "retry-after": "8" },
      error: { type: "rate_limit_exceeded" },
      request_id: "req_rate",
      message: "rate limited",
    };
    const provider = createOpenAIProvider({
      client: {
        chat: {
          completions: {
            create: async () => {
              throw sdkErr;
            },
          },
        },
      },
    });
    const promise = provider.complete({ model: "gpt-5", prompt: "p", maxOutputTokens: 8 });
    await expect(promise).rejects.toBeInstanceOf(ProviderError);
    await expect(promise).rejects.toMatchObject({
      status: 429,
      requestId: "req_rate",
      errorType: "rate_limit_exceeded",
      retryAfterMs: 8_000,
    });
  });

  it("pre-rejects maxOutputTokens <= 0 without an SDK call", async () => {
    const { client, create } = fakeClient(() => emptyCompletion());
    const provider = createOpenAIProvider({ client });
    await expect(
      provider.complete({ model: "gpt-5", prompt: "p", maxOutputTokens: 0 }),
    ).rejects.toBeInstanceOf(BadRequestError);
    expect(create).not.toHaveBeenCalled();
  });
});

describe("OpenAI pricing-table validation", () => {
  it("rejects an empty pricing table at construction", () => {
    expect(() =>
      createOpenAIProvider({
        client: fakeClient(() => emptyCompletion()).client,
        pricing: {},
      }),
    ).toThrow(/at least one model/);
  });

  it("rejects a negative rate at construction", () => {
    expect(() =>
      createOpenAIProvider({
        client: fakeClient(() => emptyCompletion()).client,
        pricing: {
          "bad-model": {
            inputMicroUsdPerMTok: -1,
            outputMicroUsdPerMTok: 1,
            cachedInputMicroUsdPerMTok: 0,
          },
        },
      }),
    ).toThrow(/non-negative safe integer/);
  });
});

describe("OpenAIProvider → Gateway → CostLedger end-to-end (mocked SDK)", () => {
  it("writes the right amount to cost_by_agent (with cached tokens)", async () => {
    const { client } = fakeClient(() => ({
      model: "gpt-5",
      choices: [{ message: { content: "ok", refusal: null }, finish_reason: "stop" }],
      usage: {
        prompt_tokens: 1_000, // includes 200 cached
        completion_tokens: 200,
        prompt_tokens_details: { cached_tokens: 200 },
      },
    }));
    const provider = createOpenAIProvider({ client });

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
        model: "gpt-5",
        prompt: "go",
        maxOutputTokens: 1_024,
      });
      // 800 fresh input × $1.25 + 200 output × $10 + 200 cached × $0.125
      //   = (800*1_250_000 + 200*10_000_000 + 200*125_000) / 1e6
      //   = (1e9 + 2e9 + 2.5e7) / 1e6 = 3025.
      expect(result.costMicroUsd as number).toBe(3_025);
      const agentRow = ledger.getAgentSpend("primary", "2026-05-12");
      expect(agentRow?.totalMicroUsd as number).toBe(3_025);
    } finally {
      db.close();
    }
  });
});

function emptyCompletion(): OpenAIChatCompletion {
  return {
    model: "gpt-5",
    choices: [{ message: { content: "", refusal: null }, finish_reason: "stop" }],
    usage: { prompt_tokens: 0, completion_tokens: 0 },
  };
}
