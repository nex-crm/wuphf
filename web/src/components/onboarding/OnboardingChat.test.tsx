/**
 * OnboardingChat tests
 *
 * The wizard renders outside the office Shell. Pins the structural
 * contract:
 *   - data-phase reflects the current onboarding phase
 *   - phase labels read like a wizard ("Step N of 5 · …") not raw enum
 *     names
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

  it("translates phase enum into a wizard-style step label", async () => {
    getMock.mockResolvedValue({ phase: "blueprint", pending_suggestion: null });
    render(<OnboardingChat />, { wrapper });
    await waitFor(() => expect(screen.getByText(/Step 3 of 5/)).toBeDefined());
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
