import { useEffect, useMemo, useRef, useState } from "react";

import { useAgentStream } from "../../hooks/useAgentStream";
import {
  type BuildActivityItem,
  extractBuildEvents,
  reduceBuildActivity,
} from "./buildActivity";

interface TaskActivityProps {
  taskId: string;
  /**
   * The agent doing the work — the task's owner. Null/empty (e.g. an unstaffed
   * task still being triaged) renders nothing; the pane appears once an owner is
   * streaming tool activity.
   */
  agentSlug: string | null | undefined;
}

/**
 * TaskActivity is the live "what the agent is doing" feed for ANY task. It
 * subscribes to the owner agent's typed HeadlessEvent stream (scoped to this
 * task) and renders each tool call as a single row that resolves running → ✓ /
 * ✗ — so the human watches concrete progress (reading files, running commands,
 * writing, building) in real time, not a wall of prose or a spinner that never
 * finishes. keepAlive keeps the feed live across the task's many turns.
 *
 * Generalized from the App Builder's build feed: an app-build task passes the
 * App Builder slug, every other task passes its own owner. The CSS classes keep
 * the original `app-build-activity` names (purely cosmetic) so the styling is
 * shared.
 */
export function TaskActivity({ taskId, agentSlug }: TaskActivityProps) {
  const slug = agentSlug?.trim() ? agentSlug.trim() : null;
  const { lines, connected } = useAgentStream(slug, taskId, {
    keepAlive: true,
    // The activity feed reduces the FULL ordered event log (pairing
    // tool_use→tool_result across the whole build), so it must keep every
    // event — a build can run hundreds of tool calls. The default 50-line cap
    // would evict early tool_use events and render phantom blank rows.
    maxLines: 5000,
  });
  const [open, setOpen] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  const items = useMemo(
    () => reduceBuildActivity(extractBuildEvents(lines)),
    [lines],
  );
  const running = items.some((i) => i.status === "running");

  const lastId = items[items.length - 1]?.id;
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on each new row
  useEffect(() => {
    const el = scrollRef.current;
    if (!(el && open)) return;
    el.scrollTop = el.scrollHeight;
  }, [lastId, items.length, open]);

  if (!slug || items.length === 0) return null;

  return (
    <section
      className="app-build-activity"
      data-open={open}
      aria-label="Task activity"
    >
      <button
        type="button"
        className="app-build-activity__header"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <span className={`app-build-activity__chevron${open ? " open" : ""}`}>
          ▸
        </span>
        <span className="app-build-activity__title">Task activity</span>
        <span
          className={`app-build-activity__dot${
            connected && running ? " live" : ""
          }`}
          aria-hidden="true"
        />
        <span className="app-build-activity__count">{items.length}</span>
      </button>
      {open ? (
        <div className="app-build-activity__list" ref={scrollRef}>
          {items.map((item) => (
            <ActivityRow key={item.id} item={item} />
          ))}
        </div>
      ) : null}
    </section>
  );
}

function ActivityRow({ item }: { item: BuildActivityItem }) {
  return (
    <div
      className={`app-build-activity__row app-build-activity__row--${item.status}`}
    >
      <span className="app-build-activity__icon" aria-hidden="true">
        {item.status === "running" ? (
          <span className="app-build-activity__spinner" />
        ) : item.status === "error" ? (
          "✗"
        ) : (
          "✓"
        )}
      </span>
      <span className="app-build-activity__verb">{item.verb}</span>
      {item.target ? (
        <span className="app-build-activity__target" title={item.target}>
          {item.target}
        </span>
      ) : null}
      {item.note && item.status !== "running" ? (
        <span className="app-build-activity__note" title={item.note}>
          {item.note}
        </span>
      ) : null}
    </div>
  );
}
