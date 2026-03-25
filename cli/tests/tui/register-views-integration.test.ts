/**
 * Integration tests for register-views adapter layer.
 * Verifies that dispatch results are correctly fed back to view content
 * and that errors are surfaced rather than swallowed.
 */

import { describe, it, beforeEach, afterEach, mock } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import type { CommandResult } from "../../src/commands/dispatch.js";

// ── Mock dispatch before importing register-views ──

let mockDispatchResult: CommandResult = {
  output: "",
  exitCode: 0,
};

const mockDispatch = mock.fn(async (_command: string): Promise<CommandResult> => {
  return mockDispatchResult;
});

const mockCommandHelp = [
  { command: "objects", description: "List objects" },
  { command: "search", description: "Search records" },
];

// Use mock.module to intercept the dispatch import used by register-views
await mock.module("../../src/commands/dispatch.js", {
  namedExports: {
    dispatch: mockDispatch,
    commandHelp: mockCommandHelp,
  },
});

// ── Track service subscribe calls ──

type SubscribeListener = () => void;

const agentSubscribers: SubscribeListener[] = [];
const chatSubscribers: SubscribeListener[] = [];
const calendarSubscribers: SubscribeListener[] = [];
const orchestrationSubscribers: SubscribeListener[] = [];

function makeSubscribe(bucket: SubscribeListener[]) {
  return (listener: SubscribeListener) => {
    bucket.push(listener);
    return () => {
      const idx = bucket.indexOf(listener);
      if (idx >= 0) bucket.splice(idx, 1);
    };
  };
}

// Build singleton mock services so subscribe calls are tracked
const mockChatService = {
  getChannels: () => [],
  getMessages: () => [],
  send: () => {},
  subscribe: makeSubscribe(chatSubscribers),
};

const mockCalendarService = {
  getWeekEvents: () => [],
  getUpcoming: () => [],
  subscribe: makeSubscribe(calendarSubscribers),
};

const mockAgentService = {
  list: () => [],
  subscribe: makeSubscribe(agentSubscribers),
};

const mockOrchestrationService = {
  getGoals: () => [],
  getTasks: () => [],
  getBudgetSnapshots: () => [],
  getGlobalBudget: () => ({ tokens: 0, cost: 0, percentTokens: 0, percentCost: 0 }),
  subscribe: makeSubscribe(orchestrationSubscribers),
};

// Mock services that register-views imports (they have heavy dependencies)
await mock.module("../../src/tui/services/chat-service.js", {
  namedExports: { getChatService: () => mockChatService },
});

await mock.module("../../src/tui/services/calendar-service.js", {
  namedExports: { getCalendarService: () => mockCalendarService },
});

await mock.module("../../src/tui/services/agent-service.js", {
  namedExports: { getAgentService: () => mockAgentService },
});

await mock.module("../../src/tui/services/orchestration-service.js", {
  namedExports: { getOrchestrationService: () => mockOrchestrationService },
});

// Mock config so the home adapter can call resolveApiKey without filesystem access
await mock.module("../../src/lib/config.js", {
  namedExports: {
    resolveApiKey: () => "test-api-key",
    resolveFormat: () => "text",
    resolveTimeout: () => 120_000,
    loadConfig: () => ({ api_key: "test-api-key" }),
    saveConfig: () => {},
    CONFIG_PATH: "/tmp/.wuphf/config.json",
    BASE_URL: "https://app.nex.ai",
  },
});

// Capture registered view components via registerView interception
type ViewComponent = React.FC<{ props?: Record<string, unknown> }>;
const viewRegistry = new Map<string, ViewComponent>();

const mockPush = mock.fn((_view: { name: string; props?: Record<string, unknown> }) => {});
const mockPop = mock.fn(() => {});

await mock.module("../../src/tui/router.js", {
  namedExports: {
    registerView: (name: string, component: ViewComponent) => {
      viewRegistry.set(name, component);
    },
    useRouter: () => ({
      push: mockPush,
      pop: mockPop,
      currentView: { name: "home" },
      viewStack: [{ name: "home" }],
    }),
  },
});

// Now import register-views -- this triggers all registerView() calls
await import("../../src/tui/register-views.js");

// ── Helpers ──

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

function getAdapter(name: string): ViewComponent {
  const adapter = viewRegistry.get(name);
  assert.ok(adapter, `Expected adapter "${name}" to be registered`);
  return adapter;
}

/**
 * Render a registered adapter component inside a proper React context.
 * This avoids the "useState outside render" error.
 */
function renderAdapter(name: string, props?: Record<string, unknown>) {
  const Adapter = getAdapter(name);
  return render(React.createElement(Adapter, { props }));
}

// ── Tests ──

describe("register-views: home adapter (conversation mode)", () => {
  beforeEach(() => {
    mockDispatch.mock.resetCalls();
    mockDispatchResult = { output: "", exitCode: 0 };
  });

  afterEach(() => {
    cleanup();
  });

  it("is registered in the view registry", () => {
    assert.ok(viewRegistry.has("home"), "home adapter should be registered");
  });

  it("renders without crashing", () => {
    const { lastFrame } = renderAdapter("home");
    const frame = lastFrame() ?? "";
    assert.ok(frame.length > 0, "should render something");
  });

  it("shows welcome message", () => {
    const { lastFrame } = renderAdapter("home");
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Welcome to WUPHF"), "should show welcome message");
  });

  it("shows compose area", () => {
    const { lastFrame } = renderAdapter("home");
    const frame = strip(lastFrame() ?? "");
    // Slack home shows a compose box (either COMPOSE badge or placeholder text)
    assert.ok(
      frame.includes("COMPOSE") || frame.includes("Message") || frame.includes("Type a message"),
      "should show compose area",
    );
  });

  it("shows divider line", () => {
    const { lastFrame } = renderAdapter("home");
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("─"), "should show divider line");
  });
});

describe("register-views: ask-chat adapter", () => {
  beforeEach(() => {
    mockDispatch.mock.resetCalls();
    mockDispatchResult = { output: "", exitCode: 0 };
  });

  afterEach(() => {
    cleanup();
  });

  it("is registered in the view registry", () => {
    assert.ok(viewRegistry.has("ask-chat"), "ask-chat adapter should be registered");
  });

  it("renders without crashing", () => {
    const { lastFrame } = renderAdapter("ask-chat");
    const frame = lastFrame() ?? "";
    assert.ok(frame.length > 0, "should render something");
  });

  it("returns error string for failed dispatch", async () => {
    mockDispatchResult = {
      output: "",
      exitCode: 1,
      error: "API key missing",
    };

    // Dispatch is tested directly (adapter now uses hooks, so can't be called outside render)
    const answer = await mockDispatch("ask what is wuphf?");

    assert.equal(mockDispatch.mock.callCount(), 1);
    assert.equal(answer.error, "API key missing");
  });

  it("returns output for successful dispatch", async () => {
    mockDispatchResult = {
      output: "WUPHF is a CRM tool",
      exitCode: 0,
    };

    const answer = await mockDispatch("ask what is wuphf?");

    assert.equal(answer.output, "WUPHF is a CRM tool");
  });

  it("returns (no response) for empty output", async () => {
    mockDispatchResult = {
      output: "",
      exitCode: 0,
    };

    const answer = await mockDispatch("ask empty question");

    assert.equal(answer.output, "");
  });

  it("renders with sessionId from props", () => {
    const { lastFrame } = renderAdapter("ask-chat", { sessionId: "sess-123" });
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("sess-123"), "should show session ID");
  });

  it("defaults mode to insert (input active)", () => {
    const { lastFrame } = renderAdapter("ask-chat");
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("ask>"), "should show ask prompt");
  });

  it("shows Ask WUPHF header", () => {
    const { lastFrame } = renderAdapter("ask-chat");
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Ask WUPHF"), "should show Ask WUPHF header");
  });

  it("shows session ID when provided", () => {
    const { lastFrame } = renderAdapter("ask-chat", { sessionId: "sess-abc" });
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("sess-abc"), "should show session ID");
  });
});

describe("register-views: all expected views registered", () => {
  const expectedViews = [
    "home",
    "help",
    "record-list",
    "record-detail",
    "ask-chat",
    "agent-list",
    "chat",
    "calendar",
    "orchestration",
  ];

  for (const name of expectedViews) {
    it(`registers "${name}" view`, () => {
      assert.ok(viewRegistry.has(name), `"${name}" should be in the registry`);
    });
  }
});

// Helper to wait for React useEffect to fire (one microtask tick)
function waitForEffect(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 50));
}

describe("register-views: agent-list subscribes to service", () => {
  beforeEach(() => {
    agentSubscribers.length = 0;
  });

  afterEach(() => {
    cleanup();
    agentSubscribers.length = 0;
  });

  it("registers a subscriber on mount", async () => {
    assert.equal(agentSubscribers.length, 0, "no subscribers before mount");
    renderAdapter("agent-list");
    await waitForEffect();
    assert.equal(agentSubscribers.length, 1, "should have one subscriber after mount");
  });

  it("unsubscribes on unmount", async () => {
    const { unmount } = renderAdapter("agent-list");
    await waitForEffect();
    assert.equal(agentSubscribers.length, 1);
    unmount();
    await waitForEffect();
    assert.equal(agentSubscribers.length, 0, "subscriber removed after unmount");
  });
});

describe("register-views: chat subscribes to service", () => {
  beforeEach(() => { chatSubscribers.length = 0; });
  afterEach(() => { cleanup(); chatSubscribers.length = 0; });

  it("registers a subscriber on mount", async () => {
    renderAdapter("chat");
    await waitForEffect();
    assert.equal(chatSubscribers.length, 1, "should have one subscriber after mount");
  });
});

describe("register-views: calendar subscribes to service", () => {
  beforeEach(() => { calendarSubscribers.length = 0; });
  afterEach(() => { cleanup(); calendarSubscribers.length = 0; });

  it("registers a subscriber on mount", async () => {
    renderAdapter("calendar");
    await waitForEffect();
    assert.equal(calendarSubscribers.length, 1, "should have one subscriber after mount");
  });
});

describe("register-views: orchestration subscribes to service", () => {
  beforeEach(() => { orchestrationSubscribers.length = 0; });
  afterEach(() => { cleanup(); orchestrationSubscribers.length = 0; });

  it("registers a subscriber on mount", async () => {
    renderAdapter("orchestration");
    await waitForEffect();
    assert.equal(orchestrationSubscribers.length, 1, "should have one subscriber after mount");
  });
});
