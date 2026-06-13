/**
 * Live Stream tab — real-time SSE output from this agent plus recent run history.
 * Mirrors StreamSection + RecentRunsSection from AgentPanel.tsx.
 */

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";

import { listAgentLogTasks, type TaskLogSummary } from "../../../api/tasks";
import { useAgentStream } from "../../../hooks/useAgentStream";
import { router } from "../../../lib/router";
import { StreamLineView } from "../../messages/StreamLineView";

interface LiveStreamTabProps {
  agentSlug: string;
}

function RecentRunsSection({ agentSlug }: { agentSlug: string }) {
  // queryKey is shared with AgentProfilePanel's runs query (Config tab), which
  // caches an ARRAY (it applies arrayOrEmpty to r.tasks). We MUST normalize to
  // the same shape here, or whichever tab loads first poisons the shared cache
  // for the other (object-vs-array → `.map is not a function`). Return the
  // tasks array so both consumers see an array.
  const { data, isLoading, isError } = useQuery({
    queryKey: ["agent-log-tasks", agentSlug],
    queryFn: () =>
      listAgentLogTasks({ limit: 8, agentSlug }).then((r) =>
        Array.isArray(r?.tasks) ? r.tasks : [],
      ),
    refetchInterval: 15_000,
  });

  const runs: TaskLogSummary[] = data ?? [];

  if (isLoading) {
    return (
      <div className="agent-stream-section">
        <div className="agent-stream-section-title">Recent runs</div>
        <div className="agent-stream-runs-empty">Loading…</div>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="agent-stream-section">
        <div className="agent-stream-section-title">Recent runs</div>
        <div className="agent-stream-runs-empty" role="alert">
          Couldn't load recent runs.
        </div>
      </div>
    );
  }

  return (
    <div className="agent-stream-section">
      <div className="agent-stream-section-title">Recent runs</div>
      {runs.length === 0 ? (
        <div className="agent-stream-runs-empty">No recent runs</div>
      ) : (
        <ul className="agent-stream-runs-list">
          {runs.map((r) => (
            <li key={r.taskId} className="agent-stream-run-item">
              <button
                type="button"
                className="agent-stream-run-btn"
                onClick={() =>
                  void router.navigate({
                    to: "/tasks/$taskId",
                    params: { taskId: r.taskId },
                  })
                }
              >
                <span className="agent-stream-run-id">{r.taskId}</span>
                <span className="agent-stream-run-meta">
                  {r.toolCallCount} tool call
                  {r.toolCallCount === 1 ? "" : "s"}
                  {r.hasError ? " ⚠" : ""}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function LiveStreamTab({ agentSlug }: LiveStreamTabProps) {
  const { lines, connected } = useAgentStream(agentSlug);
  const scrollRef = useRef<HTMLDivElement>(null);

  const lastLine = lines[lines.length - 1];

  // Auto-scroll when near bottom, same pattern as AgentPanel StreamSection.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-run on every new line so the log auto-scrolls
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 32) {
      el.scrollTop = el.scrollHeight;
    }
  }, [lines.length, lastLine?.id, lastLine?.data]);

  return (
    <div className="agent-stream-tab" data-testid="live-stream-tab">
      {/* Live stream section */}
      <div className="agent-stream-section">
        <div className="agent-stream-section-title">Live output</div>
        <div className="agent-stream-status">
          <span
            className={`status-dot ${connected ? "active pulse" : "lurking"}`}
          />
          <span>{connected ? "Connected" : "Idle"}</span>
        </div>
        <div
          className="agent-stream-log agent-stream-log--full"
          ref={scrollRef}
        >
          {lines.length === 0 ? (
            <div className="agent-stream-empty">No output yet</div>
          ) : (
            lines.map((line) => (
              <StreamLineView key={line.id} line={line} compact={true} />
            ))
          )}
        </div>
      </div>

      <RecentRunsSection agentSlug={agentSlug} />
    </div>
  );
}
