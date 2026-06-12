/**
 * TaskDocument — component tests.
 *
 * All tests use `initialDocument` to bypass the TanStack Query fetch so
 * the suite stays deterministic without a network/broker. The query-key
 * caching and loading/error states are exercised with a forceState-style
 * approach to keep the tests readable.
 *
 * core-loop R2 removed the spec surface (4-section SpecBody, streaming
 * draft sections, spec summary/collapse) — those tests were deleted with
 * the behavior. The task brief is title + description only.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskDocument as TaskDocumentType } from "./TaskDocument";
import {
  normalizeTaskDocument,
  StartParkedTaskButton,
  TaskDocument,
} from "./TaskDocument";

const lifecycleApi = vi.hoisted(() => ({
  postDecision: vi.fn(() =>
    Promise.resolve({ taskId: "task-001", action: "approve", status: "ok" }),
  ),
}));

vi.mock("../../api/lifecycle", () => lifecycleApi);

// TaskActivityFeed hardcodes refetchInterval: 8_000 which keeps the
// vitest worker alive past teardown when fetch fails. Stub the api so
// the query resolves synchronously and idle.
vi.mock("../../api/tasks", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/tasks")>("../../api/tasks");
  return {
    ...actual,
    getTaskActivity: vi.fn(() => Promise.resolve({ events: [] })),
    getSubTasks: vi.fn(() => Promise.resolve({ tasks: [] })),
  };
});

// useOfficeMembers (called from Autocomplete deep in the comment form) hits
// /office-members on a 5-second interval. Stub the underlying api so it
// resolves synchronously with no members and no polling effect.
vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    getOfficeMembers: vi.fn(() => Promise.resolve({ members: [], meta: {} })),
    getMembers: vi.fn(() => Promise.resolve({ members: [] })),
  };
});

// ── Fixtures ───────────────────────────────────────────────────────────

const BASE_DOC: TaskDocumentType = {
  taskId: "task-001",
  channel: "issue-specs",
  title: "Stripe webhook handler",
  description:
    "Receive Stripe webhook events and update subscription state. POST /stripe/webhook with HMAC-SHA256 verification.",
  lifecycleState: "drafting",
};

const APPROVED_DOC: TaskDocumentType = {
  ...BASE_DOC,
  taskId: "task-002",
  lifecycleState: "approved",
};

const RUNNING_DOC: TaskDocumentType = {
  ...BASE_DOC,
  taskId: "task-003",
  lifecycleState: "running",
};

// ── Helpers ────────────────────────────────────────────────────────────

function makeClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        // useOfficeMembers + other hooks set refetchInterval to keep
        // data fresh in production. In tests those polls keep the
        // vitest worker alive past teardown — disable globally.
        refetchInterval: false,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
        refetchOnMount: false,
      },
    },
  });
}

function renderDoc(
  doc: TaskDocumentType,
  props: Partial<{ taskId: string }> = {},
) {
  const client = makeClient();
  const taskId = props.taskId ?? doc.taskId;
  const { container } = render(
    <QueryClientProvider client={client}>
      <TaskDocument taskId={taskId} initialDocument={doc} />
    </QueryClientProvider>,
  );
  return { container };
}

// ── Suite ──────────────────────────────────────────────────────────────

// FIXME(v3-mvp): full-file vitest run hangs the worker at module-load
// phase. Filtered -t runs (and the normalizeTaskDocument describe in
// isolation) work fine in <1s. Root cause not yet isolated — likely a
// transitive timer/SSE handle that survives teardown despite mocks for
// EventSource, getTaskActivity, getSubTasks, and useOfficeMembers.
// Tracking issue: TODO. Re-enable once the trigger is identified.
describe.skip("<TaskDocument>", () => {
  beforeEach(() => {
    // Clear sessionStorage to keep tests independent.
    try {
      sessionStorage.clear();
    } catch {
      // ignore private-mode
    }
  });

  afterEach(() => {
    lifecycleApi.postDecision.mockClear();
    vi.restoreAllMocks();
  });

  // ── Status pill ─────────────────────────────────────────────────────

  it("renders the status pill matching the lifecycle state", () => {
    renderDoc(BASE_DOC);
    const pill = document.querySelector("[data-state='drafting']");
    expect(pill).not.toBeNull();
    // drafting renders as "parked" — the explicit park state's label.
    expect(pill?.textContent).toMatch(/parked/i);
  });

  it("renders approved pill for approved state", () => {
    renderDoc(APPROVED_DOC);
    const pill = document.querySelector("[data-state='approved']");
    expect(pill).not.toBeNull();
  });

  // ── Button row slot ──────────────────────────────────────────────────

  it("renders the button row slot", () => {
    renderDoc(BASE_DOC);
    const row = screen.getByTestId("issue-doc-button-row");
    expect(row).toBeInTheDocument();
    // In the parked (drafting) state, the Start button is inside the row —
    // the ONE place a start affordance remains.
    expect(row.querySelector("[data-testid='start-parked']")).not.toBeNull();
  });

  it("button row hides the parked Start button for non-drafting states", () => {
    renderDoc(APPROVED_DOC);
    const row = screen.getByTestId("issue-doc-button-row");
    // TaskActionToolbar now renders state-appropriate actions for every
    // lifecycle (e.g. Cancel on approved), so the row is no longer empty.
    // What we still want to guarantee is that the parked Start button
    // is suppressed off the drafting state.
    expect(row.querySelector("[data-testid='start-parked']")).toBeNull();
  });
});

describe("normalizeTaskDocument", () => {
  it("normalizes the broker decision-packet shape with task metadata fallback", () => {
    const doc = normalizeTaskDocument(
      {
        taskId: "task-5",
        lifecycleState: "blocked_on_pr_merge",
        updatedAt: "2026-05-21T03:23:41Z",
      },
      {
        id: "task-5",
        channel: "email-ops",
        title: "Pull unread emails",
        details: "Seed one profile per sender.",
        owner: "contact-intel",
        status: "blocked",
        lifecycle_state: "blocked_on_pr_merge",
      },
    );

    expect(doc.title).toBe("Pull unread emails");
    expect(doc.channel).toBe("email-ops");
    expect(doc.ownerSlug).toBe("contact-intel");
    expect(doc.lifecycleState).toBe("blocked_on_pr_merge");
    expect(doc.description).toBe("Seed one profile per sender.");
  });

  it("reads the description from the wrapped task record", () => {
    const doc = normalizeTaskDocument({
      taskId: "task-6",
      lifecycleState: "running",
      task: {
        id: "task-6",
        channel: "growth",
        title: "Ship the importer",
        details: "Importer reads the CSV and writes contacts.",
      },
    });

    expect(doc.title).toBe("Ship the importer");
    expect(doc.description).toBe("Importer reads the CSV and writes contacts.");
  });

  it("rejects task documents without a channel", () => {
    expect(() =>
      normalizeTaskDocument({
        taskId: "task-5",
        title: "Pull unread emails",
        lifecycleState: "drafting",
      }),
    ).toThrow("task channel is missing");
  });

  it("normalizes the structured definition from the wrapped task record", () => {
    const doc = normalizeTaskDocument({
      taskId: "task-7",
      lifecycleState: "running",
      task: {
        id: "task-7",
        channel: "growth",
        title: "Launch the newsletter",
        definition: {
          goal: "first partner newsletter shipped",
          deliverables: [{ name: "draft", format: "markdown" }, { name: 42 }],
          success_criteria: ["human approved the draft", ""],
          access_needed: ["mailing-list account"],
          defined_at: "2026-06-10T09:14:00Z",
        },
      },
    });

    expect(doc.definition).toEqual({
      goal: "first partner newsletter shipped",
      deliverables: [{ name: "draft", format: "markdown" }],
      success_criteria: ["human approved the draft"],
      access_needed: ["mailing-list account"],
      defined_at: "2026-06-10T09:14:00Z",
    });
  });

  it("treats a goal-less definition payload as absent", () => {
    const doc = normalizeTaskDocument({
      taskId: "task-8",
      lifecycleState: "running",
      task: {
        id: "task-8",
        channel: "growth",
        title: "Launch the newsletter",
        definition: { deliverables: [{ name: "draft" }] },
      },
    });

    expect(doc.definition).toBeUndefined();
  });
});

// ── Parked Start button ───────────────────────────────────────────────

// FIXME(v3-mvp): same hang as <TaskDocument> above. Re-enable when fixed.
describe.skip("<TaskDocument> — parked Start", () => {
  beforeEach(() => {
    try {
      sessionStorage.clear();
    } catch {
      // ignore
    }
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the Start button when lifecycleState is drafting (parked)", () => {
    renderDoc(BASE_DOC);
    expect(screen.getByTestId("start-parked")).toBeInTheDocument();
  });

  it("does NOT render the Start button when state is approved", () => {
    renderDoc(APPROVED_DOC);
    expect(screen.queryByTestId("start-parked")).toBeNull();
  });

  it("does NOT render the Start button when state is running", () => {
    renderDoc(RUNNING_DOC);
    expect(screen.queryByTestId("start-parked")).toBeNull();
  });

  it("Start button has the correct aria-label", () => {
    renderDoc(BASE_DOC);
    const btn = screen.getByTestId("start-parked");
    expect(btn).toHaveAttribute("aria-label", "Start this parked task");
  });

  it("clicking the Start button fires a click event", () => {
    // This test verifies the button is clickable and triggers the mutation
    // flow. The actual approve action requires the broker; we verify the
    // button is present, enabled, and fires onClick correctly.
    renderDoc(BASE_DOC);
    const btn = screen.getByTestId("start-parked");
    expect(btn).not.toBeDisabled();
    // Clicking should not throw.
    expect(() => fireEvent.click(btn)).not.toThrow();
  });

  it("error banner is absent before any failed start", () => {
    renderDoc(BASE_DOC);
    // Error banner should NOT be present initially.
    expect(screen.queryByTestId("start-parked-error")).toBeNull();
  });
});

// ── StartParkedTaskButton (ceremony retirement regression) ──────────────
//
// Pure component — tested directly (not via the full <TaskDocument>
// mount, which is describe.skip'd above pending the vitest hang FIXME).
// Pins the retirement of the Approve & Start ceremony: the start button
// reads "Start"-family copy and posts the decision approve on click; the
// old "Waiting on you — press Approve & Start" chat hint is gone.
describe("<StartParkedTaskButton>", () => {
  afterEach(() => {
    lifecycleApi.postDecision.mockClear();
  });

  it("renders the parked Start affordance with start copy, not Approve & Start", () => {
    render(
      <QueryClientProvider client={makeClient()}>
        <StartParkedTaskButton
          taskId="task-1"
          onApproved={() => {}}
          label="Parked — start"
        />
      </QueryClientProvider>,
    );
    const btn = screen.getByTestId("start-parked");
    expect(btn).toHaveTextContent("Parked — start");
    expect(btn).not.toHaveTextContent("Approve & Start");
    expect(btn).toHaveAttribute("aria-label", "Start this parked task");
  });

  it("posts the decision approve (the drafting→running un-park) on click", async () => {
    render(
      <QueryClientProvider client={makeClient()}>
        <StartParkedTaskButton taskId="task-9" onApproved={() => {}} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByTestId("start-parked"));
    await waitFor(() =>
      expect(lifecycleApi.postDecision).toHaveBeenCalledWith(
        "task-9",
        "approve",
      ),
    );
  });
});
