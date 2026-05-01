import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TeamStep } from "./Step4Team";
import type { BlueprintAgent } from "./types";

const lead: BlueprintAgent = {
  slug: "ceo",
  name: "CEO",
  role: "lead",
  checked: true,
  built_in: true,
};
const member: BlueprintAgent = {
  slug: "gtm",
  name: "GTM Lead",
  role: "go-to-market",
  checked: true,
};
const unchecked: BlueprintAgent = {
  slug: "designer",
  name: "Designer",
  role: "design",
  checked: false,
};

describe("TeamStep", () => {
  it("renders an empty-state message when there are no agents", () => {
    render(
      <TeamStep
        agents={[]}
        onToggle={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    expect(screen.getByText(/No teammates yet/i)).toBeInTheDocument();
  });

  it("renders one tile per agent and a 'Lead' badge for built_in lead", () => {
    render(
      <TeamStep
        agents={[lead, member]}
        onToggle={() => {}}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    expect(screen.getByText("CEO")).toBeInTheDocument();
    expect(screen.getByText("GTM Lead")).toBeInTheDocument();
    expect(screen.getByText("Lead")).toBeInTheDocument();
  });

  it("disables the lead tile and ignores clicks (built_in is locked)", () => {
    const onToggle = vi.fn();
    render(
      <TeamStep
        agents={[lead]}
        onToggle={onToggle}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    const tile = screen.getByText("CEO").closest("button");
    if (!tile) throw new Error("expected CEO tile to be a button");
    expect(tile).toBeDisabled();
    fireEvent.click(tile);
    expect(onToggle).not.toHaveBeenCalled();
  });

  it("clicking a non-locked tile fires onToggle with the agent slug", () => {
    const onToggle = vi.fn();
    render(
      <TeamStep
        agents={[lead, member, unchecked]}
        onToggle={onToggle}
        onNext={() => {}}
        onBack={() => {}}
      />,
    );
    const gtmTile = screen.getByText("GTM Lead").closest("button");
    const designerTile = screen.getByText("Designer").closest("button");
    if (!(gtmTile && designerTile)) throw new Error("expected tiles to render");
    fireEvent.click(gtmTile);
    fireEvent.click(designerTile);
    expect(onToggle).toHaveBeenNthCalledWith(1, "gtm");
    expect(onToggle).toHaveBeenNthCalledWith(2, "designer");
  });
});
