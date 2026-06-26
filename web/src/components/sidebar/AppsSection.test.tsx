import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { CustomApp } from "../../api/apps";

const { mockNavigate, mockListApps } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockListApps: vi.fn(),
}));

vi.mock("../../api/apps", () => ({ listApps: mockListApps }));
vi.mock("../../lib/sidebarNav", () => ({ navigateToSidebarApp: mockNavigate }));
vi.mock("../../routes/useCurrentRoute", () => ({
  useCurrentApp: () => undefined,
}));

import { AppsSection } from "./AppsSection";

function makeApp(overrides: Partial<CustomApp>): CustomApp {
  return {
    id: "app_0000000000000000",
    slug: "app",
    name: "App",
    icon: "🧩",
    entry: "index.html",
    version: 1,
    createdBy: "app-builder",
    createdAt: "2026-06-15T00:00:00Z",
    updatedAt: "2026-06-15T00:00:00Z",
    contentHash: "abc",
    ...overrides,
  };
}

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AppsSection />
    </QueryClientProvider>,
  );
}

describe("AppsSection ready/building split", () => {
  it("renders published apps as clickable items and drafts as building rows", async () => {
    mockNavigate.mockClear();
    mockListApps.mockResolvedValue([
      makeApp({ id: "app_ready", name: "Ready App", status: "ready" }),
      makeApp({ id: "app_draft", name: "Building App", status: "building" }),
    ]);

    renderSection();

    // Published app shows and is navigable.
    const ready = await screen.findByText("Ready App");
    fireEvent.click(ready);
    expect(mockNavigate).toHaveBeenCalledWith("app_ready");

    // Draft shows as a building row, not a clickable app.
    expect(screen.getByText("Building App")).toBeInTheDocument();
    expect(screen.getByText("building…")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Building App"));
    expect(mockNavigate).toHaveBeenCalledTimes(1); // still only the ready click
  });
});
