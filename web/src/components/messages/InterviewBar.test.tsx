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
    // ConnectIntegrationCard (rendered for kind="connect") reads config for the
    // Composio sign-in gate on mount; keep it off the network in tests.
    getConfig: vi.fn().mockResolvedValue({ composio_key_set: true }),
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
    render(wrap(<InterviewBar channelSlug="general" />));

    expect(screen.getByText("EXTERNAL ACTION")).toBeInTheDocument();
    expect(screen.getByText("BLOCKING")).toBeInTheDocument();
    expect(screen.getByText("Sending welcome note.")).toBeInTheDocument();
    expect(screen.getByText("alex@nex.ai")).toBeInTheDocument();
    expect(screen.getByText("Welcome to Nex")).toBeInTheDocument();
    expect(
      screen.getByText("live::gmail::default::abc123"),
    ).toBeInTheDocument();
  });

  it("renders the OAuth connect card for kind=connect, not generic options", () => {
    const connect: AgentRequest = {
      id: "request-connect",
      from: "growthops",
      channel: "general",
      kind: "connect",
      status: "pending",
      title: "Connect Gmail",
      question: "Connect Gmail so the team can run this action.",
      platform: "gmail",
      options: [
        { id: "connect", label: "Connect" },
        { id: "skip", label: "Skip" },
      ],
      blocking: true,
    };
    setPending([connect]);
    render(wrap(<InterviewBar channelSlug="general" />));
    // The card-only "Connect to continue" eyebrow proves ConnectIntegrationCard
    // rendered instead of the generic interview option buttons (the bug: those
    // buttons just answered the request and never opened OAuth).
    expect(screen.getByText("Connect to continue")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Connect Gmail" }),
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
    render(wrap(<InterviewBar channelSlug="general" />));

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
    const { rerender } = render(wrap(<InterviewBar channelSlug="general" />));
    fireEvent.click(screen.getByRole("button", { name: /Custom/i }));
    expect(screen.getByRole("textbox")).toBeInTheDocument();

    setPending([nextRequest]);
    rerender(wrap(<InterviewBar channelSlug="general" />));

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
    render(wrap(<InterviewBar channelSlug="general" />));

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
    render(wrap(<InterviewBar channelSlug="task-office-2" />));

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
    render(wrap(<InterviewBar channelSlug="task-office-2" />));

    expect(screen.getByText("INTERVIEW")).toBeInTheDocument();
    expect(screen.queryByText("NOTICE")).not.toBeInTheDocument();
    expect(screen.getByText("@ceo asks")).toBeInTheDocument();
  });

  it("still lets the human acknowledge a notice", () => {
    setPending([notice]);
    render(wrap(<InterviewBar channelSlug="task-office-2" />));
    expect(
      screen.getByRole("button", { name: /Acknowledge/i }),
    ).toBeInTheDocument();
  });
});

describe("<InterviewBar> pinned ordering (v3 buried-interview fix)", () => {
  const makeRequest = (over: Partial<AgentRequest>): AgentRequest => ({
    id: "request-0",
    from: "ae",
    channel: "general",
    kind: "notice",
    status: "pending",
    question: "placeholder",
    options: [{ id: "acknowledge", label: "Acknowledge" }],
    blocking: false,
    created_at: "2026-06-11T00:00:00Z",
    ...over,
  });

  it("pins a blocking ask above older notices and interviews", () => {
    // The live v3 run buried a blocking interview behind a pile of older
    // delivery notices for 44 minutes — the bar must show the blocking
    // ask FIRST regardless of created_at.
    const oldNotice = makeRequest({
      id: "request-1",
      kind: "notice",
      question: "OFFICE-4 delivered.",
      created_at: "2026-06-11T00:00:00Z",
    });
    const oldInterview = makeRequest({
      id: "request-2",
      kind: "interview",
      question: "Which tone do you prefer?",
      options: [{ id: "answer_directly", label: "Answer directly" }],
      created_at: "2026-06-11T00:01:00Z",
    });
    const newBlocking = makeRequest({
      id: "request-3",
      kind: "approval",
      question: "Send the three renewal emails now?",
      options: [
        { id: "approve", label: "Approve" },
        { id: "reject", label: "Reject" },
      ],
      blocking: true,
      created_at: "2026-06-11T00:30:00Z",
    });
    setPending([oldNotice, oldInterview, newBlocking]);
    render(wrap(<InterviewBar channelSlug="general" />));

    expect(screen.getByText("BLOCKING")).toBeInTheDocument();
    expect(
      screen.getByText("Send the three renewal emails now?"),
    ).toBeInTheDocument();
    // 1/3 — the blocking ask is the FIRST card in the queue.
    expect(screen.getByText("1/3")).toBeInTheDocument();
  });

  it("ranks interviews above notices when nothing blocks", () => {
    const oldNotice = makeRequest({
      id: "request-1",
      kind: "notice",
      question: "OFFICE-4 delivered.",
      created_at: "2026-06-11T00:00:00Z",
    });
    const newerInterview = makeRequest({
      id: "request-2",
      kind: "interview",
      question: "Two things needed before I queue the sends.",
      options: [{ id: "answer_directly", label: "Answer directly" }],
      created_at: "2026-06-11T00:10:00Z",
    });
    setPending([oldNotice, newerInterview]);
    render(wrap(<InterviewBar channelSlug="general" />));

    expect(
      screen.getByText("Two things needed before I queue the sends."),
    ).toBeInTheDocument();
    expect(screen.getByText("1/2")).toBeInTheDocument();
  });
});

describe("<InterviewBar> channel scoping", () => {
  const makeInterview = (over: Partial<AgentRequest>): AgentRequest => ({
    id: "scoped-0",
    from: "ae",
    channel: "general",
    kind: "interview",
    status: "pending",
    question: "placeholder",
    options: [{ id: "answer_directly", label: "Answer directly" }],
    blocking: false,
    created_at: "2026-06-12T00:00:00Z",
    ...over,
  });

  it("shows only requests that originated in the current channel", () => {
    // The broker queue is office-wide; an ask raised in a task channel must
    // not block the composer on #general. Before scoping, both questions
    // rendered (1/2) on every surface — the bug this guards.
    const here = makeInterview({
      id: "here",
      channel: "task-office-2",
      question: "Who owns the Acme renewal?",
    });
    const elsewhere = makeInterview({
      id: "elsewhere",
      channel: "general",
      question: "Which tone for the general note?",
    });
    setPending([here, elsewhere]);
    render(wrap(<InterviewBar channelSlug="task-office-2" />));

    expect(screen.getByText("Who owns the Acme renewal?")).toBeInTheDocument();
    expect(
      screen.queryByText("Which tone for the general note?"),
    ).not.toBeInTheDocument();
    // 1/1 — the other channel's request is not in this bar's queue.
    expect(screen.getByText("1/1")).toBeInTheDocument();
  });

  it("treats a channel-less request as #general (broker default)", () => {
    const legacy = makeInterview({
      id: "legacy",
      channel: undefined,
      question: "Legacy request without a channel.",
    });
    setPending([legacy]);
    render(wrap(<InterviewBar channelSlug="general" />));

    expect(
      screen.getByText("Legacy request without a channel."),
    ).toBeInTheDocument();
  });

  it("renders nothing on a non-chat surface (null channel)", () => {
    setPending([makeInterview({ id: "any", channel: "general" })]);
    render(wrap(<InterviewBar channelSlug={null} />));

    expect(
      screen.queryByRole("region", { name: "Pending agent request" }),
    ).not.toBeInTheDocument();
  });
});
