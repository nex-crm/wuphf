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

// Stable store mock so tests can assert on setPendingComposerDraft calls (a
// fresh vi.fn() per selector call would otherwise be unassertable).
const setPendingComposerDraft = vi.fn();
vi.mock("../../stores/app", () => ({
  useAppStore: (
    selector: (s: {
      setPendingComposerDraft: (channel: string, text: string) => void;
    }) => unknown,
  ) => selector({ setPendingComposerDraft }),
}));

vi.mock("../ui/Toast", () => ({ showNotice: vi.fn() }));
vi.mock("../ui/ConfirmDialog", () => ({ confirm: vi.fn() }));
vi.mock("../../lib/sidebarNav", () => ({ navigateToSidebarApp: vi.fn() }));

// The live preview boots a real dev server; stub it so the view renders inert.
// Expose a button that fires onSelect so a test can simulate a "select to edit"
// click without a real iframe/postMessage round-trip.
interface MockLivePreviewProps {
  selectMode?: boolean;
  onSelect?: (sel: {
    file: string;
    line: number;
    col: number;
    tag: string;
    label: string;
  }) => void;
}
vi.mock("./AppLivePreview", () => ({
  AppLivePreview: ({ selectMode, onSelect }: MockLivePreviewProps) => (
    <div data-testid="live-preview" data-select-mode={String(!!selectMode)}>
      <button
        type="button"
        data-testid="fire-select"
        onClick={() =>
          onSelect?.({
            file: "components/Button.tsx",
            line: 12,
            col: 4,
            tag: "button",
            label: "Save",
          })
        }
      >
        fire select
      </button>
    </div>
  ),
}));
vi.mock("./CustomAppFrame", () => ({
  CustomAppFrame: ({ html, title }: { html: string; title: string }) => (
    <div data-testid="frame" data-title={title}>
      {html}
    </div>
  ),
}));
// The edit panel mounts the shared chat primitives; stub it to a marker so we
// assert open/close + the bound channel without mounting MessageFeed/Composer.
vi.mock("./AppEditPanel", () => ({
  AppEditPanel: ({
    appName,
    channel,
    onClose,
  }: {
    appName: string;
    channel: string;
    onClose: () => void;
  }) => (
    <aside data-testid="edit-panel" data-channel={channel}>
      <span>Editing {appName}</span>
      <button type="button" data-testid="edit-panel-close" onClick={onClose}>
        close
      </button>
    </aside>
  ),
}));

const APP_ID = "app_0000000000000abc";
const EDIT_CHANNEL = "task-office-7";

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
      editChannel: EDIT_CHANNEL,
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

describe("CustomAppView single surface", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getApp).mockResolvedValue(appDetail());
  });

  it("shows ONLY the sealed bundle by default — no Live/Sealed toggle", async () => {
    renderView();
    await screen.findByText("Lead Scorer");

    // The finished app renders the sealed (published) bundle, not the live dev
    // server, and there is no mode toggle to choose between them.
    expect(screen.getByTestId("frame")).toHaveTextContent("CURRENT_HTML");
    expect(screen.queryByTestId("live-preview")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^live$/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^sealed$/i }),
    ).not.toBeInTheDocument();
  });

  it("opens the edit chat panel on Edit and swaps the stage to the live preview", async () => {
    renderView();
    await screen.findByText("Lead Scorer");

    // No edit panel and no live preview until Edit is clicked.
    expect(screen.queryByTestId("edit-panel")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^edit$/i }));

    // The panel is bound to the app's persistent edit channel, and the stage now
    // shows the hot-reloading live preview instead of the sealed bundle.
    const panel = await screen.findByTestId("edit-panel");
    expect(panel.getAttribute("data-channel")).toBe(EDIT_CHANNEL);
    expect(screen.getByTestId("live-preview")).toBeInTheDocument();
    expect(screen.queryByTestId("frame")).not.toBeInTheDocument();

    // Closing returns to the sealed bundle.
    fireEvent.click(screen.getByTestId("edit-panel-close"));
    await waitFor(() =>
      expect(screen.queryByTestId("edit-panel")).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId("frame")).toHaveTextContent("CURRENT_HTML");
  });

  it("hides the Edit affordance when the app has no edit channel", async () => {
    const detail = appDetail();
    detail.app.editChannel = "";
    vi.mocked(getApp).mockResolvedValue(detail);
    renderView();
    await screen.findByText("Lead Scorer");

    expect(
      screen.queryByRole("button", { name: /^edit$/i }),
    ).not.toBeInTheDocument();
  });
});

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
    // Default view is the sealed current build, not a past version.
    expect(screen.getByTestId("frame")).toHaveTextContent("CURRENT_HTML");

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
    // Back on the sealed current build.
    expect(screen.getByTestId("frame")).toHaveTextContent("CURRENT_HTML");
  });
});

describe("CustomAppView select to edit", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getApp).mockResolvedValue(appDetail());
  });

  it("seeds the edit chat composer with the element ref on select", async () => {
    renderView();
    await screen.findByText("Lead Scorer");

    // Select to edit appears only while the edit chat is open (it primes the
    // live preview inspector). Open Edit first.
    fireEvent.click(screen.getByRole("button", { name: /^edit$/i }));
    const toggle = await screen.findByRole("button", {
      name: /select to edit/i,
    });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    expect(
      screen.getByTestId("live-preview").getAttribute("data-select-mode"),
    ).toBe("false");

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getByTestId("live-preview").getAttribute("data-select-mode"),
    ).toBe("true");

    // Simulating a select seeds the edit composer (keyed to the app's edit
    // channel) with the element + its source location, and turns select mode
    // back off (one-shot).
    fireEvent.click(screen.getByTestId("fire-select"));
    expect(setPendingComposerDraft).toHaveBeenCalledTimes(1);
    const [channel, seed] = setPendingComposerDraft.mock.calls[0];
    expect(channel).toBe(EDIT_CHANNEL);
    expect(typeof seed).toBe("string");
    expect(seed).toContain("button");
    expect(seed).toContain("components/Button.tsx:12");
    expect(seed).toContain("Save");

    expect(toggle).toHaveAttribute("aria-pressed", "false");
    // The edit panel stays open after a select.
    expect(screen.getByTestId("edit-panel")).toBeInTheDocument();
  });
});
