import { useEffect, useMemo, useRef, useState } from "react";

import { useAgentStream } from "../../hooks/useAgentStream";
import { APP_BUILDER_SLUG } from "../../lib/constants";
import {
  type BuildActivityItem,
  extractBuildEvents,
  reduceBuildActivity,
} from "./buildActivity";

interface AppBuildActivityProps {
  taskId: string;
}

/**
 * AppBuildActivity is the live "what the builder is doing" feed shown above the
 * preview on an App Builder task. It subscribes to the App Builder's typed
 * HeadlessEvent stream (scoped to this task) and renders each tool call as a
 * single row that resolves running → ✓ / ✗ — so the human watches concrete
 * progress (writing files, running the build, publishing) in real time, not a
 * wall of prose or a spinner that never finishes. keepAlive keeps the feed live
 * across the build's many turns.
 */
export function AppBuildActivity({ taskId }: AppBuildActivityProps) {
  const { lines, connected } = useAgentStream(APP_BUILDER_SLUG, taskId, {
    keepAlive: true,
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

  if (items.length === 0) return null;

  return (
    <section
      className="app-build-activity"
      data-open={open}
      aria-label="Build activity"
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
        <span className="app-build-activity__title">Build activity</span>
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
