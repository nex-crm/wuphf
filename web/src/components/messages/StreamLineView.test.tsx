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
