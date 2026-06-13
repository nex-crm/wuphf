import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

// ── Hoist mocks before imports ───────────────────────────────────

const navigateMock = vi.hoisted(() => vi.fn());
const useOfficeMembersMock = vi.hoisted(() => vi.fn());
const getSkillsListMock = vi.hoisted(() => vi.fn());
const getOfficeTasksMock = vi.hoisted(() => vi.fn());
const getChannelsMock = vi.hoisted(() => vi.fn());
const listAgentLogTasksMock = vi.hoisted(() => vi.fn());
const getPoliciesMock = vi.hoisted(() => vi.fn());
const getConfigMock = vi.hoisted(() => vi.fn());
const getLocalProvidersStatusMock = vi.hoisted(() => vi.fn());

vi.mock("../../lib/router", () => ({
  router: { navigate: navigateMock },
}));

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: useOfficeMembersMock,
}));

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getSkillsList: getSkillsListMock,
    getChannels: getChannelsMock,
    getConfig: getConfigMock,
    getLocalProvidersStatus: getLocalProvidersStatusMock,
  };
});

vi.mock("../../api/tasks", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/tasks")>("../../api/tasks");
  return {
    ...actual,
    getOfficeTasks: getOfficeTasksMock,
    listAgentLogTasks: listAgentLogTasksMock,
  };
});

vi.mock("../../api/policies", async () => ({
  getPolicies: getPoliciesMock,
  policyAppliesToAgent: (p: { agents?: string[] }, slug: string) =>
    !p.agents || p.agents.length === 0 || p.agents.includes(slug),
  createPolicy: vi.fn(),
  deactivatePolicy: vi.fn(),
  unassignPolicyAgent: vi.fn(),
  assignPolicyAgent: vi.fn(),
}));

vi.mock("../../hooks/useConfig", () => ({
  useDefaultHarness: () => "claude-code",
}));

vi.mock("../../lib/harness", () => ({
  resolveHarness: (_provider: unknown, fallback: string) => fallback,
  isGatewayBinding: () => false,
}));

vi.mock("../../hooks/useOfficeTasks", () => ({
  useOfficeTasks: () => ({ data: [], isLoading: false }),
}));

vi.mock("../ui/PixelAvatar", () => ({
  PixelAvatar: ({ slug }: { slug: string }) => (
    <span data-testid={`avatar-${slug}`} />
  ),
}));

vi.mock("../ui/HarnessBadge", () => ({
  HarnessBadge: () => null,
}));

// Mock heavy children so tests stay fast. The Chat tab is now a pure chat
// surface: the shared MessageFeed + Composer pointed at the agent's DM channel.
vi.mock("../messages/MessageFeed", () => ({
  MessageFeed: ({ channel }: { channel?: string }) => (
    <div data-testid="message-feed" data-channel={channel} />
  ),
}));

vi.mock("../messages/Composer", () => ({
  Composer: ({ channel }: { channel?: string }) => (
    <div data-testid="composer" data-channel={channel} />
  ),
}));

vi.mock("../../hooks/useAgentStream", () => ({
  useAgentStream: () => ({ lines: [], connected: false }),
}));

vi.mock("../../stores/app", () => ({
  directChannelSlug: (slug: string) => `human__${slug}`,
  useAppStore: (sel: (s: { brokerConnected: boolean }) => unknown) =>
    sel({ brokerConnected: true }),
}));

vi.mock("./AgentInstructionsSection", () => ({
  AgentInstructionsSection: () => (
    <div data-testid="agent-instructions-section" />
  ),
}));

vi.mock("../ui/ConfirmDialog", () => ({
  confirm: vi.fn(),
  ConfirmHost: () => null,
}));

vi.mock("../apps/skills/PixelSkillCard", () => ({
  PixelSkillCard: ({ skill }: { skill: { name: string } }) => (
    <div data-testid={`skill-card-${skill.name}`} />
  ),
}));

import type { OfficeMember } from "../../api/client";
import { AGENT_TABS, AgentSubspace } from "./AgentSubspace";

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function wrap(ui: ReactNode, qc = makeQC()) {
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

const baseAgent: OfficeMember = {
  slug: "planner",
  name: "Planner",
  role: "Plans tasks",
  status: "idle",
};

describe("<AgentSubspace>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useOfficeMembersMock.mockReturnValue({ data: [baseAgent] });
    getSkillsListMock.mockResolvedValue({ skills: [] });
    getChannelsMock.mockResolvedValue({ channels: [] });
    getOfficeTasksMock.mockResolvedValue({ tasks: [] });
    listAgentLogTasksMock.mockResolvedValue({ tasks: [] });
    getPoliciesMock.mockResolvedValue([]);
    getConfigMock.mockResolvedValue({
      llm_provider: "claude-code",
      llm_provider_kinds: ["claude-code", "codex"],
    });
    getLocalProvidersStatusMock.mockResolvedValue([]);
  });

  it("renders all 6 tabs in order", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="chat" />));

    const tabs = screen.getAllByRole("tab");
    expect(tabs).toHaveLength(AGENT_TABS.length);
    const labels = tabs.map((t) => t.textContent?.trim());
    expect(labels).toEqual([
      "Chat",
      "Tasks",
      "Skills",
      "Policies",
      "Live Stream",
      "Config",
    ]);
  });

  it("defaults to Chat tab (aria-selected=true on Chat)", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="chat" />));

    const chatTab = screen.getByRole("tab", { name: "Chat" });
    expect(chatTab).toHaveAttribute("aria-selected", "true");
  });

  it("renders a pure chat surface (feed + composer on the DM channel) when Chat is active", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="chat" />));

    // Pure chat only — no workbench (Tasks/Live Stream have their own tabs).
    expect(screen.getByTestId("message-feed")).toHaveAttribute(
      "data-channel",
      "human__planner",
    );
    expect(screen.getByTestId("composer")).toHaveAttribute(
      "data-channel",
      "human__planner",
    );
  });

  it("resolves the /live alias to the Live Stream tab (no silent Chat fallback)", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="live" />));

    expect(screen.getByRole("tab", { name: "Live Stream" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("tab", { name: "Chat" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
  });

  it("renames the agent from the persistent shell header (parity with old panel)", async () => {
    const user = userEvent.setup();
    render(wrap(<AgentSubspace agent={baseAgent} tab="chat" />));

    // The header shows the name as a click-to-edit control (EditableName).
    // Clicking it must open an inline text input — the rename affordance the
    // old AgentProfilePanel header had, relocated to the shell header.
    const nameButton = screen.getByRole("button", { name: /planner/i });
    await user.click(nameButton);

    const input = await screen.findByRole("textbox", { name: /agent name/i });
    expect(input).toBeInTheDocument();
    expect(input).toHaveValue("Planner");
  });

  it("marks the active tab as aria-selected", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="tasks" />));

    const tasksTab = screen.getByRole("tab", { name: "Tasks" });
    expect(tasksTab).toHaveAttribute("aria-selected", "true");

    const chatTab = screen.getByRole("tab", { name: "Chat" });
    expect(chatTab).toHaveAttribute("aria-selected", "false");
  });

  it("navigates when a tab is clicked", async () => {
    const user = userEvent.setup();
    render(wrap(<AgentSubspace agent={baseAgent} tab="chat" />));

    await user.click(screen.getByRole("tab", { name: "Tasks" }));

    expect(navigateMock).toHaveBeenCalledWith({
      to: "/agents/$agentSlug/$tab",
      params: { agentSlug: "planner", tab: "tasks" },
    });
  });

  it("renders Config tab without losing sections (headless AgentProfilePanel)", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="config" />));

    // Config tab content should be present
    const configPanel = screen.getByTestId("config-tab");
    expect(configPanel).toBeInTheDocument();
    // The close button should NOT appear (headless mode)
    expect(
      screen.queryByLabelText("Close agent profile"),
    ).not.toBeInTheDocument();
  });

  it("renders Skills tab content", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="skills" />));
    expect(screen.getByTestId("skills-tab")).toBeInTheDocument();
  });

  it("renders Policies tab content", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="policies" />));
    expect(screen.getByTestId("policies-tab")).toBeInTheDocument();
  });

  it("renders Live Stream tab content", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="live-stream" />));
    expect(screen.getByTestId("live-stream-tab")).toBeInTheDocument();
  });

  it("falls back to chat tab for unknown tab value", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="unknown-tab" />));

    const chatTab = screen.getByRole("tab", { name: "Chat" });
    expect(chatTab).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("message-feed")).toBeInTheDocument();
  });

  it("renders the agent name in the shell header", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="chat" />));
    // EditableName renders the name as a button with the agent's name
    expect(screen.getByText("Planner")).toBeInTheDocument();
  });

  it("renders the agent avatar in the shell header", () => {
    render(wrap(<AgentSubspace agent={baseAgent} tab="chat" />));
    expect(screen.getByTestId("avatar-planner")).toBeInTheDocument();
  });
});
