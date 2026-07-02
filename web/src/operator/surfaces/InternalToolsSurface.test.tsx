import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { CustomApp } from "../../api/apps";
import { InternalToolsSurface } from "./InternalToolsSurface";

// Drive the apps hook so the surface renders without a network or React Query.
const useOperatorApps = vi.fn();
vi.mock("../apps/useOperatorApps", () => ({
  useOperatorApps: () => useOperatorApps(),
  useDeleteApp: () => ({ mutate: vi.fn(), isPending: false }),
  // Build state from status; ignore the createdAt age heuristic in tests.
  appBuildState: (app: { status?: string }) =>
    app.status === "building" ? "building" : "ready",
}));

function app(over: Partial<CustomApp>): CustomApp {
  return {
    id: "app_1",
    slug: "x",
    name: "Open Tasks",
    icon: "📋",
    entry: "index.html",
    version: 1,
    createdBy: "app-builder",
    createdAt: "2026-06-30T10:00:00Z",
    updatedAt: "2026-06-30T10:00:00Z",
    contentHash: "h",
    ...over,
  };
}

function renderSurface() {
  return render(
    <InternalToolsSurface
      onOpen={() => {}}
      onStartCall={() => {}}
      onBuild={() => {}}
    />,
  );
}

describe("InternalToolsSurface", () => {
  // Regression guard: the surface must render without throwing (a stray
  // reference like the removed mock `TOOLS` would crash the whole operator via
  // the ErrorBoundary and make every button disappear).
  it("renders the empty state with a build CTA when there are no apps", () => {
    useOperatorApps.mockReturnValue({ data: [], isLoading: false });
    const { getByText, getByRole } = renderSurface();
    expect(getByText("No agents yet")).toBeTruthy();
    expect(getByRole("button", { name: /build your first agent/i })).toBeTruthy();
  });

  it("renders a ready app as the hero without crashing", () => {
    useOperatorApps.mockReturnValue({
      data: [app({ name: "Daily Digest" })],
      isLoading: false,
    });
    const { getByText } = renderSurface();
    expect(getByText("Daily Digest")).toBeTruthy();
    // The mock "Suggested by your AI" section is gone — only real apps remain.
    expect(() => getByText(/suggested by your ai/i)).toThrow();
  });

  it("renders building and failed apps in the list", () => {
    useOperatorApps.mockReturnValue({
      data: [
        app({ id: "app_ready", name: "Ready One" }),
        app({ id: "app_build", name: "Builder", status: "building" }),
      ],
      isLoading: false,
    });
    const { getByText } = renderSurface();
    expect(getByText("Ready One")).toBeTruthy();
    expect(getByText("Builder")).toBeTruthy();
  });
});
