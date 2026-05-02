import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";

const useRequestsMock = vi.fn();
vi.mock("../../hooks/useRequests", () => ({
  useRequests: () => useRequestsMock(),
}));

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    answerRequest: vi.fn().mockResolvedValue({}),
    cancelRequest: vi.fn().mockResolvedValue({}),
  };
});

import { HumanInterviewOverlay } from "./HumanInterviewOverlay";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function makeRequest(overrides: Partial<AgentRequest> = {}): AgentRequest {
  return {
    id: "req-1",
    from: "planner",
    question: "What should we do?",
    blocking: true,
    status: "pending",
    options: [
      { id: "yes", label: "Yes" },
      { id: "no", label: "No" },
    ],
    ...overrides,
  };
}

describe("<HumanInterviewOverlay>", () => {
  it("renders nothing when there is no blocking request", () => {
    useRequestsMock.mockReturnValue({ blockingPending: null, pending: [] });
    const { container } = render(wrap(<HumanInterviewOverlay />));
    expect(container.firstChild).toBeNull();
  });

  it("renders the blocking interview card for a normal request", () => {
    const req = makeRequest({ title: "Approve the plan?" });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));
    expect(screen.getByText("Approve the plan?")).toBeInTheDocument();
    expect(screen.getByText("Yes")).toBeInTheDocument();
    expect(screen.getByText("No")).toBeInTheDocument();
  });

  it("shows the enhance-existing banner for enhance_skill_proposal kind", () => {
    const req = makeRequest({
      kind: "enhance_skill_proposal",
      title: "Enhance existing skill?",
      options: [
        { id: "enhance", label: "Enhance" },
        { id: "approve_anyway", label: "Approve anyway" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "enhance",
      metadata: { enhances_slug: "email-ops" },
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(screen.getByText(/Similar to existing skill:/i)).toBeInTheDocument();
    expect(screen.getByText("email-ops")).toBeInTheDocument();
    expect(screen.getByText("Enhance")).toBeInTheDocument();
    expect(screen.getByText("Approve anyway")).toBeInTheDocument();
    expect(screen.getByText("Reject")).toBeInTheDocument();
  });

  it("does not show the enhance banner for a normal skill_proposal", () => {
    const req = makeRequest({
      kind: "skill_proposal",
      options: [
        { id: "accept", label: "Accept" },
        { id: "reject", label: "Reject" },
      ],
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(
      screen.queryByText(/Similar to existing skill:/i),
    ).not.toBeInTheDocument();
  });

  it("styles the recommended option with btn-primary class", () => {
    const req = makeRequest({
      kind: "enhance_skill_proposal",
      options: [
        { id: "enhance", label: "Enhance" },
        { id: "approve_anyway", label: "Approve anyway" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "enhance",
      metadata: { enhances_slug: "existing-skill" },
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    const enhanceBtn = screen.getByRole("button", { name: "Enhance" });
    expect(enhanceBtn.className).toContain("btn-primary");

    const approveBtn = screen.getByRole("button", { name: "Approve anyway" });
    expect(approveBtn.className).toContain("btn-ghost");
  });
});
