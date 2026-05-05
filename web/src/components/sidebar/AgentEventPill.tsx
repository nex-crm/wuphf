import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  computePillState,
  type PillState,
  startEventTimer,
} from "../../lib/agentEventTimer";
import { pickIdleCopy } from "../../lib/officeIdleDictionary";
import { useAppStore } from "../../stores/app";

const PILL_TEXT_MAX = 48;

/**
 * Shared 1Hz tick context. Per eng decision C2 there must be EXACTLY ONE
 * `setInterval` running for the entire agent rail — not one per pill —
 * because the rail can host 10+ rows and per-row timers would fan out into
 * a steady CPU drag. The provider mounts a single `startEventTimer` call
 * and broadcasts the tick value through context; consumers re-render on
 * tick because the context value updates.
 */
// Default value is "now at first import" — used only when a pill is rendered
// outside an `AgentEventTickProvider` (mostly tests). The provider replaces
// it with a live tick value once mounted.
const TickContext = createContext<number>(Date.now());

function useNowMs(): number {
  // Subscribe to the shared tick. The provider re-renders ~every 1s with a
  // fresh `Date.now()` so consumers get a deterministic, single-source
  // current-time read without each one calling `Date.now()` independently.
  return useContext(TickContext);
}

interface TickProviderProps {
  children: React.ReactNode;
}

/**
 * Mounts the single shared 1Hz scheduler. Cleanup is wired through
 * `startEventTimer`'s returned destructor — without it the interval keeps
 * ticking after AgentList unmounts (dev hot-reload, route changes,
 * multi-tab), which the eng review flagged as a CRITICAL test gap.
 */
export function AgentEventTickProvider({ children }: TickProviderProps) {
  const [now, setNow] = useState<number>(() => Date.now());

  useEffect(() => {
    const stop = startEventTimer((nowMs) => {
      setNow(nowMs);
    });
    return stop;
  }, []);

  return <TickContext.Provider value={now}>{children}</TickContext.Provider>;
}

interface AgentEventPillProps {
  slug: string;
  /**
   * Domain agent role string ("engineer", "designer", etc.) used as a
   * lookup key into `pickIdleCopy`. Named `agentRole` rather than `role`
   * so it does not get linted as the HTML `role` ARIA attribute on the
   * underlying span — these are unrelated concepts that share the word.
   */
  agentRole?: string;
  /** `member.task` — initial-paint seed used until the first SSE snapshot lands. */
  fallbackTask?: string;
}

function truncate(text: string): string {
  if (text.length <= PILL_TEXT_MAX) return text;
  // Reserve one character for the ellipsis so the visible width stays
  // bounded at exactly PILL_TEXT_MAX.
  return `${text.slice(0, PILL_TEXT_MAX - 1)}…`;
}

function pillTextFor(
  state: PillState,
  snapshotActivity: string | undefined,
  snapshotDetail: string | undefined,
  idleCopy: string,
  fallbackTask: string | undefined,
  hasSnapshot: boolean,
): string {
  if (state === "stuck") {
    return snapshotActivity ?? snapshotDetail ?? "stuck";
  }
  if (state === "idle") {
    if (
      !hasSnapshot &&
      typeof fallbackTask === "string" &&
      fallbackTask.trim()
    ) {
      return fallbackTask;
    }
    return idleCopy;
  }
  // halo / holding / dim — live activity wins.
  return snapshotActivity ?? snapshotDetail ?? fallbackTask ?? idleCopy;
}

/**
 * Inline event pill rendered in place of the legacy `.sidebar-agent-task`
 * line. Subscribes to the per-slug snapshot in Zustand and the shared 1Hz
 * tick from `AgentEventTickProvider`, derives pill state via
 * `computePillState`, and renders the appropriate text + state-color bar.
 *
 * Tolerates the Lane A wire contract being incomplete at runtime: `kind`
 * defaults via `computePillState` and `meta.humanHasPosted` (Lane A) is
 * read by `useFirstRunNudge`, not here.
 */
export function AgentEventPill({
  slug,
  agentRole,
  fallbackTask,
}: AgentEventPillProps) {
  const snapshot = useAppStore((s) => s.agentActivitySnapshots[slug]);
  const nowMs = useNowMs();
  const lastAnnouncedStuckRef = useRef<string | null>(null);

  const pillState: PillState = useMemo(() => {
    if (!snapshot) return "idle";
    return computePillState({
      lastEventMs: snapshot.receivedAtMs,
      nowMs,
      kind: snapshot.kind,
      haloUntilMs: snapshot.haloUntilMs,
    });
  }, [snapshot, nowMs]);

  const idleMs = snapshot ? Math.max(0, nowMs - snapshot.receivedAtMs) : 0;
  const idleCopy = pickIdleCopy({ slug, role: agentRole, idleMs });

  const text = truncate(
    pillTextFor(
      pillState,
      snapshot?.activity,
      snapshot?.detail,
      idleCopy,
      fallbackTask,
      Boolean(snapshot),
    ),
  );

  // Stuck transition assertive announcement — fire ONCE per stuck text so
  // the screen reader doesn't shout the same blocker every tick. The ref
  // resets to null when state leaves "stuck", so re-stuck triggers a fresh
  // announcement on a new event. Computed in render (no mutation), then
  // synced to the ref in an effect to satisfy React's render purity.
  const stuckAnnouncement =
    pillState === "stuck" && lastAnnouncedStuckRef.current !== text
      ? text
      : null;
  useEffect(() => {
    if (pillState === "stuck") {
      if (stuckAnnouncement !== null) {
        lastAnnouncedStuckRef.current = text;
      }
      return;
    }
    lastAnnouncedStuckRef.current = null;
  }, [pillState, stuckAnnouncement, text]);

  return (
    <>
      <span className="sidebar-agent-pill" data-state={pillState} title={text}>
        {text}
      </span>
      {stuckAnnouncement !== null ? (
        <span className="sr-only" aria-live="assertive" aria-atomic="true">
          {`${slug} blocked: ${stuckAnnouncement}`}
        </span>
      ) : null}
    </>
  );
}
