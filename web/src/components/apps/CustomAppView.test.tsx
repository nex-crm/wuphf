import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  deleteApp,
  getApp,
  getAppVersion,
  listAppVersions,
  rollbackApp,
} from "../../api/apps";
import { CustomAppView } from "./CustomAppView";

vi.mock("../../api/apps", () => ({
  getApp: vi.fn(),
  listAppVersions: vi.fn(),
  getAppVersion: vi.fn(),
  rollbackApp: vi.fn(),
  deleteApp: vi.fn(),
}));

vi.mock("../../stores/app", () => ({
  useAppStore: (
    selector: (s: { openUpdateAppDialog: () => void }) => unknown,
  ) => selector({ openUpdateAppDialog: vi.fn() }),
}));

vi.mock("../ui/Toast", () => ({ showNotice: vi.fn() }));
vi.mock("../ui/ConfirmDialog", () => ({ confirm: vi.fn() }));
vi.mock("../../lib/sidebarNav", () => ({ navigateToSidebarApp: vi.fn() }));

// The live preview boots a real dev server; stub it so the view renders inert.
vi.mock("./AppLivePreview", () => ({
  AppLivePreview: () => <div data-testid="live-preview" />,
}));
vi.mock("./CustomAppFrame", () => ({
  CustomAppFrame: ({ html, title }: { html: string; title: string }) => (
    <div data-testid="frame" data-title={title}>
      {html}
    </div>
  ),
}));

const APP_ID = "app_0000000000000abc";

function appDetail() {
  return {
    app: {
      id: APP_ID,
      slug: "lead-scorer",
      name: "Lead Scorer",
      icon: "🧩",
      summary: "Scores inbound leads",
      entry: "index.html",
      version: 3,
      createdBy: "app-builder",
      createdAt: "2026-06-10T00:00:00Z",
      updatedAt: "2026-06-15T12:00:00Z",
      contentHash: "h",
    },
    html: "CURRENT_HTML",
  };
}

function renderView() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  }
  return render(<CustomAppView appId={APP_ID} />, { wrapper: Wrapper });
}

describe("CustomAppView version history", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getApp).mockResolvedValue(appDetail());
    vi.mocked(listAppVersions).mockResolvedValue([
      {
        version: 3,
        current: true,
        updatedBy: "pam",
        updatedAt: "2026-06-15T12:00:00Z",
      },
      {
        version: 2,
        current: false,
        updatedBy: "app-builder",
        updatedAt: "2026-06-14T12:00:00Z",
      },
      {
        version: 1,
        current: false,
        updatedBy: "app-builder",
        updatedAt: "2026-06-13T12:00:00Z",
      },
    ]);
    vi.mocked(getAppVersion).mockResolvedValue({
      version: 2,
      current: false,
      updatedBy: "app-builder",
      updatedAt: "2026-06-14T12:00:00Z",
      html: "V2_HTML",
    });
    vi.mocked(rollbackApp).mockResolvedValue({
      ...appDetail().app,
      version: 4,
    });
    vi.mocked(deleteApp).mockResolvedValue();
  });

  it("opens the timeline, previews an older version non-destructively, then restores", async () => {
    renderView();
    await screen.findByText("Lead Scorer");
    // Default view is the live preview, not a past version.
    expect(screen.getByTestId("live-preview")).toBeInTheDocument();

    // Open history → the timeline lists prior builds.
    fireEvent.click(screen.getByRole("button", { name: /history/i }));
    await screen.findByText("Version history");
    expect(await screen.findByText("v2")).toBeInTheDocument();

    // Select v2 → non-destructive preview: getAppVersion is read, the current
    // build is NOT mutated (no rollback yet), and the read-only banner appears.
    fireEvent.click(screen.getByText("v2"));
    await waitFor(() =>
      expect(vi.mocked(getAppVersion)).toHaveBeenCalledWith(APP_ID, 2),
    );
    expect(await screen.findByText(/Viewing/)).toBeInTheDocument();
    expect(screen.getByTestId("frame").getAttribute("data-title")).toContain(
      "v2",
    );
    expect(vi.mocked(rollbackApp)).not.toHaveBeenCalled();

    // Restore → the explicit, append-only rollback runs.
    fireEvent.click(
      screen.getByRole("button", { name: /restore this version/i }),
    );
    await waitFor(() =>
      expect(vi.mocked(rollbackApp)).toHaveBeenCalledWith(APP_ID, 2),
    );
  });

  it("returns to the current build from the preview banner without restoring", async () => {
    renderView();
    await screen.findByText("Lead Scorer");
    fireEvent.click(screen.getByRole("button", { name: /history/i }));
    fireEvent.click(await screen.findByText("v2"));
    await screen.findByText(/Viewing/);

    fireEvent.click(
      screen.getByRole("button", { name: /back to current v3/i }),
    );
    await waitFor(() =>
      expect(screen.queryByText(/Viewing/)).not.toBeInTheDocument(),
    );
    expect(vi.mocked(rollbackApp)).not.toHaveBeenCalled();
    expect(screen.getByTestId("live-preview")).toBeInTheDocument();
  });
});
