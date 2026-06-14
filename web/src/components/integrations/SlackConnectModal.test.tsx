import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../api/slackOnboarding", () => ({
  getSlackAppManifest: vi.fn(),
  saveSlackTokens: vi.fn(),
  connectSlackChannel: vi.fn(),
  getSlackOnboardingStatus: vi.fn(),
  discoverSlackBots: vi.fn(),
  connectSlackAgent: vi.fn(),
}));
vi.mock("../ui/Toast", () => ({ showNotice: vi.fn() }));

import {
  connectSlackAgent,
  connectSlackChannel,
  discoverSlackBots,
  getSlackAppManifest,
  getSlackOnboardingStatus,
  saveSlackTokens,
} from "../../api/slackOnboarding";
import { SlackConnectModal } from "./SlackConnectModal";

const manifest = {
  manifest_json: '{\n  "display_information": { "name": "WUPHF Office" }\n}',
  create_url: "https://api.slack.com/apps?new_app=1",
  guide: ["step one"],
};

describe("SlackConnectModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (getSlackAppManifest as ReturnType<typeof vi.fn>).mockResolvedValue(
      manifest,
    );
    (saveSlackTokens as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      bot_user_id: "U0BOT",
      bot_name: "wuphf",
      workspace: "Acme",
    });
    (connectSlackChannel as ReturnType<typeof vi.fn>).mockResolvedValue({
      channel_slug: "slack-team",
      name: "team",
    });
    (getSlackOnboardingStatus as ReturnType<typeof vi.fn>).mockResolvedValue({
      bot_token_set: true,
      app_token_set: true,
      channel_connected: true,
      channel_slug: "slack-team",
      transport_connected: true,
      ready: true,
    });
    // Default: no other agents in the channel → wizard goes straight to done.
    (discoverSlackBots as ReturnType<typeof vi.fn>).mockResolvedValue({
      channel_id: "C0ABCDE123",
      bots: [],
    });
    (connectSlackAgent as ReturnType<typeof vi.fn>).mockResolvedValue({
      slug: "researcher",
      name: "Researcher",
      user_id: "U0BOT1",
      created: true,
    });
  });

  it("renders nothing when closed", () => {
    const { container } = render(
      <SlackConnectModal open={false} onClose={() => {}} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("walks intro → create and loads the manifest", async () => {
    render(<SlackConnectModal open={true} onClose={() => {}} />);
    expect(screen.getByTestId("sc-step-intro")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("sc-start"));
    expect(screen.getByTestId("sc-step-create")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId("sc-manifest").textContent).toContain(
        "WUPHF Office",
      ),
    );
    expect(getSlackAppManifest).toHaveBeenCalled();
  });

  it("gates token verification on xoxb-/xapp- prefixes and persists on success", async () => {
    render(<SlackConnectModal open={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));

    const verify = screen.getByTestId("sc-verify");
    expect(verify).toBeDisabled();

    fireEvent.change(screen.getByTestId("sc-bot-token"), {
      target: { value: "not-a-token" },
    });
    fireEvent.change(screen.getByTestId("sc-app-token"), {
      target: { value: "xapp-1" },
    });
    expect(verify).toBeDisabled();

    fireEvent.change(screen.getByTestId("sc-bot-token"), {
      target: { value: "xoxb-real" },
    });
    expect(verify).not.toBeDisabled();

    fireEvent.click(verify);
    await waitFor(() =>
      expect(saveSlackTokens).toHaveBeenCalledWith("xoxb-real", "xapp-1"),
    );
    await waitFor(() =>
      expect(screen.getByTestId("sc-identity")).toHaveTextContent(/wuphf/),
    );
    expect(screen.getByTestId("sc-step-channel")).toBeInTheDocument();
  });

  it("surfaces a token error and stays on the tokens step", async () => {
    (saveSlackTokens as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("Slack rejected that bot token"),
    );
    render(<SlackConnectModal open={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));
    fireEvent.change(screen.getByTestId("sc-bot-token"), {
      target: { value: "xoxb-bad" },
    });
    fireEvent.change(screen.getByTestId("sc-app-token"), {
      target: { value: "xapp-bad" },
    });
    fireEvent.click(screen.getByTestId("sc-verify"));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/Slack rejected/),
    );
    expect(screen.getByTestId("sc-step-tokens")).toBeInTheDocument();
  });

  it("activates: connects the channel and hot-starts the bridge (no restart)", async () => {
    render(<SlackConnectModal open={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));
    fireEvent.change(screen.getByTestId("sc-bot-token"), {
      target: { value: "xoxb-real" },
    });
    fireEvent.change(screen.getByTestId("sc-app-token"), {
      target: { value: "xapp-real" },
    });
    fireEvent.click(screen.getByTestId("sc-verify"));
    await screen.findByTestId("sc-step-channel");

    const activate = screen.getByTestId("sc-activate");
    expect(activate).toBeDisabled();
    fireEvent.change(screen.getByTestId("sc-channel-id"), {
      target: { value: "C0ABCDE123" },
    });
    expect(activate).not.toBeDisabled();

    fireEvent.click(activate);
    expect(screen.getByTestId("sc-step-activating")).toBeInTheDocument();
    await waitFor(() =>
      expect(connectSlackChannel).toHaveBeenCalledWith("C0ABCDE123", undefined),
    );
    // The wizard polls /slack/status for a live transport instead of restarting.
    await waitFor(() => expect(getSlackOnboardingStatus).toHaveBeenCalled());
    await screen.findByTestId("sc-step-done", undefined, { timeout: 5000 });
  });

  it("waits for the transport to connect before going live", async () => {
    // First probe: tokens + channel saved but the Socket Mode link isn't up yet.
    // Second probe: transport connected → ready.
    (getSlackOnboardingStatus as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce({
        bot_token_set: true,
        app_token_set: true,
        channel_connected: true,
        channel_slug: "slack-team",
        transport_connected: false,
        ready: false,
      })
      .mockResolvedValue({
        bot_token_set: true,
        app_token_set: true,
        channel_connected: true,
        channel_slug: "slack-team",
        transport_connected: true,
        ready: true,
      });

    render(<SlackConnectModal open={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));
    fireEvent.change(screen.getByTestId("sc-bot-token"), {
      target: { value: "xoxb-real" },
    });
    fireEvent.change(screen.getByTestId("sc-app-token"), {
      target: { value: "xapp-real" },
    });
    fireEvent.click(screen.getByTestId("sc-verify"));
    await screen.findByTestId("sc-step-channel");
    fireEvent.change(screen.getByTestId("sc-channel-id"), {
      target: { value: "C0ABCDE123" },
    });
    fireEvent.click(screen.getByTestId("sc-activate"));

    await screen.findByTestId("sc-step-done", undefined, { timeout: 5000 });
    expect(getSlackOnboardingStatus).toHaveBeenCalledTimes(2);
  });

  it("discovers other AI agents and connects them on the team step", async () => {
    (discoverSlackBots as ReturnType<typeof vi.fn>).mockResolvedValue({
      channel_id: "C0ABCDE123",
      bots: [
        { user_id: "U0BOT1", name: "Researcher", already_registered: false },
        { user_id: "U0BOT2", name: "Closer", already_registered: false },
      ],
    });

    render(<SlackConnectModal open={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));
    fireEvent.change(screen.getByTestId("sc-bot-token"), {
      target: { value: "xoxb-real" },
    });
    fireEvent.change(screen.getByTestId("sc-app-token"), {
      target: { value: "xapp-real" },
    });
    fireEvent.click(screen.getByTestId("sc-verify"));
    await screen.findByTestId("sc-step-channel");
    fireEvent.change(screen.getByTestId("sc-channel-id"), {
      target: { value: "C0ABCDE123" },
    });
    fireEvent.click(screen.getByTestId("sc-activate"));

    // Discovery found two agents → the team step appears with both listed.
    await screen.findByTestId("sc-step-team", undefined, { timeout: 5000 });
    expect(screen.getByTestId("sc-agent-U0BOT1")).toBeInTheDocument();
    expect(screen.getByTestId("sc-agent-U0BOT2")).toBeInTheDocument();
    await waitFor(() =>
      expect(discoverSlackBots).toHaveBeenCalledWith("C0ABCDE123"),
    );

    // Both default-selected → "Connect" registers both, then we go live.
    fireEvent.click(screen.getByTestId("sc-connect-agents"));
    await screen.findByTestId("sc-step-done", undefined, { timeout: 5000 });
    expect(connectSlackAgent).toHaveBeenCalledTimes(2);
    expect(connectSlackAgent).toHaveBeenCalledWith("U0BOT1", "Researcher");
    expect(connectSlackAgent).toHaveBeenCalledWith("U0BOT2", "Closer");
  });

  it("skips the team step when no other agents are in the channel", async () => {
    // discoverSlackBots default returns no bots → straight to done, no team step.
    render(<SlackConnectModal open={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));
    fireEvent.change(screen.getByTestId("sc-bot-token"), {
      target: { value: "xoxb-real" },
    });
    fireEvent.change(screen.getByTestId("sc-app-token"), {
      target: { value: "xapp-real" },
    });
    fireEvent.click(screen.getByTestId("sc-verify"));
    await screen.findByTestId("sc-step-channel");
    fireEvent.change(screen.getByTestId("sc-channel-id"), {
      target: { value: "C0ABCDE123" },
    });
    fireEvent.click(screen.getByTestId("sc-activate"));

    await screen.findByTestId("sc-step-done", undefined, { timeout: 5000 });
    expect(connectSlackAgent).not.toHaveBeenCalled();
  });
});
