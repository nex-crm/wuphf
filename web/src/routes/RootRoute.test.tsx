/**
 * RootRoute bootstrap fallback — regression for the blank page on a
 * broker 503. When the shell's bootstrap query (/onboarding/state)
 * fails because the broker is wedged or unreachable, the app must
 * render an honest full-page state with a retry affordance — never a
 * blank body, and never the fresh-install PrePick screen.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Keep the test surface to RootRoute's own boot logic: stub the heavy app
// chrome (Shell pulls the whole sidebar/composer stack, WikiTabs pulls Pam,
// etc.) and the SSE/keyboard hooks so nothing opens sockets or timers.
vi.mock("../components/layout/Shell", () => ({
  Shell: ({ children }: { children?: React.ReactNode }) => (
    <div data-testid="shell-stub">{children}</div>
  ),
}));
vi.mock("../components/layout/UpgradeBanner", () => ({
  UpgradeBanner: () => null,
}));
vi.mock("../components/messages/ChannelParticipants", () => ({
  ChannelParticipants: () => null,
}));
vi.mock("../components/messages/Composer", () => ({ Composer: () => null }));
vi.mock("../components/messages/MessageFeed", () => ({
  MessageFeed: () => null,
}));
vi.mock("../components/wiki/WikiTabs", () => ({
  __esModule: true,
  default: () => null,
}));
vi.mock("../components/onboarding/PrePickScreen", () => ({
  PrePickScreen: () => <div data-testid="prepick-stub" />,
}));
vi.mock("../components/onboarding/wizard/OnboardingWizard", () => ({
  OnboardingWizard: () => <div data-testid="wizard-stub" />,
}));
vi.mock("../components/onboarding/tour/OfficeTour", () => ({
  OfficeTour: () => null,
}));
vi.mock("../components/integrations/TelegramConnectModal", () => ({
  TelegramConnectHost: () => null,
}));
vi.mock("../components/ui/ConfirmDialog", () => ({
  ConfirmHost: () => null,
  confirm: vi.fn(),
}));
vi.mock("../components/ui/ProviderSwitcher", () => ({
  ProviderSwitcherHost: () => null,
  openProviderSwitcher: vi.fn(),
}));
vi.mock("../components/ui/Toast", () => ({
  ToastContainer: () => null,
  showNotice: vi.fn(),
}));
vi.mock("../hooks/useBrokerEvents", () => ({ useBrokerEvents: vi.fn() }));
vi.mock("../hooks/useKeyboardShortcuts", () => ({
  useKeyboardShortcuts: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    initApi: vi.fn(),
    get: vi.fn(),
  };
});

import { ApiError, get, initApi } from "../api/client";
import RootRoute from "./RootRoute";

const initApiMock = vi.mocked(initApi);
const getMock = vi.mocked(get);

function apiError(status: number, bodyText = ""): ApiError {
  return new ApiError({ status, statusText: "err", bodyText });
}

function renderRoot() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <RootRoute />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  initApiMock.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("RootRoute bootstrap fallback", () => {
  it("renders the broker-unreachable state on a 503, not a blank body", async () => {
    getMock.mockRejectedValue(apiError(503, '{"error":"broker wedged"}'));

    const { container } = renderRoot();

    const fallback = await screen.findByTestId("broker-unreachable");
    expect(fallback.textContent).toMatch(
      /can.t reach the office broker — retrying/i,
    );
    expect(screen.getByTestId("broker-unreachable-retry")).toBeInTheDocument();
    // Never a blank body, and never the misleading fresh-install screen.
    expect(container.textContent?.trim().length).toBeGreaterThan(0);
    expect(screen.queryByTestId("prepick-stub")).not.toBeInTheDocument();
  });

  it("renders the same fallback when the bootstrap query times out", async () => {
    getMock.mockRejectedValue(
      new Error("Broker not responding — request timed out."),
    );

    renderRoot();

    expect(await screen.findByTestId("broker-unreachable")).toBeInTheDocument();
  });

  it("falls through to onboarding (not the fallback) when the broker answers 404", async () => {
    getMock.mockRejectedValue(apiError(404, "not found"));

    renderRoot();

    expect(await screen.findByTestId("prepick-stub")).toBeInTheDocument();
    expect(screen.queryByTestId("broker-unreachable")).not.toBeInTheDocument();
  });

  it("recovers via the retry button once the broker answers again", async () => {
    getMock.mockRejectedValueOnce(apiError(503, "wedged"));

    renderRoot();
    await screen.findByTestId("broker-unreachable");

    // Broker comes back but onboarding is not mounted yet (fresh install):
    // retry must leave the fallback and land on the onboarding gate.
    getMock.mockRejectedValue(apiError(404, "not found"));
    fireEvent.click(screen.getByTestId("broker-unreachable-retry"));

    expect(await screen.findByTestId("prepick-stub")).toBeInTheDocument();
    await waitFor(() => {
      expect(
        screen.queryByTestId("broker-unreachable"),
      ).not.toBeInTheDocument();
    });
  });
});
