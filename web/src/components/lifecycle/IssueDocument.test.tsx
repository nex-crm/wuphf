/**
 * IssueDocument — Phase 3 component tests.
 *
 * All tests use `initialDocument` to bypass the TanStack Query fetch so
 * the suite stays deterministic without a network/broker. The query-key
 * caching and loading/error states are exercised with a forceState-style
 * approach to keep the tests readable.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { IssueDocument as IssueDocumentType } from "./IssueDocument";
import { IssueDocument } from "./IssueDocument";

// ── Fixtures ───────────────────────────────────────────────────────────

const BASE_DOC: IssueDocumentType = {
  taskId: "task-001",
  title: "Stripe webhook handler",
  lifecycleState: "drafting",
  spec: {
    goal: "Receive Stripe webhook events and update subscription state.",
    context: "Subscriptions are stored in the billing database.",
    approach: "POST /stripe/webhook with HMAC-SHA256 verification.",
    acceptance:
      "- Webhook endpoint at POST /stripe/webhook\n- Signature verified",
  },
  comments: [
    {
      id: "c1",
      author: "ceo",
      isAgent: true,
      body: "Drafted spec based on our chat.",
      appendedAt: "2026-05-17T10:03:00Z",
    },
    {
      id: "c2",
      author: "engineer",
      isAgent: true,
      body: "Approach looks good. Add idempotency via the Stripe Event ID.",
      appendedAt: "2026-05-17T10:04:00Z",
    },
    {
      id: "c3",
      author: "human",
      isAgent: false,
      body: "Yes please add to acceptance criteria.",
      appendedAt: "2026-05-17T10:05:00Z",
    },
  ],
};

const APPROVED_DOC: IssueDocumentType = {
  ...BASE_DOC,
  taskId: "task-002",
  lifecycleState: "approved",
};

const RUNNING_DOC: IssueDocumentType = {
  ...BASE_DOC,
  taskId: "task-003",
  lifecycleState: "running",
};

// ── Helpers ────────────────────────────────────────────────────────────

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function renderDoc(
  doc: IssueDocumentType,
  props: Partial<{ taskId: string }> = {},
) {
  const client = makeClient();
  const taskId = props.taskId ?? doc.taskId;
  const { container } = render(
    <QueryClientProvider client={client}>
      <IssueDocument taskId={taskId} initialDocument={doc} />
    </QueryClientProvider>,
  );
  return { container };
}

// ── Suite ──────────────────────────────────────────────────────────────

describe("<IssueDocument>", () => {
  beforeEach(() => {
    // Clear sessionStorage to keep tests independent.
    try {
      sessionStorage.clear();
    } catch {
      // ignore private-mode
    }
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ── Spec sections ───────────────────────────────────────────────────

  it("renders all four spec section headings", () => {
    renderDoc(BASE_DOC);
    expect(screen.getByRole("heading", { name: /goal/i })).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: /context/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: /approach/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: /acceptance/i }),
    ).toBeInTheDocument();
  });

  it("renders spec section content from the document", () => {
    renderDoc(BASE_DOC);
    expect(
      screen.getByText(/Receive Stripe webhook events/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/billing database/i)).toBeInTheDocument();
  });

  it("renders em-dash placeholder for missing spec sections", () => {
    const doc: IssueDocumentType = {
      ...BASE_DOC,
      spec: { goal: "A goal" },
    };
    renderDoc(doc);
    // Three placeholders for context, approach, acceptance.
    const placeholders = screen.getAllByText("—");
    expect(placeholders.length).toBeGreaterThanOrEqual(3);
  });

  // ── Status pill ─────────────────────────────────────────────────────

  it("renders the status pill matching the lifecycle state", () => {
    renderDoc(BASE_DOC);
    const pill = document.querySelector("[data-state='drafting']");
    expect(pill).not.toBeNull();
    expect(pill?.textContent).toMatch(/drafting/i);
  });

  it("renders approved pill for approved state", () => {
    renderDoc(APPROVED_DOC);
    const pill = document.querySelector("[data-state='approved']");
    expect(pill).not.toBeNull();
  });

  // ── Phase 4 button row slot ──────────────────────────────────────────

  it("renders the empty Phase 4 button row slot", () => {
    renderDoc(BASE_DOC);
    const row = screen.getByTestId("issue-doc-button-row");
    expect(row).toBeInTheDocument();
    // Empty in Phase 3 — no buttons inside.
    expect(row.querySelectorAll("button").length).toBe(0);
  });

  // ── Comment timeline ────────────────────────────────────────────────

  it("renders all comments in the timeline", () => {
    renderDoc(BASE_DOC);
    const list = screen.getByTestId("issue-comments-list");
    expect(list).toBeInTheDocument();
    expect(
      screen.getByText(/Drafted spec based on our chat/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/idempotency via the Stripe Event ID/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Yes please add to acceptance criteria/i),
    ).toBeInTheDocument();
  });

  it("renders a PixelAvatar canvas for each comment author", () => {
    renderDoc(BASE_DOC);
    // Three comments, each gets a canvas (PixelAvatar renders a <canvas>).
    const canvases = document.querySelectorAll(".issue-comment canvas");
    expect(canvases.length).toBe(BASE_DOC.comments.length);
  });

  it("renders empty-state message when there are no comments", () => {
    renderDoc({ ...BASE_DOC, comments: [] });
    expect(screen.getByTestId("issue-comments-empty")).toBeInTheDocument();
  });

  it("interleaves human and agent comments in order", () => {
    renderDoc(BASE_DOC);
    const authors = screen
      .getAllByRole("article")
      .map((el) => el.querySelector(".issue-comment-author")?.textContent);
    expect(authors).toEqual(["ceo", "engineer", "human"]);
  });

  // ── Collapse on approved ─────────────────────────────────────────────

  it("auto-collapses spec sections when state is approved", () => {
    renderDoc(APPROVED_DOC);
    // Should show summary card, not the full spec sections.
    expect(screen.getByLabelText(/spec summary/i)).toBeInTheDocument();
    // Full spec headings should NOT be present.
    expect(screen.queryByRole("heading", { name: /^Goal$/i })).toBeNull();
  });

  it("auto-collapses spec sections when state is running", () => {
    renderDoc(RUNNING_DOC);
    expect(screen.getByLabelText(/spec summary/i)).toBeInTheDocument();
  });

  it("does NOT auto-collapse spec for drafting state", () => {
    renderDoc(BASE_DOC);
    expect(
      screen.getByRole("heading", { name: /^Goal$/i }),
    ).toBeInTheDocument();
  });

  // ── Expand-restore after re-mount ────────────────────────────────────

  it("restores expanded state from sessionStorage on re-mount", () => {
    // First mount: approved (collapsed by default).
    const { unmount } = render(
      <QueryClientProvider client={makeClient()}>
        <IssueDocument taskId="task-002" initialDocument={APPROVED_DOC} />
      </QueryClientProvider>,
    );
    // Expand via button.
    const expandBtn = screen.getByRole("button", { name: /expand spec/i });
    fireEvent.click(expandBtn);
    expect(
      screen.getByRole("heading", { name: /^Goal$/i }),
    ).toBeInTheDocument();

    unmount();

    // Second mount: same taskId — should restore expanded.
    render(
      <QueryClientProvider client={makeClient()}>
        <IssueDocument taskId="task-002" initialDocument={APPROVED_DOC} />
      </QueryClientProvider>,
    );
    expect(
      screen.getByRole("heading", { name: /^Goal$/i }),
    ).toBeInTheDocument();
  });

  it("collapse button collapses back to summary card", () => {
    // Start approved (collapsed), expand, then collapse.
    renderDoc(APPROVED_DOC);
    fireEvent.click(screen.getByRole("button", { name: /expand spec/i }));
    // Collapse button should now be visible.
    const collapseBtn = screen.getByRole("button", { name: /collapse spec/i });
    fireEvent.click(collapseBtn);
    expect(screen.getByLabelText(/spec summary/i)).toBeInTheDocument();
  });
});
