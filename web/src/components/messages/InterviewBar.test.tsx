import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";

// Mocks installed BEFORE InterviewBar import so the component picks them up.
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
  };
});

import * as clientMod from "../../api/client";
import { InterviewBar } from "./InterviewBar";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function setPending(reqs: AgentRequest[]): void {
  useRequestsMock.mockReturnValue({
    all: reqs,
    pending: reqs,
    blockingPending: reqs.find((r) => r.blocking) ?? null,
  });
}

describe("<InterviewBar> approval UX", () => {
  it("renders EXTERNAL ACTION badge and structured details for approval kind", () => {
    const approval: AgentRequest = {
      id: "request-99",
      from: "growthops",
      channel: "general",
      kind: "approval",
      status: "pending",
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
      blocking: true,
      created_at: "2026-05-06T00:00:00Z",
    };
    setPending([approval]);
    render(wrap(<InterviewBar />));

    expect(screen.getByText("EXTERNAL ACTION")).toBeInTheDocument();
    expect(screen.getByText("BLOCKING")).toBeInTheDocument();
    expect(screen.getByText("Sending welcome note.")).toBeInTheDocument();
    expect(screen.getByText("alex@nex.ai")).toBeInTheDocument();
    expect(screen.getByText("Welcome to Nex")).toBeInTheDocument();
    expect(
      screen.getByText("live::gmail::default::abc123"),
    ).toBeInTheDocument();
  });

  it("falls back to plain context for non-approval requests in the bar", () => {
    const interview: AgentRequest = {
      id: "request-100",
      from: "growthops",
      channel: "general",
      kind: "interview",
      status: "pending",
      title: "Need direction",
      question: "Which path should we take?",
      context: "background information from the agent",
      options: [{ id: "yes", label: "Yes" }],
      blocking: false,
      created_at: "2026-05-06T00:00:00Z",
    };
    setPending([interview]);
    render(wrap(<InterviewBar />));

    expect(screen.queryByText("EXTERNAL ACTION")).not.toBeInTheDocument();
    expect(
      screen.getByText("background information from the agent"),
    ).toBeInTheDocument();
  });

  it("resets text mode when the active request changes", () => {
    const needsText: AgentRequest = {
      id: "request-text",
      from: "growthops",
      channel: "general",
      kind: "interview",
      status: "pending",
      question: "What should we say?",
      options: [{ id: "custom", label: "Custom", requires_text: true }],
      blocking: false,
      created_at: "2026-05-06T00:00:00Z",
    };
    const nextRequest: AgentRequest = {
      ...needsText,
      id: "request-next",
      question: "Approve the new plan?",
      options: [{ id: "approve", label: "Approve" }],
    };

    setPending([needsText]);
    const { rerender } = render(wrap(<InterviewBar />));
    fireEvent.click(screen.getByRole("button", { name: /Custom/i }));
    expect(screen.getByRole("textbox")).toBeInTheDocument();

    setPending([nextRequest]);
    rerender(wrap(<InterviewBar />));

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.getByText("Approve the new plan?")).toBeInTheDocument();
  });

  it("collects text for legacy direct-answer interviews", async () => {
    const legacyInterview: AgentRequest = {
      id: "request-legacy-interview",
      from: "research",
      channel: "general",
      kind: "interview",
      status: "pending",
      question: "What should we ask next?",
      options: [{ id: "answer_directly", label: "Answer directly" }],
      blocking: false,
      created_at: "2026-05-06T00:00:00Z",
    };
    const answerSpy = vi.mocked(clientMod.answerRequest);
    answerSpy.mockClear();

    setPending([legacyInterview]);
    render(wrap(<InterviewBar />));

    fireEvent.click(screen.getByRole("button", { name: /Answer directly/i }));
    const textbox = screen.getByRole("textbox");
    fireEvent.change(textbox, {
      target: { value: "Ask whether the buyer has budget authority." },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Send as Answer directly/i }),
    );

    await waitFor(() => {
      expect(answerSpy).toHaveBeenCalledWith(
        "request-legacy-interview",
        "answer_directly",
        "Ask whether the buyer has budget authority.",
      );
    });
  });
});

describe("<InterviewBar> notice framing (N8)", () => {
  const notice: AgentRequest = {
    id: "request-notice-1",
    from: "ae",
    channel: "task-office-2",
    kind: "notice",
    status: "pending",
    title: "OFFICE-4 delivered",
    question:
      "Renewal sequences delivered: 3 sequences ready — artifact: agents/ae/notebook/renewal-sequences.md",
    options: [{ id: "acknowledge", label: "Acknowledge" }],
    recommended_id: "acknowledge",
    blocking: false,
    created_at: "2026-06-10T00:00:00Z",
  };

  it("labels kind=notice rows NOTICE and never says 'asks'", () => {
    setPending([notice]);
    render(wrap(<InterviewBar />));

    expect(screen.getByText("NOTICE")).toBeInTheDocument();
    expect(screen.queryByText("INTERVIEW")).not.toBeInTheDocument();
    expect(screen.queryByText(/asks/)).not.toBeInTheDocument();
    expect(screen.getByText("from @ae")).toBeInTheDocument();
    expect(screen.getByText("OFFICE-4 delivered")).toBeInTheDocument();
  });

  it("keeps INTERVIEW framing with 'asks' for real interview kinds", () => {
    const interview: AgentRequest = {
      id: "request-real-interview",
      from: "ceo",
      channel: "task-office-2",
      kind: "interview",
      status: "pending",
      question: "Who should own the Acme renewal?",
      options: [{ id: "answer_directly", label: "Answer directly" }],
      blocking: false,
      created_at: "2026-06-10T00:00:00Z",
    };
    setPending([interview]);
    render(wrap(<InterviewBar />));

    expect(screen.getByText("INTERVIEW")).toBeInTheDocument();
    expect(screen.queryByText("NOTICE")).not.toBeInTheDocument();
    expect(screen.getByText("@ceo asks")).toBeInTheDocument();
  });

  it("still lets the human acknowledge a notice", () => {
    setPending([notice]);
    render(wrap(<InterviewBar />));
    expect(
      screen.getByRole("button", { name: /Acknowledge/i }),
    ).toBeInTheDocument();
  });
});
