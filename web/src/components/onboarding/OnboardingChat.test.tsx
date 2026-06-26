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

  it("translates phase enum into a short human label, not the raw enum", async () => {
    getMock.mockResolvedValue({ phase: "blueprint", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });
    await waitFor(() =>
      expect(screen.getByText(/Pick a starter/i)).toBeDefined(),
    );
    // Guard against regressing to the inconsistent step counter format.
    expect(screen.queryByText(/Step \d of \d/)).toBeNull();
  });

  it("labels the previously-uncovered website phase", async () => {
    getMock.mockResolvedValue({ phase: "website", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });
    await waitFor(() => expect(screen.getByText(/^Website$/)).toBeDefined());
  });

  it("labels the previously-uncovered scan phase", async () => {
    getMock.mockResolvedValue({ phase: "scan", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });
    await waitFor(() =>
      expect(screen.getByText(/Scanning your site/i)).toBeDefined(),
    );
  });

  it("labels the identity phase as 'What you do', not 'Who you are'", async () => {
    getMock.mockResolvedValue({ phase: "identity", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });
    await waitFor(() => expect(screen.getByText(/What you do/i)).toBeDefined());
    expect(screen.queryByText(/Who you are/i)).toBeNull();
  });

  it("shows a hint when no pending suggestion is present", async () => {
    getMock.mockResolvedValue({ phase: "greet", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });
    await waitFor(() => {
      expect(screen.getByText(/CEO is composing/)).toBeDefined();
    });
  });

  it("hides the hint when a pending suggestion exists", async () => {
    getMock.mockResolvedValue({
      phase: "greet",
      pending_suggestion: {
        id: "greet-1",
        kind: "ceo_form_field",
        question: "Office name?",
      },
    });
    render(<OnboardingChat />, { wrapper });
    // Wait for phase to settle, then assert hint is absent.
    await waitFor(() =>
      expect(
        screen.getByTestId("onboarding-chat").getAttribute("data-phase"),
      ).toBe("greet"),
    );
    expect(screen.queryByText(/CEO is composing/)).toBeNull();
  });

  it("renders without crash when state load is still pending", () => {
    getMock.mockReturnValue(new Promise(() => {}));
    render(<OnboardingChat />, { wrapper });
    expect(screen.getByTestId("onboarding-chat")).toBeDefined();
  });
});
