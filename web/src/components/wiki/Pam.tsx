import {
  type KeyboardEvent as ReactKeyboardEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import {
  listPamActions,
  type PamActionDescriptor,
  type PamActionEvent,
  subscribePamEvents,
} from "../../api/pam";
import { buildPamMenu, type PamMenuEntry } from "../../lib/pamActions";
import { drawKnownPixelAvatar } from "../../lib/pixelAvatar";
import { PixelAvatar } from "../ui/PixelAvatar";
import "../../styles/pam.css";

interface PamProps {
  /**
   * The article Pam should act on. `null` means we're outside an article
   * view (catalog, audit) — Pam still renders as part of the wiki chrome
   * but article-scoped actions are disabled.
   */
  articlePath: string | null;
  /**
   * Called once Pam finishes an action (SSE `done`). The Wiki shell uses
   * this to bump an article refresh nonce so the enriched article +
   * history reload without a full navigation.
   */
  onActionDone?: () => void;
}

type Status =
  | { kind: "idle" }
  | { kind: "running"; label: string }
  | { kind: "done"; label: string }
  | { kind: "failed"; message: string };

type JimPamPhase = "away" | "walking-in" | "chatting" | "walking-out";

interface JimPamLine {
  who: "jim" | "pam";
  text: string;
}

interface JimPamVisitState {
  phase: JimPamPhase;
  line: JimPamLine | null;
}

const STATUS_CLEAR_MS = 4000;
const JIM_VISIT_INITIAL_DELAY_MS = 5000;
const JIM_VISIT_INITIAL_JITTER_MS = 4000;
const JIM_VISIT_NEXT_DELAY_MS = 22000;
const JIM_VISIT_NEXT_JITTER_MS = 18000;
const JIM_WALK_IN_MS = 2600;
const JIM_WALK_OUT_MS = 2200;
const JIM_CHAT_LINE_MS = 3000;

// Tone rule (ten-out-of-ten E1): no sarcastic or dismissive filler on a
// work surface — "Of course it didn't. Classic." read as broken copy to a
// real operator (ICP-eval v3 [17:41:35]). Keep the chatter warm, not snide.
const JIM_PAM_CONVERSATIONS: readonly (readonly JimPamLine[])[] = [
  [
    {
      who: "jim",
      text: "Did you hear? CEO merged 12 PRs. Didn't ask anyone.",
    },
    { who: "pam", text: "Twelve? I'd better update the wiki." },
    { who: "jim", text: "Honestly kind of amazing." },
  ],
  [
    { who: "jim", text: "Dwight filed a formal complaint. Against the wiki." },
    { who: "pam", text: "...Against the wiki?" },
    { who: "jim", text: "He says the footnotes are insubordinate." },
  ],
  [
    {
      who: "jim",
      text: "Michael is giving the CEO agent a performance review.",
    },
    { who: "pam", text: "The AI agent." },
    { who: "jim", text: "It scored Outstanding. Michael cried." },
  ],
];

/**
 * Pam — the wiki archivist, perched on the divider line at the top of the
 * wiki shell so she's visible across catalog, article, and audit views.
 * Click Pam to open her desk menu (served from GET /pam/actions so the
 * registry stays server-defined). Selecting an action POSTs to /pam/action;
 * the dispatcher spawns Pam's sub-process and fans results back via /events
 * so we update the status line without polling. Article-scoped actions
 * disable themselves when no article is open.
 */
// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
export default function Pam({ articlePath, onActionDone }: PamProps) {
  const [menu, setMenu] = useState<PamMenuEntry[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [activeJobId, setActiveJobId] = useState<number | null>(null);
  const jimVisit = useJimPamVisit();

  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuElRef = useRef<HTMLDivElement>(null);
  const statusTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Refs mirror the state the SSE handler reads. The handler subscribes
  // once on mount (empty deps) — we keep the subscription stable and read
  // the latest activeJobId / menu through refs, rather than resubscribing on
  // every state change (which caused the handler to miss the `started`
  // event landing between trigger and effect re-run).
  const activeJobIdRef = useRef<number | null>(null);
  const menuRef = useRef<PamMenuEntry[] | null>(menu);
  const onActionDoneRef = useRef<(() => void) | undefined>(onActionDone);

  const scheduleClear = useCallback(() => {
    if (statusTimerRef.current) clearTimeout(statusTimerRef.current);
    statusTimerRef.current = setTimeout(() => {
      setStatus({ kind: "idle" });
    }, STATUS_CLEAR_MS);
  }, []);

  useEffect(() => {
    activeJobIdRef.current = activeJobId;
  }, [activeJobId]);
  useEffect(() => {
    menuRef.current = menu;
  }, [menu]);
  useEffect(() => {
    onActionDoneRef.current = onActionDone;
  }, [onActionDone]);

  // Fetch the action registry once on mount. A fetch failure surfaces a
  // distinct error state in the menu so it's not silently indistinguishable
  // from "no actions available".
  useEffect(() => {
    let cancelled = false;
    listPamActions()
      .then((res) => {
        if (cancelled) return;
        const descriptors: PamActionDescriptor[] = res.actions ?? [];
        setMenu(buildPamMenu(descriptors));
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        console.error("pam: failed to load action registry", err);
        setLoadError(
          err instanceof Error ? err.message : "Could not load Pam’s menu.",
        );
        setMenu([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Subscribe to Pam's SSE progress events exactly once. The handler reads
  // the latest activeJobId / menu / onActionDone via refs so the
  // subscription does not churn on every state change and does not miss a
  // `started` event fired between POST and the next effect pass.
  useEffect(() => {
    const unsub = subscribePamEvents((evt: PamActionEvent) => {
      const { current } = activeJobIdRef;
      if (current === null || evt.job_id !== current) return;
      if (evt.kind === "started") {
        setStatus({
          kind: "running",
          label: labelFor(evt.action, menuRef.current),
        });
        return;
      }
      if (evt.kind === "done") {
        setStatus({
          kind: "done",
          label: labelFor(evt.action, menuRef.current),
        });
        setActiveJobId(null);
        scheduleClear();
        onActionDoneRef.current?.();
        return;
      }
      if (evt.kind === "failed") {
        setStatus({
          kind: "failed",
          message: evt.error || "Pam could not finish.",
        });
        setActiveJobId(null);
        scheduleClear();
      }
    });
    return () => {
      unsub();
    };
    // scheduleClear is stable (useCallback with empty deps) — safe to omit.
  }, [scheduleClear]);

  // Close menu on outside click so it doesn't linger when the user moves
  // on. Keep it simple: single global listener, cleaned up on unmount.
  useEffect(() => {
    if (!menuOpen) return;
    function onDoc(e: MouseEvent) {
      if (!wrapRef.current) return;
      if (!wrapRef.current.contains(e.target as Node)) setMenuOpen(false);
    }
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [menuOpen]);

  useEffect(() => {
    return () => {
      if (statusTimerRef.current) clearTimeout(statusTimerRef.current);
    };
  }, []);

  // When the menu opens, focus the first menuitem so keyboard users can
  // immediately arrow through the list. Runs after paint so the button
  // exists in the DOM.
  useEffect(() => {
    if (!menuOpen) return;
    const firstItem =
      menuElRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]');
    firstItem?.focus();
  }, [menuOpen]);

  const closeMenuAndRefocus = useCallback(() => {
    setMenuOpen(false);
    triggerRef.current?.focus();
  }, []);

  const runAction = useCallback(
    async (entry: PamMenuEntry) => {
      if (!articlePath) return;
      setMenuOpen(false);
      setStatus({ kind: "running", label: entry.label });
      try {
        const { job_id } = await entry.run({ articlePath });
        setActiveJobId(job_id);
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Pam could not start.";
        setStatus({ kind: "failed", message: msg });
        setActiveJobId(null);
        scheduleClear();
      }
    },
    [articlePath, scheduleClear],
  );

  const onMenuKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      if (e.key === "Escape") {
        e.preventDefault();
        closeMenuAndRefocus();
        return;
      }
      if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
      const items = Array.from(
        menuElRef.current?.querySelectorAll<HTMLButtonElement>(
          '[role="menuitem"]',
        ) ?? [],
      ).filter((el) => !el.disabled);
      if (items.length === 0) return;
      e.preventDefault();
      const { activeElement } = document;
      const activeIndex =
        activeElement instanceof HTMLButtonElement
          ? items.indexOf(activeElement)
          : -1;
      const nextIndex =
        e.key === "ArrowDown"
          ? (activeIndex + 1 + items.length) % items.length
          : (activeIndex - 1 + items.length) % items.length;
      items[nextIndex]?.focus();
    },
    [closeMenuAndRefocus],
  );

  const busy = status.kind === "running";

  return (
    <div ref={wrapRef} className="pam-wrap" data-testid="pam-wrap">
      <JimPamVisit visit={jimVisit} />
      <button
        type="button"
        ref={triggerRef}
        className="pam-button"
        data-busy={busy ? "true" : "false"}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        aria-label="Pam the Archivist"
        title="Pam — click for options"
        onClick={() => setMenuOpen((v) => !v)}
      >
        <PixelAvatar slug="pam" size={56} className="pam-avatar" />
      </button>
      <div className="pam-desk" aria-hidden="true" />

      {menuOpen ? (
        <div
          ref={menuElRef}
          className="pam-menu"
          role="menu"
          aria-label="Pam's actions"
          onKeyDown={onMenuKeyDown}
        >
          <div className="pam-menu-header">Pam can help with</div>
          {menu === null ? (
            <div className="pam-menu-empty">Loading…</div>
          ) : loadError ? (
            <div className="pam-menu-empty" role="alert">
              Could not load Pam’s menu.
            </div>
          ) : menu.length === 0 ? (
            <div className="pam-menu-empty">No actions available.</div>
          ) : !articlePath ? (
            <div className="pam-menu-empty">Open an article to use Pam.</div>
          ) : (
            menu.map((entry) => (
              <button
                key={entry.id}
                type="button"
                role="menuitem"
                className="pam-menu-item"
                disabled={busy}
                onClick={() => {
                  void runAction(entry);
                }}
              >
                {entry.label}
              </button>
            ))
          )}
        </div>
      ) : null}

      {status.kind !== "idle" && (
        <div
          className={`pam-status${menuOpen ? " is-behind-menu" : ""}`}
          role="status"
          aria-live="polite"
          aria-hidden={menuOpen}
        >
          {renderStatus(status)}
        </div>
      )}
    </div>
  );
}

function useJimPamVisit(): JimPamVisitState {
  const [visit, setVisit] = useState<JimPamVisitState>({
    phase: "away",
    line: null,
  });

  useEffect(() => {
    if (prefersReducedAmbientMotion()) return;

    let cancelled = false;
    const timers: number[] = [];

    function schedule(callback: () => void, delayMs: number) {
      const timer = window.setTimeout(() => {
        if (!cancelled) callback();
      }, delayMs);
      timers.push(timer);
    }

    function scheduleNextVisit(delayMs: number) {
      schedule(startVisit, delayMs);
    }

    function startVisit() {
      const conversation = pickJimPamConversation();
      setVisit({ phase: "walking-in", line: null });

      schedule(() => {
        setVisit({ phase: "chatting", line: conversation[0] ?? null });

        conversation.slice(1).forEach((line, index) => {
          schedule(
            () => {
              setVisit({ phase: "chatting", line });
            },
            (index + 1) * JIM_CHAT_LINE_MS,
          );
        });

        schedule(() => {
          setVisit({ phase: "walking-out", line: null });
          schedule(() => {
            setVisit({ phase: "away", line: null });
            scheduleNextVisit(
              JIM_VISIT_NEXT_DELAY_MS +
                Math.random() * JIM_VISIT_NEXT_JITTER_MS,
            );
          }, JIM_WALK_OUT_MS);
        }, conversation.length * JIM_CHAT_LINE_MS);
      }, JIM_WALK_IN_MS);
    }

    scheduleNextVisit(
      JIM_VISIT_INITIAL_DELAY_MS + Math.random() * JIM_VISIT_INITIAL_JITTER_MS,
    );

    return () => {
      cancelled = true;
      for (const timer of timers) window.clearTimeout(timer);
    };
  }, []);

  return visit;
}

function prefersReducedAmbientMotion(): boolean {
  if (typeof window.matchMedia !== "function") return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function pickJimPamConversation(): readonly JimPamLine[] {
  const index = Math.floor(Math.random() * JIM_PAM_CONVERSATIONS.length);
  return JIM_PAM_CONVERSATIONS[index] ?? JIM_PAM_CONVERSATIONS[0];
}

function JimPamVisit({ visit }: { visit: JimPamVisitState }) {
  const bubble = visit.phase === "chatting" ? visit.line : null;

  return (
    <div
      className={`jim-pam-visitor is-${visit.phase}`}
      data-testid="jim-pam-visitor"
      aria-hidden="true"
    >
      {bubble ? (
        <div className="jim-pam-bubble" data-speaker={bubble.who}>
          {bubble.text}
        </div>
      ) : null}
      <JimFullBodySprite size={46} className="jim-pixel" />
    </div>
  );
}

function JimFullBodySprite({
  size,
  className,
}: {
  size: number;
  className?: string;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    drawKnownPixelAvatar(canvas, "hybridJim", size);
  }, [size]);

  return (
    <canvas
      ref={canvasRef}
      className={["pixel-avatar", className].filter(Boolean).join(" ")}
      data-testid="jim-full-body-sprite"
    />
  );
}

function assertNever(x: never): never {
  throw new Error(`pam: unexpected status kind ${JSON.stringify(x)}`);
}

function renderStatus(status: Status): string {
  switch (status.kind) {
    case "idle":
      return "";
    case "running":
      return `Pam is working: ${status.label}…`;
    case "done":
      return `Done: ${status.label}`;
    case "failed":
      return `Pam: ${status.message}`;
    default:
      return assertNever(status);
  }
}

function labelFor(id: string, menu: PamMenuEntry[] | null): string {
  if (!menu) return id;
  const hit = menu.find((m) => m.id === id);
  return hit?.label ?? id;
}
