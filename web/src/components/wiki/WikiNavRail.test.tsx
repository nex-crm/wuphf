import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import WikiNavRail from "./WikiNavRail";

describe("<WikiNavRail>", () => {
  it("navigates to the main page, recent changes, all files, and health", () => {
    const onNavigate = vi.fn();
    render(<WikiNavRail activePath="" onNavigate={onNavigate} />);
    fireEvent.click(screen.getByRole("link", { name: "Recent changes" }));
    expect(onNavigate).toHaveBeenCalledWith("_audit");
    fireEvent.click(screen.getByRole("link", { name: "All files" }));
    expect(onNavigate).toHaveBeenCalledWith("_files");
    fireEvent.click(screen.getByRole("link", { name: "Wiki health" }));
    expect(onNavigate).toHaveBeenCalledWith("_lint");
    fireEvent.click(screen.getByRole("link", { name: "Main page" }));
    expect(onNavigate).toHaveBeenCalledWith("");
  });

  it("marks the active view with aria-current", () => {
    render(<WikiNavRail activePath="_audit" onNavigate={() => {}} />);
    expect(
      screen.getByRole("link", { name: "Recent changes" }),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Main page" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("hosts the article Contents slot", () => {
    render(
      <WikiNavRail activePath="team/people/eng.md" onNavigate={() => {}}>
        <div data-testid="contents-slot" />
      </WikiNavRail>,
    );
    expect(screen.getByTestId("contents-slot")).toBeInTheDocument();
  });
});
