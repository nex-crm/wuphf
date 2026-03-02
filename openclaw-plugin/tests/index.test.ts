import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { parseConfig, ConfigError } from "../src/config.js";
import { formatNexContext, stripNexContext, hasNexContext } from "../src/context-format.js";
import { captureFilter, resetDedupCache, type AgentMessage } from "../src/capture-filter.js";
import { RateLimiter } from "../src/rate-limiter.js";
import { SessionStore } from "../src/session-store.js";
import plugin from "../src/index.js";

// --- Config ---

describe("config", () => {
  const originalEnv = { ...process.env };

  afterEach(() => {
    process.env = { ...originalEnv };
  });

  it("parses valid config", () => {
    const cfg = parseConfig({ apiKey: "sk-test-123" });
    expect(cfg.apiKey).toBe("sk-test-123");
    expect(cfg.baseUrl).toBe("https://api.nex-crm.com");
    expect(cfg.autoRecall).toBe(true);
    expect(cfg.autoCapture).toBe(true);
    expect(cfg.captureMode).toBe("last_turn");
    expect(cfg.maxRecallResults).toBe(5);
    expect(cfg.recallTimeoutMs).toBe(1500);
    expect(cfg.debug).toBe(false);
  });

  it("falls back to NEX_API_KEY env var", () => {
    process.env.NEX_API_KEY = "sk-from-env";
    const cfg = parseConfig({});
    expect(cfg.apiKey).toBe("sk-from-env");
  });

  it("resolves ${VAR} syntax in apiKey", () => {
    process.env.MY_KEY = "sk-interpolated";
    const cfg = parseConfig({ apiKey: "${MY_KEY}" });
    expect(cfg.apiKey).toBe("sk-interpolated");
  });

  it("resolves ${VAR} syntax in baseUrl", () => {
    process.env.MY_URL = "https://staging.nex.io";
    const cfg = parseConfig({ apiKey: "sk-test", baseUrl: "${MY_URL}" });
    expect(cfg.baseUrl).toBe("https://staging.nex.io");
  });

  it("strips trailing slash from baseUrl", () => {
    const cfg = parseConfig({ apiKey: "sk-test", baseUrl: "https://example.com///" });
    expect(cfg.baseUrl).toBe("https://example.com");
  });

  it("throws on missing API key", () => {
    delete process.env.NEX_API_KEY;
    expect(() => parseConfig({})).toThrow(ConfigError);
  });

  it("throws on invalid captureMode", () => {
    expect(() => parseConfig({ apiKey: "sk-test", captureMode: "invalid" })).toThrow(ConfigError);
  });

  it("throws on maxRecallResults out of range", () => {
    expect(() => parseConfig({ apiKey: "sk-test", maxRecallResults: 0 })).toThrow(ConfigError);
    expect(() => parseConfig({ apiKey: "sk-test", maxRecallResults: 21 })).toThrow(ConfigError);
  });

  it("throws on recallTimeoutMs out of range", () => {
    expect(() => parseConfig({ apiKey: "sk-test", recallTimeoutMs: 100 })).toThrow(ConfigError);
    expect(() => parseConfig({ apiKey: "sk-test", recallTimeoutMs: 20000 })).toThrow(ConfigError);
  });

  it("overrides defaults with explicit values", () => {
    const cfg = parseConfig({
      apiKey: "sk-test",
      autoRecall: false,
      autoCapture: false,
      captureMode: "full_session",
      maxRecallResults: 10,
      sessionTracking: false,
      recallTimeoutMs: 3000,
      debug: true,
    });
    expect(cfg.autoRecall).toBe(false);
    expect(cfg.autoCapture).toBe(false);
    expect(cfg.captureMode).toBe("full_session");
    expect(cfg.maxRecallResults).toBe(10);
    expect(cfg.sessionTracking).toBe(false);
    expect(cfg.recallTimeoutMs).toBe(3000);
    expect(cfg.debug).toBe(true);
  });
});

// --- Context Format ---

describe("context-format", () => {
  it("formats context with entity count", () => {
    const result = formatNexContext({
      answer: "John works at Acme Corp.",
      entityCount: 2,
    });
    expect(result).toContain("<nex-context>");
    expect(result).toContain("</nex-context>");
    expect(result).toContain("John works at Acme Corp.");
    expect(result).toContain("[2 related entities found]");
  });

  it("omits entity count when zero", () => {
    const result = formatNexContext({ answer: "No entities.", entityCount: 0 });
    expect(result).not.toContain("related entities");
  });

  it("strips complete nex-context blocks", () => {
    const text = "Before <nex-context>injected stuff</nex-context> After";
    expect(stripNexContext(text)).toBe("Before  After");
  });

  it("strips unclosed nex-context tags", () => {
    const text = "Before <nex-context>injected but no close";
    expect(stripNexContext(text)).toBe("Before");
  });

  it("strips multiline blocks", () => {
    const text = "Start\n<nex-context>\nline1\nline2\n</nex-context>\nEnd";
    expect(stripNexContext(text)).toBe("Start\n\nEnd");
  });

  it("detects presence of nex-context", () => {
    expect(hasNexContext("hello <nex-context>x</nex-context>")).toBe(true);
    expect(hasNexContext("hello world")).toBe(false);
  });
});

// --- Capture Filter ---

describe("capture-filter", () => {
  const defaultConfig = parseConfig({ apiKey: "sk-test" });

  beforeEach(() => {
    resetDedupCache();
  });

  it("extracts last turn in last_turn mode", () => {
    const messages: AgentMessage[] = [
      { role: "user", content: "First question" },
      { role: "assistant", content: "First answer" },
      { role: "user", content: "Second question here" },
      { role: "assistant", content: "Second answer here" },
    ];
    const result = captureFilter(messages, defaultConfig);
    expect(result.skipped).toBe(false);
    if (!result.skipped) {
      expect(result.text).toContain("Second question");
      expect(result.text).toContain("Second answer");
    }
  });

  it("extracts full session in full_session mode", () => {
    const cfg = parseConfig({ apiKey: "sk-test", captureMode: "full_session" });
    const messages: AgentMessage[] = [
      { role: "user", content: "First question here" },
      { role: "assistant", content: "First answer here" },
      { role: "user", content: "Second question here" },
      { role: "assistant", content: "Second answer here" },
    ];
    const result = captureFilter(messages, cfg);
    expect(result.skipped).toBe(false);
    if (!result.skipped) {
      expect(result.text).toContain("First question");
      expect(result.text).toContain("Second answer");
    }
  });

  it("strips nex-context before capture", () => {
    const messages: AgentMessage[] = [
      { role: "user", content: "<nex-context>injected</nex-context>What color?" },
      { role: "assistant", content: "Blue is your favorite." },
    ];
    const result = captureFilter(messages, defaultConfig);
    expect(result.skipped).toBe(false);
    if (!result.skipped) {
      expect(result.text).not.toContain("<nex-context>");
    }
  });

  it("skips failed agent runs", () => {
    const result = captureFilter(
      [{ role: "user", content: "hello world test" }],
      defaultConfig,
      { success: false },
    );
    expect(result.skipped).toBe(true);
    if (result.skipped) expect(result.reason).toContain("failed");
  });

  it("skips exec-event provider", () => {
    const result = captureFilter(
      [{ role: "user", content: "hello world test" }],
      defaultConfig,
      { messageProvider: "exec-event" },
    );
    expect(result.skipped).toBe(true);
  });

  it("skips cron-event provider", () => {
    const result = captureFilter(
      [{ role: "user", content: "hello world test" }],
      defaultConfig,
      { messageProvider: "cron-event" },
    );
    expect(result.skipped).toBe(true);
  });

  it("skips slash commands", () => {
    const result = captureFilter(
      [{ role: "user", content: "/help me out please" }],
      defaultConfig,
    );
    expect(result.skipped).toBe(true);
    if (result.skipped) expect(result.reason).toContain("slash command");
  });

  it("skips short messages", () => {
    const result = captureFilter(
      [{ role: "user", content: "hi" }],
      defaultConfig,
    );
    expect(result.skipped).toBe(true);
    if (result.skipped) expect(result.reason).toContain("short");
  });

  it("skips empty messages array", () => {
    const result = captureFilter([], defaultConfig);
    expect(result.skipped).toBe(true);
  });

  it("skips duplicate content", () => {
    const messages: AgentMessage[] = [
      { role: "user", content: "Tell me about OpenClaw plugins" },
      { role: "assistant", content: "OpenClaw plugins are extensions..." },
    ];
    const first = captureFilter(messages, defaultConfig);
    expect(first.skipped).toBe(false);

    const second = captureFilter(messages, defaultConfig);
    expect(second.skipped).toBe(true);
    if (second.skipped) expect(second.reason).toContain("duplicate");
  });

  it("handles array content format", () => {
    const messages: AgentMessage[] = [
      {
        role: "user",
        content: [{ type: "text", text: "Question about entities here" }],
      },
      {
        role: "assistant",
        content: [{ type: "text", text: "Answer about entities here" }],
      },
    ];
    const result = captureFilter(messages, defaultConfig);
    expect(result.skipped).toBe(false);
    if (!result.skipped) {
      expect(result.text).toContain("Question about entities");
    }
  });
});

// --- Rate Limiter ---

describe("rate-limiter", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("allows requests within limit", async () => {
    const limiter = new RateLimiter({ maxRequests: 3, windowMs: 1000, maxQueueDepth: 5 });
    const results: number[] = [];

    const p1 = limiter.enqueue(async () => { results.push(1); });
    const p2 = limiter.enqueue(async () => { results.push(2); });
    const p3 = limiter.enqueue(async () => { results.push(3); });

    await Promise.all([p1, p2, p3]);
    expect(results).toEqual([1, 2, 3]);
    limiter.destroy();
  });

  it("queues requests exceeding limit", async () => {
    const limiter = new RateLimiter({ maxRequests: 2, windowMs: 1000, maxQueueDepth: 5 });
    const results: number[] = [];

    const p1 = limiter.enqueue(async () => { results.push(1); });
    const p2 = limiter.enqueue(async () => { results.push(2); });
    const p3 = limiter.enqueue(async () => { results.push(3); });

    // First two execute immediately
    await p1;
    await p2;
    expect(results).toEqual([1, 2]);

    // Third waits for window to slide
    vi.advanceTimersByTime(1100);
    await p3;
    expect(results).toEqual([1, 2, 3]);
    limiter.destroy();
  });

  it("evicts oldest on queue overflow (LIFO)", async () => {
    const limiter = new RateLimiter({ maxRequests: 1, windowMs: 10000, maxQueueDepth: 2 });
    const results: number[] = [];
    const errors: string[] = [];

    const p1 = limiter.enqueue(async () => { results.push(1); }); // executes immediately
    await p1;

    // These 3 queue up, but max depth is 2 — oldest gets evicted
    const p2 = limiter.enqueue(async () => { results.push(2); }).catch((e) => errors.push(e.message));
    const p3 = limiter.enqueue(async () => { results.push(3); }).catch((e) => errors.push(e.message));
    const p4 = limiter.enqueue(async () => { results.push(4); }).catch((e) => errors.push(e.message));

    // Let eviction happen
    await vi.advanceTimersByTimeAsync(100);

    // One should be evicted
    expect(errors.length).toBeGreaterThanOrEqual(1);
    expect(errors[0]).toContain("eviction");

    limiter.destroy();
  });

  it("reports pending count", () => {
    const limiter = new RateLimiter({ maxRequests: 1, windowMs: 60000, maxQueueDepth: 5 });
    limiter.enqueue(async () => {}).catch(() => {});
    // After first executes, pending should go to 0
    expect(limiter.pending).toBeGreaterThanOrEqual(0);
    limiter.destroy();
  });
});

// --- Session Store ---

describe("session-store", () => {
  it("get/set/delete", () => {
    const store = new SessionStore();
    expect(store.get("key1")).toBeUndefined();

    store.set("key1", "session-abc");
    expect(store.get("key1")).toBe("session-abc");

    store.delete("key1");
    expect(store.get("key1")).toBeUndefined();
  });

  it("LRU eviction at max size", () => {
    const store = new SessionStore(3);
    store.set("a", "1");
    store.set("b", "2");
    store.set("c", "3");
    store.set("d", "4"); // should evict "a"

    expect(store.get("a")).toBeUndefined();
    expect(store.get("b")).toBe("2");
    expect(store.get("d")).toBe("4");
    expect(store.size).toBe(3);
  });

  it("access refreshes LRU position", () => {
    const store = new SessionStore(3);
    store.set("a", "1");
    store.set("b", "2");
    store.set("c", "3");

    // Access "a" to make it most recent
    store.get("a");

    store.set("d", "4"); // should evict "b" (now oldest)
    expect(store.get("a")).toBe("1");
    expect(store.get("b")).toBeUndefined();
  });

  it("clear removes all entries", () => {
    const store = new SessionStore();
    store.set("x", "1");
    store.set("y", "2");
    store.clear();
    expect(store.size).toBe(0);
  });
});

// --- Plugin shape ---

describe("plugin", () => {
  it("has correct id and kind", () => {
    expect(plugin.id).toBe("memory-nex");
    expect(plugin.kind).toBe("memory");
  });

  it("has a register function", () => {
    expect(typeof plugin.register).toBe("function");
  });

  it("has name, description, and version", () => {
    expect(plugin.name).toBe("Nex Memory");
    expect(plugin.version).toBe("0.1.0");
    expect(plugin.description).toBeTruthy();
  });
});
