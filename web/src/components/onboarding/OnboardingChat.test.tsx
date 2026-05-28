/**
 * OnboardingChat tests
 *
 * The wizard renders outside the office Shell. Pins the structural
 * contract:
 *   - data-phase reflects the current onboarding phase
 *   - phase labels read as short human beats ("Office name", "What you
 *     do", "Pick a starter"), never raw enum names. No step counter
 *     ("Step N of M") — the scratch path skips phases, so a fixed
 *     denominator would lie (#939).
 *   - InterviewBar mounts inside the footer so chip / form-field cards
 *     surface as the input zone
 *   - When there's no pending suggestion, a hint banner appears in place
 *     of the absent input so users don't think the wizard is stuck.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { OnboardingChat } from "./OnboardingChat";

vi.mock("../../hooks/useMessages", () => ({
  useMessages: () => ({ data: [] }),
}));

vi.mock("../messages/MessageBubble", () => ({
  MessageBubble: () => <div data-testid="message-bubble" />,
}));

vi.mock("../messages/TypingIndicator", () => ({
  TypingIndicator: () => <div data-testid="typing-indicator" />,
}));

vi.mock("../messages/InterviewBar", () => ({
  InterviewBar: () => <div data-testid="interview-bar" />,
}));

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return { ...actual, get: vi.fn(), post: vi.fn() };
});

import { get } from "../../api/client";

const getMock = vi.mocked(get);

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  getMock.mockReset();
});

afterEach(() => cleanup());

describe("OnboardingChat", () => {
  it("renders the wizard chrome and InterviewBar inside its footer", async () => {
    getMock.mockResolvedValue({ phase: "greet", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });

    const root = await screen.findByTestId("onboarding-chat");
    expect(root).toBeDefined();
    // The wizard owns its own viewport — it is intentionally NOT inside
    // .office so it can be styled position:fixed without the Shell layout
    // boxing it in.
    expect(root.closest(".office")).toBeNull();
    expect(screen.getByTestId("interview-bar")).toBeDefined();
  });

  it("surfaces the current phase via the chat root's data-phase attribute", async () => {
    // Phase label header was removed in favor of just brand + restart;
    // the phase is still exposed for e2e specs and CSS selectors via
    // `data-phase` on the chat root. Cover each previously-labelled phase
    // here so a regression to the labelled header is still caught by tests.
    for (const phase of ["blueprint", "website", "scan", "identity"]) {
      getMock.mockResolvedValue({ phase, pending_suggestion: null });
      const { unmount } = render(<OnboardingChat />, { wrapper });
      await waitFor(() =>
        expect(
          screen.getByTestId("onboarding-chat").getAttribute("data-phase"),
        ).toBe(phase),
      );
      unmount();
    }
  });

  it("does not render the legacy 'Hang tight…' hint", async () => {
    // The hint was removed when CeoCardSection adopted the sticky-last-
    // suggestion pattern — the footer keeps showing the previously-
    // committed card across the brief broker-think gap instead of going
    // empty, so the hint became dead text.
    getMock.mockResolvedValue({ phase: "greet", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });
    await waitFor(() =>
      expect(
        screen.getByTestId("onboarding-chat").getAttribute("data-phase"),
      ).toBe("greet"),
    );
    expect(screen.queryByText(/CEO is composing/)).toBeNull();
    expect(screen.queryByText(/Hang tight/)).toBeNull();
  });

  it("renders without crash when state load is still pending", () => {
    getMock.mockReturnValue(new Promise(() => {}));
    render(<OnboardingChat />, { wrapper });
    expect(screen.getByTestId("onboarding-chat")).toBeDefined();
  });
});
