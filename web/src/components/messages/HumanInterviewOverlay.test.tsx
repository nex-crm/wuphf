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

  it("renders the EXTERNAL ACTION badge and Variant B layout for approval-kind requests", () => {
    const req = makeRequest({
      kind: "approval",
      title: "Send Email via Gmail",
      question: "@growthops wants to send email via Gmail. Approve?",
      context: [
        "Why: Sending welcome note.",
        "",
        "What this will do:",
        "• To: alex@nex.ai",
        "• Subject: Welcome to Nex",
        "• Body: Hi Alex, welcome aboard!",
        "",
        "Action: GMAIL_SEND_EMAIL via Gmail",
        "Account: live::gmail::default::abc123",
        "Channel: #general",
      ].join("\n"),
      options: [
        { id: "approve", label: "Approve" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "approve",
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(screen.getByText("EXTERNAL ACTION")).toBeInTheDocument();
    expect(screen.getByText("BLOCKING")).toBeInTheDocument();
    // The dedicated card splits the verb headline from the platform eyebrow.
    expect(screen.getByText("Send Email")).toBeInTheDocument();
    expect(screen.getByText("Gmail")).toBeInTheDocument();
    // Why surface
    expect(screen.getByText("Sending welcome note.")).toBeInTheDocument();
    // Payload fields: each label/value is a <dt>/<dd> pair
    expect(screen.getByText("To")).toBeInTheDocument();
    expect(screen.getByText("alex@nex.ai")).toBeInTheDocument();
    expect(screen.getByText("Subject")).toBeInTheDocument();
    expect(screen.getByText("Welcome to Nex")).toBeInTheDocument();
    expect(screen.getByText("Body")).toBeInTheDocument();
    expect(screen.getByText("Hi Alex, welcome aboard!")).toBeInTheDocument();
    // Raw action id + connected account
    expect(screen.getByText("GMAIL_SEND_EMAIL")).toBeInTheDocument();
    expect(
      screen.getByText("live::gmail::default::abc123"),
    ).toBeInTheDocument();
    // The trust escalation is offered.
    expect(
      screen.getByRole("button", { name: /always allow/i }),
    ).toBeInTheDocument();
  });

  it("renders the (truncated) chip when a detail value ends with the ellipsis", () => {
    const req = makeRequest({
      kind: "approval",
      title: "Send Email via Gmail",
      context: [
        "What this will do:",
        "• To: alex@nex.ai",
        "• Body: Hi Alex this is a very long body…",
        "",
        "Action: GMAIL_SEND_EMAIL via Gmail",
        "Channel: #general",
      ].join("\n"),
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));
    expect(screen.getByText("truncated")).toBeInTheDocument();
  });

  it("renders the empty fallback when neither Why nor details are present", () => {
    const req = makeRequest({
      kind: "approval",
      title: "Refresh OAuth Token via Gmail",
      context: [
        "Action: GMAIL_REFRESH_TOKEN via Gmail",
        "Channel: #general",
      ].join("\n"),
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));
    expect(screen.getByText(/No structured payload/i)).toBeInTheDocument();
  });

  it("falls back to plain pre-wrap context for legacy/non-approval requests", () => {
    const req = makeRequest({
      kind: "interview",
      title: "Pricing call?",
      context: "platform: gmail\naction_id: GMAIL_SEND_EMAIL",
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(screen.queryByText("EXTERNAL ACTION")).not.toBeInTheDocument();
    const ctx = screen.getByText((_, el) => {
      if (!el || el.tagName.toLowerCase() !== "p") return false;
      return (el.textContent ?? "").startsWith("platform: gmail");
    });
    expect(ctx.getAttribute("style") ?? "").toContain("pre-wrap");
  });

  it("styles the recommended option with btn-primary class", () => {
    const req = makeRequest({
      options: [
        { id: "proceed", label: "Proceed" },
        { id: "hold", label: "Hold" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "proceed",
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    const proceedBtn = screen.getByRole("button", { name: "Proceed" });
    expect(proceedBtn.className).toContain("btn-primary");

    const holdBtn = screen.getByRole("button", { name: "Hold" });
    expect(holdBtn.className).toContain("btn-ghost");
  });
});
