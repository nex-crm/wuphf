import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TemplatesStep } from "./Step2Templates";
import type { BlueprintTemplate } from "./types";

function makeTemplates(): BlueprintTemplate[] {
  return [
    // Two known blueprint ids in different categories + one unknown id
    // exercise the "Other" catch-all bucket. See BLUEPRINT_DISPLAY in
    // constants.ts for the keyed entries.
    {
      id: "bookkeeping-invoicing-service",
      name: "Bookkeeping",
      description: "long description",
    },
    {
      id: "youtube-factory",
      name: "YouTube Factory",
      description: "long description",
    },
    {
      id: "future-blueprint-not-yet-keyed",
      name: "Mystery Op",
      description: "from scratch backend",
    },
  ];
}

describe("TemplatesStep", () => {
  it("shows a loading placeholder and no template cards while loading", () => {
    render(
      <TemplatesStep
        templates={[]}
        loading={true}
        selected={null}
        onSelect={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    expect(screen.getByText(/Loading blueprints/i)).toBeInTheDocument();
    expect(
      screen.queryAllByRole("button", { name: /Bookkeeping|YouTube|Mystery/i }),
    ).toHaveLength(0);
  });

  it("groups known blueprints by category and routes unknown ids to Other", () => {
    render(
      <TemplatesStep
        templates={makeTemplates()}
        loading={false}
        selected={null}
        onSelect={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );

    // Three categories should render: Services, Media & Community, Other.
    // Products has no cards so its panel must be omitted.
    expect(screen.getByText("Services")).toBeInTheDocument();
    expect(screen.getByText("Media & Community")).toBeInTheDocument();
    expect(screen.getByText("Other")).toBeInTheDocument();
    expect(screen.queryByText("Products")).not.toBeInTheDocument();

    // Mystery Op (unknown id) lands in Other, not in any keyed category.
    expect(screen.getByText("Mystery Op")).toBeInTheDocument();
  });

  it("marks 'Start from scratch' as selected when selected=null", () => {
    render(
      <TemplatesStep
        templates={makeTemplates()}
        loading={false}
        selected={null}
        onSelect={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    const fromScratch = screen
      .getByText(/Start from scratch/i)
      .closest("button");
    expect(fromScratch).toHaveClass("selected");
  });

  it("clicking a template tile fires onSelect with that id", () => {
    const onSelect = vi.fn();
    render(
      <TemplatesStep
        templates={makeTemplates()}
        loading={false}
        selected={null}
        onSelect={onSelect}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    fireEvent.click(screen.getByText("Bookkeeping"));
    expect(onSelect).toHaveBeenCalledWith("bookkeeping-invoicing-service");
  });

  it("Back and Next buttons fire their respective callbacks", () => {
    const onBack = vi.fn();
    const onNext = vi.fn();
    render(
      <TemplatesStep
        templates={makeTemplates()}
        loading={false}
        selected={null}
        onSelect={() => {}}
        onNext={onNext}
        onBack={onBack}
      />,
    );
    const nav = document.querySelector(".wizard-nav") as HTMLElement;
    fireEvent.click(within(nav).getByRole("button", { name: /Back/i }));
    fireEvent.click(within(nav).getByRole("button", { name: /Review/i }));
    expect(onBack).toHaveBeenCalledTimes(1);
    expect(onNext).toHaveBeenCalledTimes(1);
  });
});
