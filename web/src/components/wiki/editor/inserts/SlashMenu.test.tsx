/**
 * Component tests for the slash menu.
 *
 * Verifies query filtering, keyboard navigation (Arrow keys + Enter +
 * Escape), and that selection emits the canonical `SlashAction` id.
 */
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SlashMenu } from "./SlashMenu";

describe("<SlashMenu>", () => {
  const position = { top: 100, left: 100 };

  it("shows every action when the query is empty", () => {
    render(
      <SlashMenu
        query=""
        position={position}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByTestId("wk-slash-action-wiki-link")).toBeInTheDocument();
    expect(screen.getByTestId("wk-slash-action-citation")).toBeInTheDocument();
    expect(screen.getByTestId("wk-slash-action-fact")).toBeInTheDocument();
    expect(screen.getByTestId("wk-slash-action-task-ref")).toBeInTheDocument();
    expect(
      screen.getByTestId("wk-slash-action-agent-mention"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("wk-slash-action-decision")).toBeInTheDocument();
    expect(screen.getByTestId("wk-slash-action-related")).toBeInTheDocument();
  });

  it("filters by title or keyword substring", () => {
    render(
      <SlashMenu
        query="cite"
        position={position}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByTestId("wk-slash-action-citation")).toBeInTheDocument();
    expect(screen.queryByTestId("wk-slash-action-fact")).toBeNull();
  });

  it("renders the empty state when no actions match", () => {
    render(
      <SlashMenu
        query="absolutely-nothing"
        position={position}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByTestId("wk-slash-menu-empty")).toBeInTheDocument();
  });

  it("emits the action id on click", () => {
    const onSelect = vi.fn();
    render(
      <SlashMenu
        query=""
        position={position}
        onSelect={onSelect}
        onClose={() => {}}
      />,
    );
    fireEvent.mouseDown(screen.getByTestId("wk-slash-action-fact"));
    expect(onSelect).toHaveBeenCalledWith("fact");
  });

  it("commits the active action on Enter", () => {
    const onSelect = vi.fn();
    render(
      <SlashMenu
        query=""
        position={position}
        onSelect={onSelect}
        onClose={() => {}}
      />,
    );
    fireEvent.keyDown(window, { key: "ArrowDown" });
    fireEvent.keyDown(window, { key: "ArrowDown" });
    fireEvent.keyDown(window, { key: "Enter" });
    expect(onSelect).toHaveBeenCalledTimes(1);
    // First action was wiki-link (idx 0); two ArrowDowns -> fact (idx 2).
    expect(onSelect).toHaveBeenCalledWith("fact");
  });

  it("calls onClose on Escape", () => {
    const onClose = vi.fn();
    render(
      <SlashMenu
        query=""
        position={position}
        onSelect={() => {}}
        onClose={onClose}
      />,
    );
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });
});
