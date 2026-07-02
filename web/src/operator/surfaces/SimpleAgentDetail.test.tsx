import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SimpleAgentDetail } from "./SimpleAgentDetail";

// Control the data hook so we can render without a network or React Query
// provider (same seam as OperatorAppDetail.test.tsx).
const useOperatorAppMock = vi.fn();
vi.mock("../apps/useOperatorApps", () => ({
  useOperatorApp: (id: string) => useOperatorAppMock(id),
  appBuildState: (app: { status?: string }) =>
    app.status === "building" ? "building" : "ready",
  useDeleteApp: () => ({ mutate: vi.fn(), isPending: false }),
  isRealAppId: (id: string | null | undefined) =>
    typeof id === "string" && id.startsWith("app_"),
}));

// The three sections' bodies have their own tests; here we test the shell:
// exactly three sections, chat as the default main screen, chat kept mounted.
vi.mock("../agents/AgentSessions", () => ({
  AgentSessions: ({ agentName }: { agentName: string }) => (
    <div data-testid="agent-chat">chat:{agentName}</div>
  ),
}));
vi.mock("./AppToolsTab", () => ({
  AppToolsTab: ({ appName }: { appName: string }) => (
    <div data-testid="agent-tools">tools:{appName}</div>
  ),
}));
vi.mock("./ToolIntegrations", () => ({
  ToolIntegrations: ({ usedNames }: { usedNames: string[] }) => (
    <div data-testid="agent-integrations">{usedNames.join(",")}</div>
  ),
}));
const getMock = vi.fn();
vi.mock("../../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../api/client")>()),
  get: (path: string) => getMock(path),
}));

function withApp(status?: string) {
  useOperatorAppMock.mockReturnValue({
    data: {
      app: {
        id: "app_abc",
        slug: "pipeline-agent",
        name: "Pipeline Agent",
        icon: "📈",
        entry: "index.html",
        version: 2,
        status,
        createdBy: "app-builder",
        createdAt: "2026-06-29T10:00:00Z",
        updatedAt: "2026-06-29T10:00:00Z",
        contentHash: "h",
      },
      html: "<html></html>",
    },
    isError: false,
  });
}

describe("SimpleAgentDetail", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    getMock.mockReset();
  });

  it("has exactly three sections — Chat, Tools, Integrations", () => {
    withApp();
    getMock.mockResolvedValue({ connected: [] });
    const { getAllByRole } = render(
      <SimpleAgentDetail appId="app_abc" onBack={() => {}} />,
    );
    expect(getAllByRole("tab").map((t) => t.textContent)).toEqual([
      "Chat",
      "Tools",
      "Integrations",
    ]);
  });

  it("opens on the chat and keeps it mounted while viewing Tools", () => {
    withApp();
    getMock.mockResolvedValue({ connected: [] });
    const { getByTestId, getByRole, queryByTestId } = render(
      <SimpleAgentDetail appId="app_abc" onBack={() => {}} />,
    );
    const chat = getByTestId("agent-chat");
    expect(chat.textContent).toBe("chat:Pipeline Agent");
    expect(queryByTestId("agent-tools")).toBeNull();

    fireEvent.click(getByRole("tab", { name: "Tools" }));
    expect(getByTestId("agent-tools").textContent).toBe("tools:Pipeline Agent");
    // Chat pane is hidden, not unmounted — an in-flight conversation survives.
    expect(getByTestId("agent-chat")).toBeTruthy();
    expect(
      (getByTestId("agent-chat").parentElement as HTMLElement).style.display,
    ).toBe("none");
  });

  it("feeds the broker's connected platforms into the Integrations section", async () => {
    withApp();
    getMock.mockResolvedValue({
      connected: [
        { platform: "hubspot", name: "HubSpot", read_actions: [] },
        { platform: "slack", name: "Slack", read_actions: [] },
      ],
    });
    const { getByRole, getByTestId } = render(
      <SimpleAgentDetail appId="app_abc" onBack={() => {}} />,
    );
    fireEvent.click(getByRole("tab", { name: "Integrations" }));
    expect(getMock).toHaveBeenCalledWith("/apps/integrations/catalog");
    await waitFor(() =>
      expect(getByTestId("agent-integrations").textContent).toBe(
        "HubSpot,Slack",
      ),
    );
  });

  it("shows the building state in the header while the agent builds", () => {
    withApp("building");
    getMock.mockResolvedValue({ connected: [] });
    const { getByText } = render(
      <SimpleAgentDetail appId="app_abc" onBack={() => {}} />,
    );
    expect(getByText("Building")).toBeTruthy();
  });
});
