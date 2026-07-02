import { fireEvent, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { CustomAppDetail } from "../../api/apps";
import { OperatorAppDetail } from "./OperatorAppDetail";

// Control the data hook so we can render the building vs ready states without a
// network or React Query provider.
const useOperatorAppMock = vi.fn();
vi.mock("../apps/useOperatorApps", () => ({
  useOperatorApp: (id: string) => useOperatorAppMock(id),
  // Drive build state from status in tests (ignore the createdAt age heuristic).
  appBuildState: (app: { status?: string }) =>
    app.status === "building" ? "building" : "ready",
  useDeleteApp: () => ({ mutate: vi.fn(), isPending: false }),
  // Real-id check used by the agent-service wiring (tools/routines/sessions).
  isRealAppId: (id: string | null | undefined) =>
    typeof id === "string" && id.startsWith("app_"),
}));

// Stub the sandbox frame (real iframe), the integrations tab (fetches the
// catalog), and the Ask AI chat (React Query) so the test stays unit-scoped.
vi.mock("../../components/apps/CustomAppFrame", () => ({
  CustomAppFrame: ({ appId, html }: { appId: string; html: string }) => (
    <div data-testid="app-frame" data-app-id={appId}>
      {html}
    </div>
  ),
}));
// AppLivePreview runs a dev-server query; stub it so a building app's UI tab is
// unit-testable without a QueryClient or a real dev server.
vi.mock("../../components/apps/AppLivePreview", () => ({
  AppLivePreview: ({ appId }: { appId: string }) => (
    <div data-testid="live-preview" data-app-id={appId} />
  ),
}));
vi.mock("./ToolIntegrations", () => ({
  ToolIntegrations: () => <div data-testid="tool-integrations" />,
}));
vi.mock("./AppToolsChat", () => ({
  AppToolsChat: ({ appName }: { appName: string }) => (
    <div data-testid="ask-ai-chat">tools:{appName}</div>
  ),
}));

function detail(
  over: Partial<CustomAppDetail["app"]>,
  html: string,
): CustomAppDetail {
  return {
    app: {
      id: "app_abc",
      slug: "x",
      name: "Open Tasks",
      icon: "📋",
      entry: "index.html",
      version: 1,
      createdBy: "app-builder",
      createdAt: "2026-06-29T10:00:00Z",
      updatedAt: "2026-06-29T10:00:00Z",
      contentHash: "h",
      ...over,
    },
    html,
  };
}

describe("OperatorAppDetail", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("shows the live preview (UI builds in front of you) while building", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "building" }, ""),
      isError: false,
    });
    const { getByTestId, queryByTestId } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    // The building app's UI tab shows the live dev-server preview, not the
    // sealed published frame.
    expect(getByTestId("live-preview").getAttribute("data-app-id")).toBe(
      "app_abc",
    );
    expect(queryByTestId("app-frame")).toBeNull();
  });

  it("renders the live app in the UI tab once ready", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "ready" }, "<html>hi</html>"),
      isError: false,
    });
    const { getByTestId } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    const frame = getByTestId("app-frame");
    expect(frame.getAttribute("data-app-id")).toBe("app_abc");
    expect(frame.textContent).toContain("<html>hi</html>");
  });

  it("routes the Routines tab to the agent's scheduled routines", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "ready" }, "<html>hi</html>"),
      isError: false,
    });
    const { getByRole, getByText, getAllByText } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    fireEvent.click(getByRole("tab", { name: "Routines" }));
    // A routine is a scheduled prompt with its own lifecycle controls.
    expect(getByText("Monday pipeline recap")).toBeTruthy();
    expect(getAllByText("Publish new version").length).toBeGreaterThan(0);
  });

  it("shows the Ask AI header button and floating bubble for a ready app", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "ready" }, "<html>hi</html>"),
      isError: false,
    });
    const { getByRole } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    // Header action button (exact name "Ask AI").
    expect(getByRole("button", { name: /^ask agent$/i })).toBeTruthy();
    // Floating bubble (aria-label "Ask AI about <app>").
    expect(
      getByRole("button", { name: /ask agent about open tasks/i }),
    ).toBeTruthy();
  });

  it("hides the Ask AI affordances while the app is still building", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "building" }, ""),
      isError: false,
    });
    const { queryByRole } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    expect(queryByRole("button", { name: /ask agent/i })).toBeNull();
  });

  it("appends the agent service's artifacts after the live app artifact", async () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "ready" }, "<html>hi</html>"),
      isError: false,
    });
    const fetchMock = vi.fn(async (url: string) => {
      if (url === "/agent/artifacts?agent=app_abc") {
        return {
          ok: true,
          json: async () => ({
            artifacts: [
              {
                id: "art_1",
                type: "md",
                title: "weekly-recap.md",
                producedBy: "Monday pipeline recap",
                at: "Monday 9:02",
                content: "# Recap",
              },
            ],
          }),
        };
      }
      // Any other call (e.g. the tools hydration) degrades gracefully.
      return { ok: false, status: 404, json: async () => ({}) };
    });
    vi.stubGlobal("fetch", fetchMock);
    const { container, findByText } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    // The persisted artifact renders in the strip…
    expect(await findByText("weekly-recap.md")).toBeTruthy();
    // …AFTER the live app artifact.
    const chips = container.querySelectorAll(".opr-artifact-chip");
    expect(chips.length).toBe(2);
    expect(chips[0].textContent).toContain("Open Tasks");
    expect(chips[1].textContent).toContain("weekly-recap.md");
  });

  it("opens Ask AI as a docked drawer (not full screen) when clicked", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "ready" }, "<html>hi</html>"),
      isError: false,
    });
    const { getByRole, getByTestId, queryByTestId } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    // Drawer closed: only the floating bubble exists, no chat yet.
    expect(queryByTestId("ask-ai-chat")).toBeNull();
    fireEvent.click(
      getByRole("button", { name: /ask agent about open tasks/i }),
    );
    // Drawer open: the tools chat is mounted inside the docked panel.
    expect(getByTestId("ask-ai-chat").textContent).toContain(
      "tools:Open Tasks",
    );
  });
});
