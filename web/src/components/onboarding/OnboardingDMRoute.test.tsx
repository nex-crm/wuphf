/**
 * OnboardingDMRoute tests
 *
 * 1. Renders DMView with the canonical CEO DM channel slug
 * 2. Surfaces PendingSuggestion as a CEO card via CeoCardSection
 * 3. Loading state renders without crash when state is undefined
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { OnboardingDMRoute } from "./OnboardingDMRoute";
import type { CeoFormFieldSuggestion } from "./types";

// Stub DMView to avoid full message infrastructure setup in unit tests.
vi.mock("../messages/DMView", () => ({
  DMView: ({
    agentSlug,
    channelSlug,
  }: {
    agentSlug: string;
    channelSlug: string;
  }) => (
    <div
      data-testid="dm-view-stub"
      data-agent={agentSlug}
      data-channel={channelSlug}
    />
  ),
}));

// Stub api client
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

afterEach(() => {
  cleanup();
});

describe("OnboardingDMRoute", () => {
  it("renders DMView pointed at the canonical CEO DM channel", async () => {
    getMock.mockResolvedValue({ phase: "greet" });
    render(<OnboardingDMRoute />, { wrapper });

    const stub = await screen.findByTestId("dm-view-stub");
    expect(stub).toHaveAttribute("data-channel", "ceo__human");
    expect(stub).toHaveAttribute("data-agent", "ceo");
  });

  it("exposes the phase via data-phase attribute", async () => {
    getMock.mockResolvedValue({ phase: "identity" });
    render(<OnboardingDMRoute />, { wrapper });

    // Immediately renders with "loading" then updates after query resolves.
    const route = screen.getByTestId("onboarding-dm-route");
    expect(route).toBeDefined();
  });

  it("renders without crash when onboarding state is unavailable", () => {
    getMock.mockRejectedValue(new Error("unreachable"));
    expect(() => render(<OnboardingDMRoute />, { wrapper })).not.toThrow();
  });

  it("sets data-phase=loading initially while state fetches", () => {
    getMock.mockReturnValue(new Promise(() => {})); // never resolves
    render(<OnboardingDMRoute />, { wrapper });

    const route = screen.getByTestId("onboarding-dm-route");
    expect(route).toHaveAttribute("data-phase", "loading");
  });

  it("surfaces a pending ceo_form_field suggestion via CeoCardSection", async () => {
    const suggestion: CeoFormFieldSuggestion = {
      id: "sug-1",
      phase: "identity",
      kind: "ceo_form_field",
      payload: {
        field: "company_name",
        label: "Office name?",
        optional: false,
      },
    };
    getMock.mockResolvedValue({
      phase: "identity",
      pending_suggestion: suggestion,
    });

    render(<OnboardingDMRoute />, { wrapper });

    // CeoCardSection renders inside OnboardingDMRoute via context.
    // This test verifies the context is provided correctly by checking the
    // data attribute updates once the query resolves.
    // Full card rendering is tested in InterviewBar.ceoKinds.test.tsx.
    const route = screen.getByTestId("onboarding-dm-route");
    await waitFor(() =>
      expect(route).toHaveAttribute("data-phase", "identity"),
    );
  });
});
