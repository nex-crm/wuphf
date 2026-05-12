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
  type SupervisorContext,
  UnknownModelError,
} from "../../src/index.ts";
import {
  createOpenCodeGoProvider,
  createOpenCodeHttpClient,
  createOpenCodeProvider,
  DEFAULT_OPENCODE_PRICING,
  DEFAULT_OPENCODEGO_PRICING,
  estimateOpenCodeCostMicroUsd,
  type OpenCodeChatRequest,
  type OpenCodeChatResponse,
  type OpenCodeClient,
} from "../../src/providers/opencode.ts";

function fakeClient(stub: (req: OpenCodeChatRequest) => OpenCodeChatResponse): {
  readonly client: OpenCodeClient;
  readonly chat: ReturnType<typeof vi.fn>;
} {
  const chat = vi.fn(
    async (req: OpenCodeChatRequest, _options?: { readonly headers?: Record<string, string> }) =>
      Promise.resolve(stub(req)),
  );
  return { client: { chat }, chat };
}

const PRIMARY_CTX: SupervisorContext = { agentSlug: asAgentSlug("primary") };

describe("opencode pricing (zero-default + host-override)", () => {
  it("returns 0 μUSD for default opencode rates regardless of token counts", () => {
    const cost = estimateOpenCodeCostMicroUsd(DEFAULT_OPENCODE_PRICING, "opencode-default", {
      inputTokens: 1_000_000,
      outputTokens: 500_000,
      cacheReadTokens: 100_000,
      cacheCreationTokens: 0,
    });
    expect(cost as number).toBe(0);
  });

  it("returns 0 μUSD for every default-table model in opencode", () => {
    for (const model of Object.keys(DEFAULT_OPENCODE_PRICING)) {
      const cost = estimateOpenCodeCostMicroUsd(DEFAULT_OPENCODE_PRICING, model, {
        inputTokens: 1_000_000,
        outputTokens: 1_000_000,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      });
      expect(cost as number).toBe(0);
    }
  });

  it("opencodego has its own default registry — distinct from opencode", () => {
    expect(Object.keys(DEFAULT_OPENCODEGO_PRICING)).toContain("opencodego-default");
    for (const k of Object.keys(DEFAULT_OPENCODE_PRICING)) {
      expect(Object.keys(DEFAULT_OPENCODEGO_PRICING)).not.toContain(k);
    }
  });

  it("respects a host-supplied pricing override (round-half-up integer math)", () => {
    const cost = estimateOpenCodeCostMicroUsd(
      {
        "opencode-sonnet": {
          inputMicroUsdPerMTok: 3_000_000,
          outputMicroUsdPerMTok: 15_000_000,
          cachedInputMicroUsdPerMTok: 300_000,
        },
      },
      "opencode-sonnet",
      { inputTokens: 1_000, outputTokens: 500, cacheReadTokens: 0, cacheCreationTokens: 0 },
    );
    // 1000 * 3_000_000 + 500 * 15_000_000 = 1.05e10 ; /1e6 = 10_500.
    expect(cost as number).toBe(10_500);
  });

  it("throws UnknownModelError for unrecognized models", () => {
    expect(() =>
      estimateOpenCodeCostMicroUsd(DEFAULT_OPENCODE_PRICING, "opencode-not-real", {
        inputTokens: 1,
        outputTokens: 1,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      }),
    ).toThrow(UnknownModelError);
  });
});

describe("OpenCodeProvider (TS) and OpenCodeGoProvider (Go) — separate audit identities", () => {
  it('opencode reports kind="opencode"', () => {
    const provider = createOpenCodeProvider({ client: fakeClient(() => emptyResponse()).client });
    expect(provider.kind).toBe("opencode");
    expect(provider.models).toContain("opencode-default");
  });

  it('opencodego reports kind="opencodego" with its own model set', () => {
    const provider = createOpenCodeGoProvider({
      client: fakeClient(() => emptyResponse()).client,
    });
    expect(provider.kind).toBe("opencodego");
    expect(provider.models).toContain("opencodego-default");
    expect(provider.models).not.toContain("opencode-default");
  });
});

describe("OpenCodeProvider request translation", () => {
  it("translates ProviderRequest → OpenCodeChatRequest and forwards Idempotency-Key", async () => {
    const { client, chat } = fakeClient(() => ({
      text: "hi there",
      usage: { inputTokens: 50, outputTokens: 20 },
      finishReason: "stop",
    }));
    const provider = createOpenCodeProvider({ client });

    const res = await provider.complete({
      model: "opencode-default",
      prompt: "hello",
      maxOutputTokens: 64,
      requestKey: "ctx-bound-hash",
    });

    expect(chat).toHaveBeenCalledOnce();
    expect(chat).toHaveBeenCalledWith(
      { model: "opencode-default", prompt: "hello", maxOutputTokens: 64 },
      expect.objectContaining({
        headers: expect.objectContaining({ "Idempotency-Key": "wuphf-ctx-bound-hash" }),
      }),
    );
    expect(res.text).toBe("hi there");
    expect(res.finishReason).toBe("stop");
  });

  it("derives a deterministic idempotency key when ctx.requestKey is absent", async () => {
    const { client, chat } = fakeClient(() => emptyResponse());
    const provider = createOpenCodeProvider({ client });
    await provider.complete({ model: "opencode-default", prompt: "p", maxOutputTokens: 8 });
    await provider.complete({ model: "opencode-default", prompt: "p", maxOutputTokens: 8 });
    const firstKey = (chat.mock.calls[0]?.[1] as { headers: Record<string, string> }).headers[
      "Idempotency-Key"
    ];
    const secondKey = (chat.mock.calls[1]?.[1] as { headers: Record<string, string> }).headers[
      "Idempotency-Key"
    ];
    expect(firstKey).toBe(secondKey);
    expect(firstKey?.startsWith("wuphf-")).toBe(true);
  });
});

describe("OpenCodeProvider refusal + usage edge cases", () => {
  it("routes refusal into the refusal field — does NOT fold prose into text", async () => {
    const { client } = fakeClient(() => ({
      text: "redacted",
      usage: { inputTokens: 5, outputTokens: 5 },
      finishReason: "refusal",
      refusal: "policy: cannot help",
    }));
    const provider = createOpenCodeProvider({ client });
    const res = await provider.complete({
      model: "opencode-default",
      prompt: "p",
      maxOutputTokens: 8,
    });
    expect(res.text).toBe("");
    expect(res.refusal).toBe("policy: cannot help");
  });

  it("clamps cachedInputTokens to inputTokens (B3-6 contract)", async () => {
    const { client } = fakeClient(() => ({
      text: "ok",
      usage: { inputTokens: 100, outputTokens: 5, cachedInputTokens: 9_999 },
    }));
    const provider = createOpenCodeProvider({ client });
    const res = await provider.complete({
      model: "opencode-default",
      prompt: "p",
      maxOutputTokens: 8,
    });
    expect(res.usage.cacheReadTokens).toBe(100);
    expect(res.usage.inputTokens).toBe(0);
  });

  it("classifies negative usage counters as ProviderError (not silent)", async () => {
    const { client } = fakeClient(() => ({
      text: "ok",
      usage: { inputTokens: -1, outputTokens: 0 },
    }));
    const provider = createOpenCodeProvider({ client });
    await expect(
      provider.complete({ model: "opencode-default", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toBeInstanceOf(ProviderError);
  });

  it("classifies fractional usage counters as ProviderError", async () => {
    const { client } = fakeClient(() => ({
      text: "ok",
      usage: { inputTokens: 1.5, outputTokens: 0 },
    }));
    const provider = createOpenCodeProvider({ client });
    await expect(
      provider.complete({ model: "opencode-default", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toBeInstanceOf(ProviderError);
  });
});

describe("OpenCodeProvider pre-validation + error classification", () => {
  it("pre-rejects maxOutputTokens <= 0 without a transport call", async () => {
    const { client, chat } = fakeClient(() => emptyResponse());
    const provider = createOpenCodeProvider({ client });
    await expect(
      provider.complete({ model: "opencode-default", prompt: "p", maxOutputTokens: 0 }),
    ).rejects.toBeInstanceOf(BadRequestError);
    expect(chat).not.toHaveBeenCalled();
  });

  it("pre-rejects fractional maxOutputTokens without a transport call", async () => {
    const { client, chat } = fakeClient(() => emptyResponse());
    const provider = createOpenCodeProvider({ client });
    await expect(
      provider.complete({ model: "opencode-default", prompt: "p", maxOutputTokens: 8.5 }),
    ).rejects.toBeInstanceOf(BadRequestError);
    expect(chat).not.toHaveBeenCalled();
  });

  it("rejects models not in the pricing table with UnknownModelError", async () => {
    const provider = createOpenCodeProvider({ client: fakeClient(() => emptyResponse()).client });
    await expect(
      provider.complete({ model: "opencode-not-registered", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toBeInstanceOf(UnknownModelError);
  });

  it("wraps transport errors as ProviderError (breaker-eligible)", async () => {
    const provider = createOpenCodeProvider({
      client: {
        chat: async () => {
          throw new Error("ECONNREFUSED");
        },
      },
    });
    const promise = provider.complete({
      model: "opencode-default",
      prompt: "p",
      maxOutputTokens: 8,
    });
    await expect(promise).rejects.toBeInstanceOf(ProviderError);
    await expect(promise).rejects.toMatchObject({ providerKind: "opencode" });
  });

  it('opencodego transport errors carry providerKind="opencodego"', async () => {
    const provider = createOpenCodeGoProvider({
      client: {
        chat: async () => {
          throw new Error("ECONNREFUSED");
        },
      },
    });
    await expect(
      provider.complete({ model: "opencodego-default", prompt: "p", maxOutputTokens: 8 }),
    ).rejects.toMatchObject({ providerKind: "opencodego" });
  });
});

describe("OpenCode pricing-table validation", () => {
  it("rejects an empty pricing table at construction", () => {
    expect(() =>
      createOpenCodeProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {},
      }),
    ).toThrow(/at least one model/);
  });

  it("rejects a negative rate at construction", () => {
    expect(() =>
      createOpenCodeProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {
          "bad-model": {
            inputMicroUsdPerMTok: -1,
            outputMicroUsdPerMTok: 1,
          },
        },
      }),
    ).toThrow(/non-negative safe integer/);
  });

  it("rejects a non-integer rate at construction", () => {
    expect(() =>
      createOpenCodeProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {
          "bad-model": {
            inputMicroUsdPerMTok: 1.5,
            outputMicroUsdPerMTok: 1,
          },
        },
      }),
    ).toThrow(/non-negative safe integer/);
  });

  it("accepts the all-zero default shape", () => {
    expect(() =>
      createOpenCodeProvider({
        client: fakeClient(() => emptyResponse()).client,
        pricing: {
          "all-zero": { inputMicroUsdPerMTok: 0, outputMicroUsdPerMTok: 0 },
        },
      }),
    ).not.toThrow();
  });
});

describe("createOpenCodeHttpClient", () => {
  it("POSTs to baseUrl/chat, merges headers, and parses the JSON response", async () => {
    let captured: { url: string; init: RequestInit } | null = null;
    const fetchFn = (async (
      input: Request | URL | string,
      init?: RequestInit,
    ): Promise<Response> => {
      captured = { url: String(input), init: init ?? {} };
      return new Response(
        JSON.stringify({
          text: "from http",
          usage: { inputTokens: 10, outputTokens: 5 },
          finishReason: "stop",
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      );
    }) as typeof globalThis.fetch;

    const client = createOpenCodeHttpClient({
      baseUrl: "http://localhost:9100",
      headers: { authorization: "Bearer test" },
      fetchFn,
    });
    const res = await client.chat(
      { model: "opencode-default", prompt: "p", maxOutputTokens: 16 },
      { headers: { "Idempotency-Key": "wuphf-abc" } },
    );

    expect(res.text).toBe("from http");
    expect(captured).not.toBeNull();
    const ref = captured as unknown as { url: string; init: RequestInit };
    expect(ref.url).toBe("http://localhost:9100/chat");
    const headers = ref.init.headers as {
      readonly authorization?: string;
      readonly "Idempotency-Key"?: string;
      readonly "Content-Type"?: string;
    };
    expect(headers.authorization).toBe("Bearer test");
    expect(headers["Idempotency-Key"]).toBe("wuphf-abc");
    expect(headers["Content-Type"]).toBe("application/json");
  });

  it("trims trailing slash from baseUrl", async () => {
    let capturedUrl = "";
    const fetchFn = (async (input: Request | URL | string): Promise<Response> => {
      capturedUrl = String(input);
      return new Response(
        JSON.stringify({ text: "", usage: { inputTokens: 0, outputTokens: 0 } }),
        { status: 200 },
      );
    }) as typeof globalThis.fetch;
    const client = createOpenCodeHttpClient({ baseUrl: "http://h/", fetchFn });
    await client.chat({ model: "opencode-default", prompt: "p", maxOutputTokens: 1 });
    expect(capturedUrl).toBe("http://h/chat");
  });

  it("requires baseUrl", () => {
    expect(() => createOpenCodeHttpClient({ baseUrl: "" })).toThrow(/baseUrl required/);
  });

  it("surfaces non-2xx HTTP as an Error with attached status", async () => {
    const fetchFn = (async (): Promise<Response> =>
      new Response("upstream failure", { status: 500 })) as typeof globalThis.fetch;
    const client = createOpenCodeHttpClient({ baseUrl: "http://h", fetchFn });
    await expect(
      client.chat({ model: "opencode-default", prompt: "p", maxOutputTokens: 1 }),
    ).rejects.toMatchObject({ status: 500 });
  });
});

describe("OpenCodeProvider → Gateway → CostLedger end-to-end (mocked transport)", () => {
  it("writes a zero-amount cost_event row for a default (free-default) opencode call", async () => {
    // Hard rule #1: every successful Gateway.complete() writes one
    // cost_event BEFORE returning. The row exists even when amount = 0
    // so cost_by_agent stays uniform across providers.
    const { client } = fakeClient(() => ({
      text: "ok",
      usage: { inputTokens: 1_000, outputTokens: 100 },
    }));
    const provider = createOpenCodeProvider({ client });
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
      const result = await gateway.complete(PRIMARY_CTX, {
        model: "opencode-default",
        prompt: "hi",
        maxOutputTokens: 128,
      });
      expect(result.costMicroUsd as number).toBe(0);
      expect(result.costEventLsn).toBeDefined();
      const agentRow = ledger.getAgentSpend("primary", "2026-05-12");
      expect(agentRow?.totalMicroUsd as number).toBe(0);
    } finally {
      db.close();
    }
  });

  it("writes non-zero spend when host overrides pricing for the backing model", async () => {
    const { client } = fakeClient(() => ({
      text: "ok",
      usage: { inputTokens: 1_000, outputTokens: 500 },
    }));
    const provider = createOpenCodeProvider({
      client,
      pricing: {
        "opencode-sonnet": {
          inputMicroUsdPerMTok: 3_000_000,
          outputMicroUsdPerMTok: 15_000_000,
          cachedInputMicroUsdPerMTok: 300_000,
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
      const result = await gateway.complete(PRIMARY_CTX, {
        model: "opencode-sonnet",
        prompt: "hi",
        maxOutputTokens: 128,
      });
      // 1000*3e6 + 500*15e6 = 1.05e10 ; /1e6 = 10_500.
      expect(result.costMicroUsd as number).toBe(10_500);
      const agentRow = ledger.getAgentSpend("primary", "2026-05-12");
      expect(agentRow?.totalMicroUsd as number).toBe(10_500);
    } finally {
      db.close();
    }
  });

  it("opencode and opencodego accumulate spend under separate agent rows", async () => {
    const ts = fakeClient(() => ({ text: "ts", usage: { inputTokens: 1, outputTokens: 1 } }));
    const go = fakeClient(() => ({ text: "go", usage: { inputTokens: 1, outputTokens: 1 } }));
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const clock = { now: new Date("2026-05-12T10:00:00.000Z").getTime() };
    const gateway = createGateway({
      ledger,
      providers: [
        createOpenCodeProvider({ client: ts.client }),
        createOpenCodeGoProvider({ client: go.client }),
      ],
      nowMs: () => clock.now,
    });
    try {
      const ctxA: SupervisorContext = { agentSlug: asAgentSlug("a") };
      const ctxB: SupervisorContext = { agentSlug: asAgentSlug("b") };
      await gateway.complete(ctxA, {
        model: "opencode-default",
        prompt: "p1",
        maxOutputTokens: 8,
      });
      await gateway.complete(ctxB, {
        model: "opencodego-default",
        prompt: "p2",
        maxOutputTokens: 8,
      });
      const rowA = ledger.getAgentSpend("a", "2026-05-12");
      const rowB = ledger.getAgentSpend("b", "2026-05-12");
      expect(rowA).not.toBeNull();
      expect(rowB).not.toBeNull();
      expect(rowA?.totalMicroUsd as number).toBe(0);
      expect(rowB?.totalMicroUsd as number).toBe(0);
    } finally {
      db.close();
    }
  });
});

function emptyResponse(): OpenCodeChatResponse {
  return { text: "", usage: { inputTokens: 0, outputTokens: 0 } };
}
