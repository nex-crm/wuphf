/**
 * Component tests for the @-mention picker.
 */
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MentionMenu } from "./MentionMenu";
import type { MentionItem } from "./mentionCatalog";

const ITEMS: MentionItem[] = [
  { slug: "team/people/alex", title: "Alex Chen", category: "people" },
  { slug: "team/people/sarah", title: "Sarah Lee", category: "people" },
  {
    slug: "team/projects/backend",
    title: "Backend rewrite",
    category: "projects",
  },
  {
    slug: "agents/operator/notebook/today",
    title: "Operator notebook",
    category: "agents",
  },
];

describe("<MentionMenu>", () => {
  const position = { top: 100, left: 100 };

  it("groups results by category", () => {
    render(
      <MentionMenu
        items={ITEMS}
        query=""
        position={position}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByText("People")).toBeInTheDocument();
    expect(screen.getByText("Projects")).toBeInTheDocument();
    expect(screen.getByText("Agents")).toBeInTheDocument();
  });

  it("filters items by query", () => {
    render(
      <MentionMenu
        items={ITEMS}
        query="alex"
        position={position}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(
      screen.getByTestId("wk-mention-team/people/alex"),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("wk-mention-team/people/sarah")).toBeNull();
  });

  it("respects categoryFilter for sliced pickers", () => {
    render(
      <MentionMenu
        items={ITEMS}
        query=""
        position={position}
        categoryFilter="agents"
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(
      screen.getByTestId("wk-mention-agents/operator/notebook/today"),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("wk-mention-team/people/alex")).toBeNull();
  });

  it("emits the picked item on click", () => {
    const onSelect = vi.fn();
    render(
      <MentionMenu
        items={ITEMS}
        query=""
        position={position}
        onSelect={onSelect}
        onClose={() => {}}
      />,
    );
    fireEvent.mouseDown(screen.getByTestId("wk-mention-team/people/alex"));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect.mock.calls[0][0].slug).toBe("team/people/alex");
  });

  it("renders the empty state when nothing matches", () => {
    render(
      <MentionMenu
        items={ITEMS}
        query="nonexistent"
        position={position}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByTestId("wk-mention-menu-empty")).toBeInTheDocument();
  });

  it("renders the heading when provided", () => {
    render(
      <MentionMenu
        items={ITEMS}
        query=""
        position={position}
        heading="Insert task reference"
        categoryFilter="agents"
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByText("Insert task reference")).toBeInTheDocument();
  });
});
