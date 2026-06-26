import { describe, expect, it } from "vitest";

import {
  type BuildEvent,
  extractBuildEvents,
  humanizeToolEvent,
  reduceBuildActivity,
  summarizeResult,
} from "./buildActivity";

function ev(partial: Partial<BuildEvent>): BuildEvent {
  return {
    type: "",
    toolName: "",
    detail: "",
    text: "",
    turnId: "t1",
    ...partial,
  };
}

describe("extractBuildEvents", () => {
  it("keeps only headless_event lines and maps the fields", () => {
    const lines = [
      { parsed: { kind: "raw_provider", type: "tool_use" } },
      {
        parsed: {
          kind: "headless_event",
          type: "tool_use",
          tool_name: "Write",
          detail: '{"file_path":"src/App.tsx"}',
          turn_id: "abc",
        },
      },
      { parsed: undefined },
    ];
    const out = extractBuildEvents(lines);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({ type: "tool_use", toolName: "Write" });
  });
});

describe("humanizeToolEvent", () => {
  it("maps native tools to a verb + short target", () => {
    expect(humanizeToolEvent("Write", '{"file_path":"a/b/App.tsx"}')).toEqual({
      verb: "Writing",
      target: "App.tsx",
    });
    expect(humanizeToolEvent("Bash", '{"command":"bun run build"}')).toEqual({
      verb: "Running",
      target: "bun run build",
    });
    expect(humanizeToolEvent("Read", '{"file_path":"AI_RULES.md"}')).toEqual({
      verb: "Reading",
      target: "AI_RULES.md",
    });
  });

  it("maps app tools and strips an mcp prefix", () => {
    expect(
      humanizeToolEvent("mcp__teammcp__register_app", '{"name":"Lead Scorer"}'),
    ).toEqual({ verb: "Publishing", target: "Lead Scorer" });
    expect(humanizeToolEvent("list_apps", "")).toEqual({
      verb: "Checking",
      target: "existing apps",
    });
  });

  it("title-cases an unknown tool and surfaces its first string arg", () => {
    expect(humanizeToolEvent("custom_thing", '{"x":"hello"}')).toEqual({
      verb: "Custom Thing",
      target: "hello",
    });
  });
});

describe("summarizeResult", () => {
  it("pulls a message out of a JSON result", () => {
    expect(summarizeResult('{"message":"app published","version":2}')).toBe(
      "app published",
    );
  });
  it("truncates long plain text", () => {
    expect(summarizeResult("a".repeat(80))).toHaveLength(56);
  });
});

describe("reduceBuildActivity", () => {
  it("merges a tool_use + tool_result into one resolved row", () => {
    const items = reduceBuildActivity([
      ev({
        type: "tool_use",
        toolName: "Write",
        detail: '{"file_path":"App.tsx"}',
      }),
      ev({ type: "tool_result", toolName: "Write", text: '{"message":"ok"}' }),
    ]);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      verb: "Writing",
      target: "App.tsx",
      status: "done",
      note: "ok",
    });
  });

  it("resolves a still-open tool_use when the turn ends (no zombie spinner)", () => {
    const items = reduceBuildActivity([
      ev({
        type: "tool_use",
        toolName: "Bash",
        detail: '{"command":"bun install"}',
      }),
      ev({ type: "idle" }),
    ]);
    expect(items).toHaveLength(1);
    expect(items[0].status).toBe("done");
  });

  it("marks open rows in a turn as error on an error event", () => {
    const items = reduceBuildActivity([
      ev({
        type: "tool_use",
        toolName: "Bash",
        detail: '{"command":"bun run build"}',
      }),
      ev({ type: "error", text: "exit 1" }),
    ]);
    expect(items[0].status).toBe("error");
  });

  it("matches results FIFO across two calls of the same tool", () => {
    const items = reduceBuildActivity([
      ev({
        type: "tool_use",
        toolName: "Write",
        detail: '{"file_path":"A.tsx"}',
      }),
      ev({
        type: "tool_use",
        toolName: "Write",
        detail: '{"file_path":"B.tsx"}',
      }),
      ev({
        type: "tool_result",
        toolName: "Write",
        text: '{"message":"wrote A"}',
      }),
      ev({
        type: "tool_result",
        toolName: "Write",
        text: '{"message":"wrote B"}',
      }),
    ]);
    expect(items).toHaveLength(2);
    expect(items[0]).toMatchObject({
      target: "A.tsx",
      note: "wrote A",
      status: "done",
    });
    expect(items[1]).toMatchObject({
      target: "B.tsx",
      note: "wrote B",
      status: "done",
    });
  });

  it("resolves a prior native tool when the next tool starts (no named result)", () => {
    // Native tools (Read/Bash) rarely emit a name-tagged tool_result, so the
    // next tool_use is what tells us the prior one finished. Only the last row
    // should still be running.
    const items = reduceBuildActivity([
      ev({
        type: "tool_use",
        toolName: "Read",
        detail: '{"file_path":"AI_RULES.md"}',
      }),
      ev({
        type: "tool_use",
        toolName: "Read",
        detail: '{"file_path":"App.tsx"}',
      }),
      ev({
        type: "tool_use",
        toolName: "Bash",
        detail: '{"command":"bun run build"}',
      }),
    ]);
    expect(items.map((i) => i.status)).toEqual(["done", "done", "running"]);
    expect(items[2]).toMatchObject({
      verb: "Running",
      target: "bun run build",
    });
  });

  it("keeps a row running until it resolves", () => {
    const items = reduceBuildActivity([
      ev({
        type: "tool_use",
        toolName: "Bash",
        detail: '{"command":"bun run dev"}',
      }),
    ]);
    expect(items[0].status).toBe("running");
  });
});
