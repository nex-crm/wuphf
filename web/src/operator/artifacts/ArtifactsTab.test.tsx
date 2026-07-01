import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ArtifactsTab } from "./ArtifactsTab";
import type { Artifact } from "./artifacts";

const ARTIFACTS: Artifact[] = [
  {
    id: "app",
    type: "app",
    title: "Pipeline Agent",
    producedBy: "built by Nex",
    at: "v3",
  },
  {
    id: "m1",
    type: "md",
    title: "weekly-summary.md",
    producedBy: "weeklyPipelineSummary",
    at: "Monday",
    content: "# Recap\n6 deals moved",
  },
  {
    id: "p1",
    type: "pdf",
    title: "brief.pdf",
    producedBy: "weeklyPipelineSummary",
    at: "Jun 30",
    size: "182 KB",
  },
];

describe("ArtifactsTab", () => {
  it("lists every artifact and renders the app first via renderApp", () => {
    const { getByText, getByTestId } = render(
      <ArtifactsTab
        agentName="Pipeline Agent"
        artifacts={ARTIFACTS}
        renderApp={() => <div data-testid="live-app" />}
      />,
    );
    // All artifacts appear as chips (app + md + pdf).
    expect(getByText("weekly-summary.md")).toBeTruthy();
    expect(getByText("brief.pdf")).toBeTruthy();
    // The first artifact (the app) is selected and rendered via the host slot.
    expect(getByTestId("live-app")).toBeTruthy();
  });

  it("switches viewer when another artifact is selected", () => {
    const { getByText, queryByTestId } = render(
      <ArtifactsTab
        agentName="Pipeline Agent"
        artifacts={ARTIFACTS}
        renderApp={() => <div data-testid="live-app" />}
      />,
    );
    fireEvent.click(getByText("weekly-summary.md"));
    expect(queryByTestId("live-app")).toBeNull();
    expect(getByText(/6 deals moved/)).toBeTruthy();
    // The pdf artifact shows the file card with a download affordance.
    fireEvent.click(getByText("brief.pdf"));
    expect(getByText("Download")).toBeTruthy();
    expect(getByText(/182 KB/)).toBeTruthy();
  });

  it("shows the honest empty state when nothing was produced yet", () => {
    const { getByText } = render(
      <ArtifactsTab agentName="Pipeline Agent" artifacts={[]} />,
    );
    expect(getByText(/Nothing yet/)).toBeTruthy();
  });
});
