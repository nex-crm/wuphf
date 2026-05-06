import { createRef } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { StoredActivitySnapshot } from "../../stores/app";
import { AgentEventPeek } from "./AgentEventPeek";

// ─── helpers ────────────────────────────────────────────────────────────────

function makeSnap(
  overrides: Partial<StoredActivitySnapshot> = {},
): StoredActivitySnapshot {
  return {
    slug: "tess",
    activity: "running tests",
    detail: "running jest suite with 3 failing snapshots",
    kind: "routine",
    receivedAtMs: Date.now() - 5000,
    haloUntilMs: 0,
    ...overrides,
  };
}

function makeAnchorRef() {
  const div = document.createElement("div");
  // Simulate a bounding rect so computePosition() gets real numbers.
  div.getBoundingClientRect = () =>
    ({
      top: 100,
      bottom: 140,
      left: 260,
      right: 260,
      width: 0,
      height: 40,
      x: 260,
      y: 100,
      toJSON: () => ({}),
    }) as DOMRect;
  document.body.appendChild(div);
  const ref = createRef<HTMLElement | null>();
  // createRef is readonly; assign via cast.
  (ref as React.MutableRefObject<HTMLElement | null>).current = div;
  return ref;
}

const defaultProps = {
  slug: "tess",
  agentName: "Tess Edison",
  agentRole: "engineer",
  open: true,
  current: makeSnap(),
  history: [],
  onClose: vi.fn(),
  onOpenWorkspace: vi.fn(),
};

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  // Remove any leftover anchor divs.
  for (const el of Array.from(document.body.querySelectorAll("div[style]"))) {
    el.remove();
  }
});

// ─── render behavior ─────────────────────────────────────────────────────────

describe("<AgentEventPeek> render", () => {
  it("renders nothing when open=false", () => {
    const anchorRef = makeAnchorRef();
    const { container } = render(
      <AgentEventPeek {...defaultProps} anchorRef={anchorRef} open={false} />,
    );
    // Portal target is document.body, but the dialog itself must be absent.
    expect(document.querySelector(".sidebar-agent-peek")).toBeNull();
    expect(container.firstChild).toBeNull();
  });

  it("renders the empty-state when open=true but no snapshot has arrived yet", () => {
    const anchorRef = makeAnchorRef();
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        current={undefined}
      />,
    );
    // Dialog itself still mounts so the chevron's aria-controls target
    // resolves and the user gets a visible response to the click.
    expect(document.querySelector(".sidebar-agent-peek")).not.toBeNull();
    // Header still renders (agent name + role).
    expect(screen.getByText(defaultProps.agentName)).toBeDefined();
    // Empty-state line carries the "no activity yet" copy.
    expect(screen.getByTestId("peek-empty")).toBeDefined();
    // State row, detail, recent list all collapse — no snapshot to render.
    expect(document.querySelector(".sidebar-agent-peek-state-row")).toBeNull();
    expect(document.querySelector(".sidebar-agent-peek-detail")).toBeNull();
    expect(
      document.querySelector(".sidebar-agent-peek-recent-section"),
    ).toBeNull();
  });

  it("renders the current-thought block when detail differs from activity", () => {
    const anchorRef = makeAnchorRef();
    const current = makeSnap({
      activity: "running tests",
      detail: "running jest suite with 3 failing snapshots",
    });
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        current={current}
      />,
    );
    expect(
      screen.getByText("running jest suite with 3 failing snapshots"),
    ).toBeDefined();
  });

  it("omits the current-thought block when detail equals activity", () => {
    const anchorRef = makeAnchorRef();
    const sameText = "running tests";
    const current = makeSnap({ activity: sameText, detail: sameText });
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        current={current}
      />,
    );
    const detailEl = document.getElementById(
      `peek-current-${defaultProps.slug}`,
    );
    expect(detailEl).toBeNull();
  });

  it("omits RECENT block when history is empty and not stuck", () => {
    const anchorRef = makeAnchorRef();
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        history={[]}
        current={makeSnap({ kind: "routine" })}
      />,
    );
    expect(document.querySelector(".sidebar-agent-peek-recent")).toBeNull();
  });

  it("shows up to 6 history entries in passed order", () => {
    const anchorRef = makeAnchorRef();
    const now = Date.now();
    const history: StoredActivitySnapshot[] = Array.from(
      { length: 8 },
      (_, i) =>
        makeSnap({
          activity: `event-${i}`,
          detail: `event-${i}`,
          receivedAtMs: now - (i + 1) * 10000,
        }),
    );
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        history={history}
        current={makeSnap({ kind: "routine" })}
      />,
    );
    const items = document.querySelectorAll(".sidebar-agent-peek-recent-item");
    // Cap is 6 (not stuck, so no pinned entry).
    expect(items.length).toBe(6);
    // First visible item matches the first history entry.
    expect(items[0].textContent).toContain("event-0");
  });

  describe("stuck variant", () => {
    it("sets data-stuck='true' on the dialog", () => {
      const anchorRef = makeAnchorRef();
      render(
        <AgentEventPeek
          {...defaultProps}
          anchorRef={anchorRef}
          current={makeSnap({ kind: "stuck", activity: "vault timeout" })}
        />,
      );
      const dialog = document.querySelector(".sidebar-agent-peek");
      expect(dialog?.getAttribute("data-stuck")).toBe("true");
    });

    it("renders the BLOCKED chip", () => {
      const anchorRef = makeAnchorRef();
      render(
        <AgentEventPeek
          {...defaultProps}
          anchorRef={anchorRef}
          current={makeSnap({ kind: "stuck", activity: "vault timeout" })}
        />,
      );
      expect(
        document.querySelector(".sidebar-agent-peek-blocked-chip"),
      ).not.toBeNull();
    });

    it("pins the stuck event at the top of the recent list with a BLOCKED: prefix", () => {
      const anchorRef = makeAnchorRef();
      const stuckSnap = makeSnap({
        kind: "stuck",
        activity: "vault credentials expired",
      });
      const history: StoredActivitySnapshot[] = [
        makeSnap({
          activity: "deploy started",
          receivedAtMs: Date.now() - 60000,
        }),
      ];
      render(
        <AgentEventPeek
          {...defaultProps}
          anchorRef={anchorRef}
          current={stuckSnap}
          history={history}
        />,
      );
      const items = document.querySelectorAll(
        ".sidebar-agent-peek-recent-item",
      );
      expect(items.length).toBeGreaterThanOrEqual(1);
      expect(items[0].textContent).toContain("BLOCKED:");
      expect(items[0].textContent).toContain("vault credentials expired");
    });
  });

  it("renders correctly under prefers-reduced-motion: reduce (smoke)", () => {
    // Mock matchMedia to report reduced motion.
    const original = window.matchMedia;
    try {
      window.matchMedia = (query: string) => ({
        matches: query === "(prefers-reduced-motion: reduce)",
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      });

      const anchorRef = makeAnchorRef();
      render(<AgentEventPeek {...defaultProps} anchorRef={anchorRef} />);
      // The dialog must still be present — CSS handles the motion suppression.
      expect(document.querySelector(".sidebar-agent-peek")).not.toBeNull();
    } finally {
      window.matchMedia = original;
    }
  });
});

// ─── interaction and accessibility ────────────────────────────────────────────

describe("<AgentEventPeek> interaction and accessibility", () => {
  it("calls onClose when Escape is pressed inside the dialog", () => {
    const anchorRef = makeAnchorRef();
    const onClose = vi.fn();
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        onClose={onClose}
      />,
    );
    const dialog = document.querySelector(".sidebar-agent-peek") as HTMLElement;
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("calls onOpenWorkspace when Enter is pressed inside the dialog", () => {
    const anchorRef = makeAnchorRef();
    const onOpenWorkspace = vi.fn();
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        onOpenWorkspace={onOpenWorkspace}
      />,
    );
    const dialog = document.querySelector(".sidebar-agent-peek") as HTMLElement;
    fireEvent.keyDown(dialog, { key: "Enter" });
    expect(onOpenWorkspace).toHaveBeenCalledOnce();
  });

  it("calls onClose on outside pointerdown and does NOT close on inside pointerdown", () => {
    const anchorRef = makeAnchorRef();
    const onClose = vi.fn();
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        onClose={onClose}
      />,
    );

    // Pointerdown inside the dialog — should NOT close.
    const dialog = document.querySelector(".sidebar-agent-peek") as HTMLElement;
    fireEvent.pointerDown(dialog);
    expect(onClose).not.toHaveBeenCalled();

    // Pointerdown outside — should close.
    fireEvent.pointerDown(document.body);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("aria-labelledby points to an element containing the agent name", () => {
    const anchorRef = makeAnchorRef();
    render(<AgentEventPeek {...defaultProps} anchorRef={anchorRef} />);
    const dialog = document.querySelector(".sidebar-agent-peek") as HTMLElement;
    const labelId = dialog.getAttribute("aria-labelledby");
    expect(labelId).toBe(`peek-name-${defaultProps.slug}`);
    const labelEl = document.getElementById(labelId as string);
    expect(labelEl).not.toBeNull();
    expect(labelEl?.textContent).toBe(defaultProps.agentName);
  });

  it("aria-describedby points to the detail element when detail differs from activity", () => {
    const anchorRef = makeAnchorRef();
    const current = makeSnap({
      activity: "running tests",
      detail: "running jest suite with 3 failing snapshots",
    });
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        current={current}
      />,
    );
    const dialog = document.querySelector(".sidebar-agent-peek") as HTMLElement;
    const descId = dialog.getAttribute("aria-describedby");
    expect(descId).toBe(`peek-current-${defaultProps.slug}`);
    const descEl = document.getElementById(descId as string);
    expect(descEl).not.toBeNull();
    expect(descEl?.textContent).toContain(
      "running jest suite with 3 failing snapshots",
    );
  });

  it("aria-describedby is absent when detail equals activity", () => {
    const anchorRef = makeAnchorRef();
    const sameText = "running tests";
    const current = makeSnap({ activity: sameText, detail: sameText });
    render(
      <AgentEventPeek
        {...defaultProps}
        anchorRef={anchorRef}
        current={current}
      />,
    );
    const dialog = document.querySelector(".sidebar-agent-peek") as HTMLElement;
    expect(dialog.getAttribute("aria-describedby")).toBeNull();
  });
});
