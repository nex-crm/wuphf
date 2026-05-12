import { createCostLedger, createEventLog, openDatabase, runMigrations } from "@wuphf/broker";
import { asAgentSlug } from "@wuphf/protocol";
import { describe, expect, it, vi } from "vitest";

import {
  BadRequestError,
  createGateway,
  ProviderError,
  type SupervisorContext,
  UnknownModelError,
} from "../../src/index.ts";
import {
  createOllamaProvider,
  DEFAULT_OLLAMA_PRICING,
  estimateOllamaCostMicroUsd,
  type OllamaChatRequest,
  type OllamaChatResponse,
  type OllamaClient,
} from "../../src/providers/ollama.ts";

function fakeClient(stub: (request: OllamaChatRequest) => OllamaChatResponse): {
  readonly client: OllamaClient;
  readonly chat: ReturnType<typeof vi.fn>;
} {
  const chat = vi.fn(async (request: OllamaChatRequest) => Promise.resolve(stub(request)));
  return { client: { chat }, chat };
}

describe("Ollama pricing", () => {
  it("returns zero cost for the default (free, local) pricing table", () => {
    // Ollama runs locally; default rates are all zero. Any usage shape
    // must still produce a non-negative integer (0).
    const cost = estimateOllamaCostMicroUsd(DEFAULT_OLLAMA_PRICING, "llama3.3", {
      inputTokens: 1_000,
      outputTokens: 500,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
    expect(cost as number).toBe(0);
  });

  it("returns zero across every default-table model", () => {
    // Sanity: the contract is "every default model bills at $0".
    for (const model of Object.keys(DEFAULT_OLLAMA_PRICING)) {
      const cost = estimateOllamaCostMicroUsd(DEFAULT_OLLAMA_PRICING, model, {
        inputTokens: 1_000_000,
        outputTokens: 1_000_000,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      });
      expect(cost as number).toBe(0);
    }
  });

  it("throws UnknownModelError for unrecognized models", () => {
    expect(() =>
      estimateOllamaCostMicroUsd(DEFAULT_OLLAMA_PRICING, "llama-mystery", {
        inputTokens: 1,
        outputTokens: 1,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      }),
    ).toThrow(UnknownModelError);
  });

  it("respects a host-supplied pricing-table override (GPU cost modeling)", () => {
    // Host that wants to model GPU/electricity cost passes non-zero
    // rates; the same integer-μUSD/MTok math applies.
    const cost = estimateOllamaCostMicroUsd(
      {
        "llama3.3-local-gpu": {
          inputMicroUsdPerMTok: 100_000, // $0.10/MTok hypothetical GPU cost
          outputMicroUsdPerMTok: 200_000, // $0.20/MTok
          cacheReadMicroUsdPerMTok: 0,
          cacheCreationMicroUsdPerMTok: 0,
        },
      },
      "llama3.3-local-gpu",
      { inputTokens: 1_000, outputTokens: 500, cacheReadTokens: 0, cacheCreationTokens: 0 },
    );
    // (1000*100_000 + 500*200_000) / 1e6 = (1e8 + 1e8) / 1e6 = 200.
    expect(cost as number).toBe(200);
  });
});

describe("OllamaProvider", () => {
  it("translates ProviderRequest → ollama chat request with num_predict", async () => {
    const { client, chat } = fakeClient(() => ({
      model: "llama3.3",
      message: { role: "assistant", content: "hello world" },
      done: true,
      prompt_eval_count: 100,
      eval_count: 50,
    }));
    const provider = createOllamaProvider({ client });

    const res = await provider.complete({
      model: "llama3.3",
      prompt: "say hi",
      maxOutputTokens: 64,
    });

    expect(chat).toHaveBeenCalledOnce();
    expect(chat).toHaveBeenCalledWith({
      model: "llama3.3",
      messages: [{ role: "user", content: "say hi" }],
      stream: false,
      options: { num_predict: 64 },
    });
    expect(res.text).toBe("hello world");
    expect(res.usage).toEqual({
      inputTokens: 100,
      outputTokens: 50,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
  });

  it("does NOT pass an options/idempotency bag (local; no server dedup)", async () => {
    // Sanity: the SDK's chat() takes a single request object — there is
    // no second-argument options bag like Anthropic/OpenAI use for
    // idempotency-key headers. The adapter must call chat with exactly
    // one argument so a future SDK that tightens its overload signature
    // still accepts our call shape.
    const { client, chat } = fakeClient(() => emptyResponse());
    const provider = createOllamaProvider({ client });
    await provider.complete({ model: "llama3.3", prompt: "p", maxOutputTokens: 8 });
    expect(chat.mock.calls[0]).toHaveLength(1);
  });

  it("declares models = pricing table keys for gateway routing", () => {
    const provider = createOllamaProvider({ client: fakeClient(() => emptyResponse()).client });
    expect(provider.models).toEqual(Object.keys(DEFAULT_OLLAMA_PRICING));
    expect(provider.models).toContain("llama3.3");
    expect(provider.models).toContain("qwen2.5");
    expect(provider.models).toContain("gemma2");
  });

  it("uses pricing-override model list when provided", () => {
    const provider = createOllamaProvider({
      client: fakeClient(() => emptyResponse()).client,
      pricing: {
        "custom-local-model": {
          inputMicroUsdPerMTok: 0,
          outputMicroUsdPerMTok: 0,
          cacheReadMicroUsdPerMTok: 0,
          cacheCreationMicroUsdPerMTok: 0,
        },
      },
    });
    expect(provider.models).toEqual(["custom-local-model"]);
  });

  it("coerces missing eval counters to zero (older response shapes)", async () => {
    // Some Ollama versions / partial responses omit prompt_eval_count or
    // eval_count. The adapter must coerce to 0 so the CostUnits
    // invariant (non-negative integer) holds.
    const { client } = fakeClient(
      () =>
        ({
          model: "llama3.3",
          message: { role: "assistant", content: "ok" },
          done: true,
        }) as unknown as OllamaChatResponse,
    );
    const provider = createOllamaProvider({ client });
    const res = await provider.complete({
      model: "llama3.3",
      prompt: "p",
      maxOutputTokens: 8,
    });
    expect(res.usage).toEqual({
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    });
  });

  it("wraps SDK errors as ProviderError", async () => {
    // Typical Ollama error: server not running, model not pulled,
    // connection refused. We map all of them to ProviderError; there
    // is no reliable HTTP-status convention to peel off as BadRequest.
    const sdkErr = new Error("connect ECONNREFUSED 127.0.0.1:11434");
    const provider = createOllamaProvider({
      client: {
        chat: async () => {
          throw sdkErr;
        },
      },
    });
    const promise = provider.complete({
      model: "llama3.3",
      prompt: "p",
      maxOutputTokens: 8,
    });
    await expect(promise).rejects.toBeInstanceOf(ProviderError);
    await expect(promise).rejects.toMatchObject({
      providerKind: "ollama",
    });
  });

  it("rejects models not in the pricing table with UnknownModelError", async () => {
    const provider = createOllamaProvider({ client: fakeClient(() => emptyResponse()).client });
    await expect(
      provider.complete({ model: "llama-not-real", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toBeInstanceOf(UnknownModelError);
  });

  it("pre-rejects maxOutputTokens <= 0 without an SDK call", async () => {
    const { client, chat } = fakeClient(() => emptyResponse());
    const provider = createOllamaProvider({ client });
    await expect(
      provider.complete({ model: "llama3.3", prompt: "p", maxOutputTokens: 0 }),
    ).rejects.toBeInstanceOf(BadRequestError);
    expect(chat).not.toHaveBeenCalled();
  });

  it("pre-rejects fractional maxOutputTokens without an SDK call", async () => {
    const { client, chat } = fakeClient(() => emptyResponse());
    const provider = createOllamaProvider({ client });
    await expect(
      provider.complete({ model: "llama3.3", prompt: "p", maxOutputTokens: 8.5 }),
    ).rejects.toBeInstanceOf(BadRequestError);
    expect(chat).not.toHaveBeenCalled();
  });
});

describe("Ollama pricing-table validation", () => {
  it("rejects an empty pricing table at construction", () => {
    expect(() =>
      createOllamaProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {},
      }),
    ).toThrow(/at least one model/);
  });

  it("rejects a non-integer rate at construction", () => {
    expect(() =>
      createOllamaProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {
          "bad-model": {
            inputMicroUsdPerMTok: 1.5,
            outputMicroUsdPerMTok: 0,
            cacheReadMicroUsdPerMTok: 0,
            cacheCreationMicroUsdPerMTok: 0,
          },
        },
      }),
    ).toThrow(/non-negative safe integer/);
  });

  it("rejects a negative rate at construction", () => {
    expect(() =>
      createOllamaProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {
          "bad-model": {
            inputMicroUsdPerMTok: -1,
            outputMicroUsdPerMTok: 0,
            cacheReadMicroUsdPerMTok: 0,
            cacheCreationMicroUsdPerMTok: 0,
          },
        },
      }),
    ).toThrow(/non-negative safe integer/);
  });

  it("accepts a table with all-zero rates (the default shape)", () => {
    expect(() =>
      createOllamaProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {
          "all-zero": {
            inputMicroUsdPerMTok: 0,
            outputMicroUsdPerMTok: 0,
            cacheReadMicroUsdPerMTok: 0,
            cacheCreationMicroUsdPerMTok: 0,
          },
        },
      }),
    ).not.toThrow();
  });
});

describe("OllamaProvider → Gateway → CostLedger end-to-end (mocked SDK)", () => {
  it("writes a zero-amount cost_event row for a free local call", async () => {
    // Hard rule #1: every successful Gateway.complete() writes one
    // cost_event BEFORE returning. For Ollama that row's amount is 0
    // (local execution, no provider charge), but the row MUST exist
    // so cost_by_agent stays uniform across providers.
    const { client } = fakeClient(() => ({
      model: "llama3.3",
      message: { role: "assistant", content: "ok" },
      done: true,
      prompt_eval_count: 1_000,
      eval_count: 500,
    }));
    const provider = createOllamaProvider({ client });

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
        model: "llama3.3",
        prompt: "go",
        maxOutputTokens: 1_024,
      });
      // Free pricing → 0 μUSD spend, but the row was written:
      // costEventLsn is non-null (its presence is the proof) and
      // cost_by_agent reflects the zero entry as an existing day-row.
      expect(result.costMicroUsd as number).toBe(0);
      expect(result.costEventLsn).toBeDefined();
      const agentRow = ledger.getAgentSpend("primary", "2026-05-12");
      expect(agentRow?.totalMicroUsd as number).toBe(0);
    } finally {
      db.close();
    }
  });

  it("writes a non-zero cost_event row when host overrides pricing (GPU model)", async () => {
    // Host wants to track GPU cost: same end-to-end shape, just with
    // non-zero rates. Verifies the override path is wired the same way
    // as the other adapters.
    const { client } = fakeClient(() => ({
      model: "llama3.3-gpu",
      message: { role: "assistant", content: "ok" },
      done: true,
      prompt_eval_count: 1_000,
      eval_count: 500,
    }));
    const provider = createOllamaProvider({
      client,
      pricing: {
        "llama3.3-gpu": {
          inputMicroUsdPerMTok: 100_000,
          outputMicroUsdPerMTok: 200_000,
          cacheReadMicroUsdPerMTok: 0,
          cacheCreationMicroUsdPerMTok: 0,
        },
      },
    });

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
        model: "llama3.3-gpu",
        prompt: "go",
        maxOutputTokens: 1_024,
      });
      // (1000*100_000 + 500*200_000) / 1e6 = 200.
      expect(result.costMicroUsd as number).toBe(200);
      const agentRow = ledger.getAgentSpend("primary", "2026-05-12");
      expect(agentRow?.totalMicroUsd as number).toBe(200);
    } finally {
      db.close();
    }
  });
});

function emptyResponse(): OllamaChatResponse {
  return {
    model: "llama3.3",
    message: { role: "assistant", content: "" },
    done: true,
    prompt_eval_count: 0,
    eval_count: 0,
  };
}
