/**
 * RootRoute bootstrap fallback — regression for the blank page on a
 * broker 503. When the shell's bootstrap query (/onboarding/state)
 * fails because the broker is wedged or unreachable, the app must
 * render an honest full-page state with a retry affordance — never a
 * blank body, and never the fresh-install PrePick screen.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
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
vi.mock("../operator/OperatorApp", () => ({
  OperatorApp: () => <div data-testid="operator-stub" />,
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
import { useAppStore } from "../stores/app";
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
  // onboardingComplete lives in a module-level zustand store that React cleanup
  // does not reset; clear it so an onboarded-state test cannot leak into a
  // fresh-install test that expects the onboarding gate.
  useAppStore.setState({ onboardingComplete: false });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  window.location.hash = "";
});

describe("RootRoute bootstrap fallback", () => {
  it("auto-retries the bootstrap after BOOT_RETRY_MS without a click", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      // First attempt fails; the timer-driven second attempt succeeds.
      getMock.mockRejectedValueOnce(apiError(503, '{"error":"broker wedged"}'));
      getMock.mockResolvedValue({ complete: false });
      renderRoot();
      const fallback = await screen.findByTestId("broker-unreachable");
      expect(fallback).toBeInTheDocument();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
      });
      await waitFor(() =>
        expect(screen.queryByTestId("broker-unreachable")).toBeNull(),
      );
    } finally {
      vi.useRealTimers();
    }
  });

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

  it("does not boot the office broker on the /#/operator route", async () => {
    // The operator shell is self-contained; gating must keep the bootstrap from
    // firing initApi()/onboarding traffic and the failing retry loop.
    window.location.hash = "#/operator";

    renderRoot();

    expect(await screen.findByTestId("operator-stub")).toBeInTheDocument();
    expect(initApiMock).not.toHaveBeenCalled();
    expect(getMock).not.toHaveBeenCalled();
    expect(screen.queryByTestId("broker-unreachable")).not.toBeInTheDocument();
  });

  it("lands an onboarded home ('/') in the operator surface, not the office shell", async () => {
    // Operator is the index: a fresh, onboarded user opening the root URL gets
    // the operator product, not the legacy office chat shell.
    window.location.hash = "";
    getMock.mockResolvedValue({ onboarded: true });

    renderRoot();

    expect(await screen.findByTestId("operator-stub")).toBeInTheDocument();
    expect(screen.queryByTestId("shell-stub")).not.toBeInTheDocument();
  });

  it("gates the operator index behind onboarding (reuses the wizard)", async () => {
    // A not-yet-onboarded user at home sees the onboarding gate first, then
    // lands in operator once onboarding completes.
    window.location.hash = "";
    getMock.mockRejectedValue(apiError(404, "not found"));

    renderRoot();

    expect(await screen.findByTestId("prepick-stub")).toBeInTheDocument();
    expect(screen.queryByTestId("operator-stub")).not.toBeInTheDocument();
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
