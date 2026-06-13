import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../api/slackOnboarding", () => ({
  getSlackAppManifest: vi.fn(),
  saveSlackTokens: vi.fn(),
  connectSlackChannel: vi.fn(),
  getSlackOnboardingStatus: vi.fn(),
  restartBroker: vi.fn(),
}));
vi.mock("../ui/Toast", () => ({ showNotice: vi.fn() }));

import {
  connectSlackChannel,
  getSlackAppManifest,
  getSlackOnboardingStatus,
  restartBroker,
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
    (getSlackAppManifest as ReturnType<typeof vi.fn>).mockResolvedValue(manifest);
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
    (restartBroker as ReturnType<typeof vi.fn>).mockResolvedValue({});
    (getSlackOnboardingStatus as ReturnType<typeof vi.fn>).mockResolvedValue({
      bot_token_set: true,
      app_token_set: true,
      channel_connected: true,
      channel_slug: "slack-team",
      ready: true,
    });
  });

  it("renders nothing when closed", () => {
    const { container } = render(<SlackConnectModal open={false} onClose={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("walks intro → create and loads the manifest", async () => {
    render(<SlackConnectModal open onClose={() => {}} />);
    expect(screen.getByTestId("sc-step-intro")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("sc-start"));
    expect(screen.getByTestId("sc-step-create")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId("sc-manifest").textContent).toContain("WUPHF Office"),
    );
    expect(getSlackAppManifest).toHaveBeenCalled();
  });

  it("gates token verification on xoxb-/xapp- prefixes and persists on success", async () => {
    render(<SlackConnectModal open onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));

    const verify = screen.getByTestId("sc-verify");
    expect(verify).toBeDisabled();

    fireEvent.change(screen.getByTestId("sc-bot-token"), { target: { value: "not-a-token" } });
    fireEvent.change(screen.getByTestId("sc-app-token"), { target: { value: "xapp-1" } });
    expect(verify).toBeDisabled();

    fireEvent.change(screen.getByTestId("sc-bot-token"), { target: { value: "xoxb-real" } });
    expect(verify).not.toBeDisabled();

    fireEvent.click(verify);
    await waitFor(() => expect(saveSlackTokens).toHaveBeenCalledWith("xoxb-real", "xapp-1"));
    await waitFor(() => expect(screen.getByTestId("sc-identity")).toHaveTextContent(/wuphf/));
    expect(screen.getByTestId("sc-step-channel")).toBeInTheDocument();
  });

  it("surfaces a token error and stays on the tokens step", async () => {
    (saveSlackTokens as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("Slack rejected that bot token"),
    );
    render(<SlackConnectModal open onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));
    fireEvent.change(screen.getByTestId("sc-bot-token"), { target: { value: "xoxb-bad" } });
    fireEvent.change(screen.getByTestId("sc-app-token"), { target: { value: "xapp-bad" } });
    fireEvent.click(screen.getByTestId("sc-verify"));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/Slack rejected/));
    expect(screen.getByTestId("sc-step-tokens")).toBeInTheDocument();
  });

  it("activates: connects the channel and restarts the bridge", async () => {
    render(<SlackConnectModal open onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("sc-start"));
    fireEvent.click(screen.getByTestId("sc-created"));
    fireEvent.change(screen.getByTestId("sc-bot-token"), { target: { value: "xoxb-real" } });
    fireEvent.change(screen.getByTestId("sc-app-token"), { target: { value: "xapp-real" } });
    fireEvent.click(screen.getByTestId("sc-verify"));
    await screen.findByTestId("sc-step-channel");

    const activate = screen.getByTestId("sc-activate");
    expect(activate).toBeDisabled();
    fireEvent.change(screen.getByTestId("sc-channel-id"), { target: { value: "C0ABCDE123" } });
    expect(activate).not.toBeDisabled();

    fireEvent.click(activate);
    expect(screen.getByTestId("sc-step-activating")).toBeInTheDocument();
    await waitFor(() => expect(connectSlackChannel).toHaveBeenCalledWith("C0ABCDE123", undefined));
    await waitFor(() => expect(restartBroker).toHaveBeenCalled());
    await screen.findByTestId("sc-step-done", undefined, { timeout: 5000 });
  });
});
