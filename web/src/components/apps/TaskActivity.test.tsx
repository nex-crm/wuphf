import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const { mockUseAgentStream } = vi.hoisted(() => ({
  mockUseAgentStream: vi.fn(),
}));

vi.mock("../../hooks/useAgentStream", () => ({
  useAgentStream: mockUseAgentStream,
}));

import { TaskActivity } from "./TaskActivity";

function line(id: number, event: Record<string, unknown>) {
  return { id, data: "", parsed: { kind: "headless_event", ...event } };
}

describe("TaskActivity", () => {
  it("renders the owner agent's tool activity as resolving rows", () => {
    mockUseAgentStream.mockReturnValue({
      connected: true,
      lines: [
        line(1, {
          type: "tool_use",
          tool_name: "Write",
          detail: '{"file_path":"src/App.tsx"}',
          turn_id: "t1",
        }),
        line(2, {
          type: "tool_result",
          tool_name: "Write",
          text: '{"message":"ok"}',
          turn_id: "t1",
        }),
        line(3, {
          type: "tool_use",
          tool_name: "Bash",
          detail: '{"command":"bun run build"}',
          turn_id: "t1",
        }),
      ],
    });

    render(<TaskActivity taskId="OFFICE-1" agentSlug="app-builder" />);

    expect(screen.getByText("Writing")).toBeInTheDocument();
    expect(screen.getByText("App.tsx")).toBeInTheDocument();
    expect(screen.getByText("✓")).toBeInTheDocument();
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("bun run build")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    // It is labelled "Task activity" (generalized from "Build activity").
    expect(
      screen.getByRole("region", { name: /task activity/i }),
    ).toBeInTheDocument();
  });

  it("passes the owner slug to the stream", () => {
    mockUseAgentStream.mockReturnValue({ connected: false, lines: [] });
    render(<TaskActivity taskId="OFFICE-2" agentSlug="revops" />);
    expect(mockUseAgentStream).toHaveBeenCalledWith("revops", "OFFICE-2", {
      keepAlive: true,
    });
  });

  it("renders nothing when there is no activity", () => {
    mockUseAgentStream.mockReturnValue({ connected: false, lines: [] });
    const { container } = render(
      <TaskActivity taskId="OFFICE-2" agentSlug="app-builder" />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for an unstaffed task (no owner)", () => {
    mockUseAgentStream.mockReturnValue({ connected: false, lines: [] });
    const { container } = render(
      <TaskActivity taskId="OFFICE-4" agentSlug={null} />,
    );
    expect(container).toBeEmptyDOMElement();
    // A null owner streams nothing.
    expect(mockUseAgentStream).toHaveBeenCalledWith(null, "OFFICE-4", {
      keepAlive: true,
    });
  });

  it("collapses the list when the header is toggled", () => {
    mockUseAgentStream.mockReturnValue({
      connected: true,
      lines: [
        line(1, {
          type: "tool_use",
          tool_name: "Read",
          detail: '{"file_path":"AI_RULES.md"}',
          turn_id: "t1",
        }),
      ],
    });

    render(<TaskActivity taskId="OFFICE-3" agentSlug="app-builder" />);
    expect(screen.getByText("Reading")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /task activity/i }));
    expect(screen.queryByText("Reading")).not.toBeInTheDocument();
  });
});
