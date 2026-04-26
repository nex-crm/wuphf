import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StreamLineView } from "./StreamLineView";

describe("<StreamLineView>", () => {
  it("renders Claude assistant text and tool-use blocks", () => {
    render(
      <StreamLineView
        line={{
          id: 1,
          data: "",
          parsed: {
            type: "assistant",
            message: {
              content: [
                { type: "text", text: "I am posting the update now." },
                {
                  type: "tool_use",
                  name: "team_broadcast",
                  input: { channel: "general", content: "Done" },
                },
              ],
            },
          },
        }}
      />,
    );

    expect(
      screen.getByText("I am posting the update now."),
    ).toBeInTheDocument();
    expect(screen.getByText("team_broadcast")).toBeInTheDocument();
    expect(screen.getByText("Done")).toBeInTheDocument();
  });

  it("renders structured MCP tool audit events", () => {
    render(
      <StreamLineView
        line={{
          id: 1,
          data: "",
          parsed: {
            type: "mcp_tool_event",
            phase: "call",
            tool: "team_broadcast",
            arguments: { channel: "general", content: "Exact content" },
          },
        }}
      />,
    );

    expect(screen.getByText("call: team_broadcast")).toBeInTheDocument();
    expect(screen.getByText("Exact content")).toBeInTheDocument();
  });

  it("does not render an Error chip for tool events that omit the error field", () => {
    // Regression: the broker emits mcp_tool_event with `error` either
    // missing entirely (call phase) or set to "" (successful result).
    // The previous `error !== null` check let undefined pass through
    // and rendered an "× ERROR / undefined" chip on every successful
    // tool call. The fix treats null AND undefined AND "" as "no
    // error". This test would fail under the old code because
    // `screen.getByText("✗")` (or any error glyph) would match.
    const { container } = render(
      <StreamLineView
        line={{
          id: 1,
          data: "",
          parsed: {
            type: "mcp_tool_event",
            phase: "result",
            tool: "team_broadcast",
            arguments: { channel: "general", content: "Done" },
            result: "Posted to #general as @ceo",
            // error: undefined — key intentionally omitted to match
            // what the broker actually sends for a successful call.
          },
        }}
      />,
    );
    // No "Error" summary chip in the closed-card view.
    expect(container.querySelector(".cc-tool-error")).toBeNull();
    // Sanity: nothing leaks the literal string "undefined" as content.
    expect(container.textContent).not.toMatch(/undefined/);
  });

  it("does render the Error chip when the tool event reports a real error", () => {
    render(
      <StreamLineView
        line={{
          id: 1,
          data: "",
          parsed: {
            type: "mcp_tool_event",
            phase: "error",
            tool: "broker_post_message",
            arguments: { channel: "general" },
            error: "permission denied",
          },
        }}
      />,
    );
    // Closed-card "× <truncated error>" summary visible.
    expect(screen.getByText(/permission denied/)).toBeInTheDocument();
  });

  it("renders Codex completed message content arrays", () => {
    render(
      <StreamLineView
        line={{
          id: 1,
          data: "",
          parsed: {
            type: "item.completed",
            item: {
              type: "message",
              content: [{ type: "output_text", text: "Final Codex answer" }],
            },
          },
        }}
      />,
    );

    expect(screen.getByText("Final Codex answer")).toBeInTheDocument();
  });
});
