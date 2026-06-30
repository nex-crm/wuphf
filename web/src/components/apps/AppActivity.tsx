import { useEffect, useMemo, useRef, useState } from "react";

import { sseURL } from "../../api/client";
import { useAgentStream } from "../../hooks/useAgentStream";
import { APP_BUILDER_SLUG } from "../../lib/constants";
import {
  type BuildActivityItem,
  extractBuildEvents,
  reduceBuildActivity,
} from "./buildActivity";

interface AppActivityProps {
  /** The app whose build/edit run to stream. Null/empty renders nothing. */
  appId: string | null | undefined;
}

/**
 * AppActivity is the live "what the App Builder is doing" feed scoped to a single
 * APP. It subscribes to GET /apps/{id}/activity — the broker resolves the app's
 * backing build/edit run and streams its HeadlessEvents — so the operator
 * surface watches the thinking + tool-call chain by APP ID alone, never a task
 * id. Same reducer + rows + styling as the office-side TaskActivity; the only
 * difference is the app-scoped stream URL. This is the operator app-build
 * substrate: the App is the single identifier.
 */
export function AppActivity({ appId }: AppActivityProps) {
  const id = appId?.trim() ? appId.trim() : null;
  // sseURL appends ?token= for EventSource auth; the broker resolves the app's
  // backing run server-side, so no task id is ever in the URL.
  const url = id
    ? sseURL(`/apps/${encodeURIComponent(id)}/activity`)
    : undefined;
  const { lines, connected } = useAgentStream(
    id ? APP_BUILDER_SLUG : null,
    null,
    {
      keepAlive: true,
      // Reduce the FULL ordered event log (pairing tool_use→tool_result across
      // the whole build), so keep every event — a build can run hundreds of
      // tool calls and the default cap would evict early tool_use rows.
      maxLines: 5000,
      url,
    },
  );
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

  if (!id || items.length === 0) return null;

  return (
    <section
      className="app-build-activity"
      data-open={open}
      aria-label="App build activity"
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
        <span className="app-build-activity__title">Building</span>
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
