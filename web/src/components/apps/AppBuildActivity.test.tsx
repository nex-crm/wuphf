import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const { mockUseAgentStream } = vi.hoisted(() => ({
  mockUseAgentStream: vi.fn(),
}));

vi.mock("../../hooks/useAgentStream", () => ({
  useAgentStream: mockUseAgentStream,
}));

import { AppBuildActivity } from "./AppBuildActivity";

function line(id: number, event: Record<string, unknown>) {
  return { id, data: "", parsed: { kind: "headless_event", ...event } };
}

describe("AppBuildActivity", () => {
  it("renders merged tool activity as resolving rows", () => {
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

    render(<AppBuildActivity taskId="OFFICE-1" />);

    // The resolved write row (target is the basename).
    expect(screen.getByText("Writing")).toBeInTheDocument();
    expect(screen.getByText("App.tsx")).toBeInTheDocument();
    expect(screen.getByText("✓")).toBeInTheDocument();
    // The still-running build row.
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("bun run build")).toBeInTheDocument();
    // Two tool calls -> count of 2.
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("renders nothing when there is no activity", () => {
    mockUseAgentStream.mockReturnValue({ connected: false, lines: [] });
    const { container } = render(<AppBuildActivity taskId="OFFICE-2" />);
    expect(container).toBeEmptyDOMElement();
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

    render(<AppBuildActivity taskId="OFFICE-3" />);
    expect(screen.getByText("Reading")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /build activity/i }));
    expect(screen.queryByText("Reading")).not.toBeInTheDocument();
  });
});
