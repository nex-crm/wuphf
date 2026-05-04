import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  getHealth,
  getHumanMe,
  getHumanSessions,
  getShareStatus,
  revokeHumanSession,
  startShare,
  stopShare,
} from "../../api/platform";
import { useAppStore } from "../../stores/app";
import { HealthCheckApp, selfAccessDetails } from "./HealthCheckApp";

vi.mock("../../api/platform", () => ({
  getHealth: vi.fn(),
  getHumanMe: vi.fn(),
  getHumanSessions: vi.fn(),
  getShareStatus: vi.fn(),
  revokeHumanSession: vi.fn(),
  startShare: vi.fn(),
  stopShare: vi.fn(),
}));

const getHealthMock = vi.mocked(getHealth);
const getHumanMeMock = vi.mocked(getHumanMe);
const getHumanSessionsMock = vi.mocked(getHumanSessions);
const getShareStatusMock = vi.mocked(getShareStatus);
const revokeHumanSessionMock = vi.mocked(revokeHumanSession);
const startShareMock = vi.mocked(startShare);
const stopShareMock = vi.mocked(stopShare);

function wrap(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ brokerConnected: true });
  getHealthMock.mockResolvedValue({
    status: "ok",
    session_mode: "office",
    one_on_one_agent: "",
    focus_mode: false,
    provider: "codex",
    provider_model: "gpt-5.5",
    memory_backend: "markdown",
    memory_backend_active: "markdown",
    memory_backend_ready: true,
    nex_connected: false,
    build: {
      version: "0.114.1",
      build_timestamp: "2026-05-04T18:12:38Z",
    },
  });
  getHumanMeMock.mockResolvedValue({
    human: {
      role: "host",
      display_name: "Maya",
      slug: "maya",
    },
  });
  getHumanSessionsMock.mockResolvedValue({
    sessions: [
      {
        id: "session-1",
        invite_id: "invite-1",
        human_slug: "tara",
        display_name: "Tara",
        device: "browser",
        created_at: "2026-05-04T18:00:00Z",
        expires_at: "2026-05-05T18:00:00Z",
        last_seen_at: "2026-05-04T18:05:00Z",
      },
    ],
  });
  getShareStatusMock.mockResolvedValue({ running: false });
  revokeHumanSessionMock.mockResolvedValue({ ok: true });
  startShareMock.mockResolvedValue({
    running: true,
    bind: "100.64.0.2",
    interface: "tailscale0",
    invite_url: "http://100.64.0.2:7890/join/invite-token",
    expires_at: "2026-05-05T18:00:00Z",
  });
  stopShareMock.mockResolvedValue({ running: false });
});

describe("HealthCheckApp access and sharing", () => {
  it("renders host connection health and active team-member sessions", async () => {
    render(wrap(<HealthCheckApp />));

    expect(await screen.findByText("Access & Health")).toBeInTheDocument();
    expect(screen.getByText("Signed in as Maya")).toBeInTheDocument();
    expect(screen.getByText("Live event stream")).toBeInTheDocument();
    expect(await screen.findByText("Tara")).toBeInTheDocument();
    expect(screen.getByText("Team-member sessions (1)")).toBeInTheDocument();
  });

  it("creates and stops a scoped team-member invite from the host UI", async () => {
    const user = userEvent.setup();
    render(wrap(<HealthCheckApp />));

    await user.click(
      await screen.findByRole("button", { name: "Create invite" }),
    );

    await waitFor(() => expect(startShareMock).toHaveBeenCalledTimes(1));
    expect(
      await screen.findByText("http://100.64.0.2:7890/join/invite-token"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Sharing on tailscale0 / 100.64.0.2"),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Stop sharing" }));

    await waitFor(() => expect(stopShareMock).toHaveBeenCalledTimes(1));
  });

  it("disconnects an active team-member session from the host UI", async () => {
    const user = userEvent.setup();
    render(wrap(<HealthCheckApp />));

    await screen.findByText("Tara");
    await user.click(screen.getByRole("button", { name: "Disconnect Tara" }));

    await waitFor(() =>
      expect(revokeHumanSessionMock).toHaveBeenCalledWith("session-1"),
    );
    await waitFor(() =>
      expect(
        screen.getByText("No active team-member browser sessions."),
      ).toBeInTheDocument(),
    );
  });

  it("keeps the disconnect action available after a failed revoke", async () => {
    revokeHumanSessionMock.mockRejectedValueOnce(new Error("network down"));
    const user = userEvent.setup();
    render(wrap(<HealthCheckApp />));

    await screen.findByText("Tara");
    await user.click(screen.getByRole("button", { name: "Disconnect Tara" }));

    expect(await screen.findByText("network down")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Disconnect Tara" }),
    ).not.toBeDisabled();
  });

  it("hides invite controls from team-member sessions", async () => {
    getHumanMeMock.mockResolvedValue({
      human: {
        role: "member",
        display_name: "Tara",
        human_slug: "tara",
      },
    });

    render(wrap(<HealthCheckApp />));

    expect(await screen.findByText("Signed in as Tara")).toBeInTheDocument();
    expect(
      screen.getByText("Team-member invites are host-only."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create invite" })).toBeNull();
    expect(getShareStatusMock).not.toHaveBeenCalled();
    expect(getHumanSessionsMock).not.toHaveBeenCalled();
  });

  it("describes network web access without forcing SSH when already remote", () => {
    expect(selfAccessDetails("localhost", "http://localhost:7890")).toEqual({
      detail:
        "For a server you reach through SSH, keep the tunnel open while you work.",
      code: "ssh -L 7890:localhost:7890 user@server",
      footer: "Then open http://localhost:7890",
    });
    expect(selfAccessDetails("100.64.0.2", "http://100.64.0.2:7890")).toEqual({
      detail: "This browser is already connected through the network web UI.",
      code: "http://100.64.0.2:7890",
      footer: "Use team-member invites for scoped shared sessions.",
    });
  });
});
