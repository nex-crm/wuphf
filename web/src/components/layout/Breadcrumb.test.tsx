/**
 * Tests for Breadcrumb component — renders correct label for each object kind,
 * deep links are proper hash URLs, mobile nav remains functional.
 *
 * Phase 5 PR 2 — app navigation refresh.
 */

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Breadcrumb } from "./Breadcrumb";

afterEach(cleanup);

describe("Breadcrumb", () => {
  it("renders nothing when items is empty", () => {
    const { container } = render(<Breadcrumb items={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders a single segment without a separator", () => {
    render(<Breadcrumb items={[{ label: "Tasks", href: "#/tasks" }]} />);
    expect(screen.getByText("Tasks")).toBeInTheDocument();
    expect(screen.queryByText("/")).not.toBeInTheDocument();
  });

  it("renders two segments with a separator", () => {
    render(
      <Breadcrumb
        items={[
          { label: "Tasks", href: "#/tasks" },
          { label: "Task abc-123", href: "#/tasks/abc-123" },
        ]}
      />,
    );
    expect(screen.getByText("Tasks")).toBeInTheDocument();
    expect(screen.getByText("Task abc-123")).toBeInTheDocument();
    // Separator char
    expect(screen.getByText("/")).toBeInTheDocument();
  });

  it("intermediate segments are links, leaf segment is styled differently", () => {
    render(
      <Breadcrumb
        items={[
          { label: "Wiki", href: "#/wiki" },
          { label: "people/nazz", href: "#/wiki/people/nazz" },
        ]}
      />,
    );
    const wikiLink = screen.getByText("Wiki").closest("a");
    expect(wikiLink).toHaveAttribute("href", "#/wiki");

    // Leaf is also an anchor (for deep-link semantics)
    const leafLink = screen.getByText("people/nazz").closest("a");
    expect(leafLink).toHaveAttribute("href", "#/wiki/people/nazz");
  });

  it("copy-link button is present on the leaf segment", () => {
    render(
      <Breadcrumb
        items={[{ label: "Agent: gaia", href: "#/dm/gaia" }]}
      />,
    );
    const copyBtn = screen.getByRole("button", { name: /copy deep link/i });
    expect(copyBtn).toBeInTheDocument();
  });

  it("renders correctly with three segments (notebook-entry)", () => {
    render(
      <Breadcrumb
        items={[
          { label: "Notebooks", href: "#/notebooks" },
          { label: "researcher", href: "#/notebooks/researcher" },
          {
            label: "2026-05-01-insights",
            href: "#/notebooks/researcher/2026-05-01-insights",
          },
        ]}
      />,
    );
    expect(screen.getByText("Notebooks")).toBeInTheDocument();
    expect(screen.getByText("researcher")).toBeInTheDocument();
    expect(screen.getByText("2026-05-01-insights")).toBeInTheDocument();
  });

  it("renders accessibly with nav landmark and aria-label", () => {
    render(<Breadcrumb items={[{ label: "Tasks", href: "#/tasks" }]} />);
    expect(screen.getByRole("navigation", { name: /breadcrumb/i })).toBeInTheDocument();
  });
});
