import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TemplatesStep } from "./Step2Templates";
import type { BlueprintTemplate } from "./types";

function makeTemplates(): BlueprintTemplate[] {
  // Two known blueprint ids in different categories + one unknown id
  // exercise the "Other" filter chip. See BLUEPRINT_DISPLAY in
  // constants.ts for the keyed entries.
  return [
    {
      id: "bookkeeping-invoicing-service",
      name: "Bookkeeping",
      description: "Long backend description.",
      outcome: "Books · invoices · monthly close",
      category: "services",
      estimated_setup_minutes: 5,
      agents: [
        {
          slug: "ceo",
          name: "CEO",
          role: "lead",
          built_in: true,
          checked: true,
        },
        {
          slug: "bookkeeper",
          name: "Bookkeeper",
          role: "books",
          checked: true,
        },
      ],
      channels: [{ slug: "books", name: "books", purpose: "Reconciliation" }],
      skills: [{ name: "Reconciliation", purpose: "Match bank feeds" }],
      first_tasks: [
        {
          id: "draft-monthly-close",
          title: "Draft a monthly close checklist",
          prompt: "Draft it.",
          expected_output: "A numbered checklist.",
        },
      ],
      requirements: [
        {
          kind: "api-key",
          name: "ANTHROPIC_API_KEY or OPENAI_API_KEY",
          required: true,
        },
      ],
    },
    {
      id: "youtube-factory",
      name: "YouTube Factory",
      description: "Long backend description.",
      outcome: "Script · film · publish",
      category: "media",
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

  it("renders pack cards across categories with All filter active by default", () => {
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

    // Filter chips should be present, with "All" selected.
    const allChip = screen.getByRole("tab", { name: "All" });
    expect(allChip).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Services" })).toBeInTheDocument();
    expect(
      screen.getByRole("tab", { name: "Media & Community" }),
    ).toBeInTheDocument();
    // "Other" chip surfaces because Mystery Op has no category.
    expect(screen.getByRole("tab", { name: "Other" })).toBeInTheDocument();

    // All three packs render under the All filter.
    expect(screen.getByText("Bookkeeping")).toBeInTheDocument();
    expect(screen.getByText("YouTube Factory")).toBeInTheDocument();
    expect(screen.getByText("Mystery Op")).toBeInTheDocument();
  });

  it("filters cards when a category chip is clicked", () => {
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

    fireEvent.click(screen.getByRole("tab", { name: "Services" }));
    expect(screen.getByText("Bookkeeping")).toBeInTheDocument();
    expect(screen.queryByText("YouTube Factory")).not.toBeInTheDocument();
    expect(screen.queryByText("Mystery Op")).not.toBeInTheDocument();

    // Other filter routes the unknown blueprint id.
    fireEvent.click(screen.getByRole("tab", { name: "Other" }));
    expect(screen.getByText("Mystery Op")).toBeInTheDocument();
    expect(screen.queryByText("Bookkeeping")).not.toBeInTheDocument();
  });

  it("clicking a pack card opens the detail panel and selects the pack", () => {
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

    // Detail panel surfaces the outcome and the first task title.
    const panel = screen.getByLabelText("Bookkeeping details");
    expect(within(panel).getByText("Bookkeeping")).toBeInTheDocument();
    expect(
      within(panel).getByText("Draft a monthly close checklist"),
    ).toBeInTheDocument();
    expect(
      within(panel).getByText(/ANTHROPIC_API_KEY or OPENAI_API_KEY/),
    ).toBeInTheDocument();
  });

  it("closes the detail panel when the close button is clicked", () => {
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
    fireEvent.click(screen.getByText("Bookkeeping"));
    fireEvent.click(
      screen.getByRole("button", { name: /Close pack details/i }),
    );
    expect(
      screen.queryByLabelText("Bookkeeping details"),
    ).not.toBeInTheDocument();
  });

  it("shows the empty-state copy when a pack has no first-task or skill metadata", () => {
    const templates: BlueprintTemplate[] = [
      {
        id: "future-blueprint-not-yet-keyed",
        name: "Mystery Op",
        description: "no metadata",
      },
    ];
    render(
      <TemplatesStep
        templates={templates}
        loading={false}
        selected={null}
        onSelect={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    fireEvent.click(screen.getByText("Mystery Op"));
    const panel = screen.getByLabelText("Mystery Op details");
    expect(
      within(panel).getByText(
        /Tasks, skills, and requirements will be configured during setup/i,
      ),
    ).toBeInTheDocument();
  });

  it("marks 'Start from scratch' as selected when selected=null and unselects detail on click", () => {
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
    const fromScratch = screen
      .getByText(/Start from scratch/i)
      .closest("button");
    expect(fromScratch).toHaveClass("selected");
    fireEvent.click(fromScratch as HTMLElement);
    expect(onSelect).toHaveBeenCalledWith(null);
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

  it("auto-opens the detail panel for the currently-selected pack on mount", () => {
    render(
      <TemplatesStep
        templates={makeTemplates()}
        loading={false}
        selected="bookkeeping-invoicing-service"
        onSelect={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    expect(screen.getByLabelText("Bookkeeping details")).toBeInTheDocument();
  });

  it("applies the responsive grid class so detail columns stack on mobile", () => {
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
    fireEvent.click(screen.getByText("Bookkeeping"));
    // The .pack-detail-grid class is the load-bearing element for the
    // ≤640px column-stack rule in onboarding.css. Asserting the class
    // is present is the cheapest reliable check; jsdom does not run
    // CSS so we cannot test the cascade itself.
    const panel = screen.getByLabelText("Bookkeeping details");
    expect(panel.querySelector(".pack-detail-grid")).not.toBeNull();
  });
});
