import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { CustomAppDetail } from "../../api/apps";
import { OperatorAppDetail } from "./OperatorAppDetail";

// Control the data hook so we can render the building vs ready states without a
// network or React Query provider.
const useOperatorAppMock = vi.fn();
vi.mock("../apps/useOperatorApps", () => ({
  useOperatorApp: (id: string) => useOperatorAppMock(id),
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
vi.mock("./ToolIntegrations", () => ({
  ToolIntegrations: () => <div data-testid="tool-integrations" />,
}));
vi.mock("./AppBuilderChat", () => ({
  AppBuilderChat: ({ editApp }: { editApp?: { name: string } }) => (
    <div data-testid="ask-ai-chat">edit:{editApp?.name}</div>
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
  it("shows the building state while the first version publishes", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "building" }, ""),
      isError: false,
    });
    const { getByText, queryByTestId } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    expect(getByText(/building your app/i)).toBeTruthy();
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

  it("offers Slack delivery scheduling on the Workflow tab of a ready app", () => {
    useOperatorAppMock.mockReturnValue({
      data: detail({ status: "ready" }, "<html>hi</html>"),
      isError: false,
    });
    const { getByText, getByRole } = render(
      <OperatorAppDetail appId="app_abc" onBack={() => {}} />,
    );
    fireEvent.click(getByRole("tab", { name: "Workflow" }));
    expect(getByText("Deliver to Slack")).toBeTruthy();
    expect(
      getByRole("button", { name: /schedule daily delivery/i }),
    ).toBeTruthy();
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
    fireEvent.click(getByRole("button", { name: /ask ai about open tasks/i }));
    // Drawer open: the edit chat is mounted inside the docked panel.
    expect(getByTestId("ask-ai-chat").textContent).toContain("edit:Open Tasks");
  });
});
