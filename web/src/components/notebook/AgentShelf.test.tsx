import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { NotebookAgentSummary } from "../../api/notebook";
import AgentShelf from "./AgentShelf";

const AGENT: NotebookAgentSummary = {
  agent_slug: "pm",
  name: "PM",
  role: "Product Manager · agent",
  entries: [
    {
      entry_slug: "e1",
      title: "Entry one",
      last_edited_ts: new Date().toISOString(),
      status: "draft",
    },
    {
      entry_slug: "e2",
      title: "Entry two",
      last_edited_ts: new Date().toISOString(),
      status: "promoted",
    },
    {
      entry_slug: "e3",
      title: "Entry three",
      last_edited_ts: new Date().toISOString(),
      status: "in-review",
    },
  ],
  total: 3,
  promoted_count: 1,
  last_updated_ts: new Date().toISOString(),
};

describe("<AgentShelf>", () => {
  it("renders the agent name and role", () => {
    render(
      <AgentShelf
        agent={AGENT}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
      />,
    );
    expect(screen.getByText("PM's notebook")).toBeInTheDocument();
    expect(screen.getByText("Product Manager · agent")).toBeInTheDocument();
  });

  it("shows preview entries as buttons", () => {
    render(
      <AgentShelf
        agent={AGENT}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
      />,
    );
    expect(screen.getByText("Entry one")).toBeInTheDocument();
    expect(screen.getByText("Entry two")).toBeInTheDocument();
    expect(screen.getByText("Entry three")).toBeInTheDocument();
  });

  it("renders status badges", () => {
    render(
      <AgentShelf
        agent={AGENT}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
      />,
    );
    expect(screen.getByText("DRAFT")).toBeInTheDocument();
    expect(screen.getByText("promoted")).toBeInTheDocument();
    expect(screen.getByText("in review")).toBeInTheDocument();
  });

  it("invokes the appropriate callbacks when name or card is clicked", async () => {
    const onOpenAgent = vi.fn();
    const onOpenEntry = vi.fn();
    render(
      <AgentShelf
        agent={AGENT}
        onOpenAgent={onOpenAgent}
        onOpenEntry={onOpenEntry}
      />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByText("PM's notebook"));
    expect(onOpenAgent).toHaveBeenCalledWith("pm");
    await user.click(screen.getByText("Entry two"));
    expect(onOpenEntry).toHaveBeenCalledWith("pm", "e2");
  });

  it("shows empty prompt when agent has no entries", () => {
    render(
      <AgentShelf
        agent={{ ...AGENT, entries: [], total: 0 }}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
      />,
    );
    expect(screen.getByText(/No entries yet/)).toBeInTheDocument();
  });

  it("caps preview to 5 entries by default", () => {
    const many = {
      ...AGENT,
      entries: Array.from({ length: 8 }, (_, i) => ({
        entry_slug: `e${i}`,
        title: `Entry ${i}`,
        last_edited_ts: new Date().toISOString(),
        status: "draft" as const,
      })),
    };
    render(
      <AgentShelf agent={many} onOpenAgent={() => {}} onOpenEntry={() => {}} />,
    );
    // 6 cards total? Count via title role=button with entry text.
    const rendered = screen.getAllByText(/^Entry \d$/);
    expect(rendered).toHaveLength(5);
  });
});
