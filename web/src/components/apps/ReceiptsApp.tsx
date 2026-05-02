import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  getAgentLogEntries,
  listAgentLogTasks,
  type TaskLogEntry,
  type TaskLogSummary,
} from "../../api/client";
import { keyedByOccurrence } from "../../lib/reactKeys";

export function ReceiptsApp() {
  const [selectedTask, setSelectedTask] = useState<string | null>(null);

  if (selectedTask) {
    return (
      <ReceiptDetail
        taskId={selectedTask}
        onBack={() => setSelectedTask(null)}
      />
    );
  }

  return <ReceiptList onSelectTask={setSelectedTask} />;
}

function ReceiptList({
  onSelectTask,
}: {
  onSelectTask: (taskId: string) => void;
}) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["agent-logs"],
    queryFn: () => listAgentLogTasks({ limit: 100 }),
    refetchInterval: 10_000,
  });

  return (
    <>
      <div
        style={{
          padding: "16px 20px",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <h3 style={{ fontSize: 16, fontWeight: 600 }}>Receipts</h3>
        <div
          style={{ fontSize: 12, color: "var(--text-tertiary)", marginTop: 4 }}
        >
          What each agent actually did, tool by tool. No claims {"—"} just the
          log.
        </div>
      </div>

      {isLoading ? (
        <div style={{ padding: 20, color: "var(--text-tertiary)" }}>
          Loading...
        </div>
      ) : null}

      {error ? (
        <div
          style={{
            padding: "40px 20px",
            textAlign: "center",
            color: "var(--text-tertiary)",
            fontSize: 14,
          }}
        >
          Could not load receipts.
        </div>
      ) : null}

      {!(isLoading || error) && (
        <TaskTable tasks={data?.tasks ?? []} onSelectTask={onSelectTask} />
      )}
    </>
  );
}

function TaskTable({
  tasks,
  onSelectTask,
}: {
  tasks: TaskLogSummary[];
  onSelectTask: (taskId: string) => void;
}) {
  if (tasks.length === 0) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        No receipts yet. Agents write one when they use a tool.
      </div>
    );
  }

  return (
    <div style={{ overflow: "auto", flex: 1 }}>
      <table
        style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}
      >
        <thead>
          <tr
            style={{
              textAlign: "left",
              color: "var(--text-tertiary)",
              fontSize: 11,
              textTransform: "uppercase",
            }}
          >
            <th style={{ padding: "8px 20px" }}>Agent</th>
            <th style={{ padding: "8px 12px" }}>Task</th>
            <th style={{ padding: "8px 12px", textAlign: "right" }}>Tools</th>
            <th style={{ padding: "8px 12px" }}>Last activity</th>
            <th style={{ padding: "8px 12px", textAlign: "right" }}>Size</th>
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => (
            <tr
              key={t.taskId}
              style={{
                borderTop: "1px solid var(--border-light)",
                cursor: "pointer",
                background: t.hasError
                  ? "color-mix(in srgb, var(--danger, #c0392b) 6%, transparent)"
                  : undefined,
              }}
              onClick={() => onSelectTask(t.taskId)}
            >
              <td style={{ padding: "10px 20px", fontWeight: 500 }}>
                {t.agentSlug || "—"}
              </td>
              <td
                style={{
                  padding: "10px 12px",
                  color: "var(--text-secondary)",
                  fontFamily: "var(--font-mono)",
                  fontSize: 12,
                }}
              >
                {t.taskId}
                {t.hasError ? (
                  <span
                    title="Contains a tool error"
                    style={{
                      marginLeft: 6,
                      fontSize: 11,
                      color: "var(--danger, #c0392b)",
                    }}
                  >
                    {"⚠"}
                  </span>
                ) : null}
              </td>
              <td
                style={{
                  padding: "10px 12px",
                  textAlign: "right",
                  fontFamily: "var(--font-mono)",
                  fontSize: 12,
                }}
              >
                {t.toolCallCount}
              </td>
              <td
                style={{
                  padding: "10px 12px",
                  color: "var(--text-secondary)",
                }}
              >
                {formatEpochMs(t.lastToolAt)}
              </td>
              <td
                style={{
                  padding: "10px 12px",
                  textAlign: "right",
                  fontFamily: "var(--font-mono)",
                  fontSize: 12,
                  color: "var(--text-tertiary)",
                }}
              >
                {formatBytes(t.sizeBytes)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ReceiptDetail({
  taskId,
  onBack,
}: {
  taskId: string;
  onBack: () => void;
}) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["agent-logs", taskId],
    queryFn: () => getAgentLogEntries(taskId),
  });

  const entries = data?.entries ?? [];
  const keyedEntries = keyedTaskLogEntries(entries);

  return (
    <>
      <button
        type="button"
        className="btn btn-secondary btn-sm"
        style={{ margin: "12px 20px 0" }}
        onClick={onBack}
      >
        {"←"} Back to receipts
      </button>

      <div style={{ padding: "12px 20px 8px" }}>
        <h3
          style={{
            fontSize: 15,
            fontWeight: 600,
            fontFamily: "var(--font-mono)",
          }}
        >
          {taskId}
        </h3>
        <div
          style={{ fontSize: 12, color: "var(--text-tertiary)", marginTop: 4 }}
        >
          Tool-by-tool trace of this task.
        </div>
      </div>

      {isLoading ? (
        <div style={{ padding: "16px 20px", color: "var(--text-tertiary)" }}>
          Loading...
        </div>
      ) : null}

      {error ? (
        <div
          style={{
            padding: "40px 20px",
            textAlign: "center",
            color: "var(--text-tertiary)",
            fontSize: 14,
          }}
        >
          Could not load task trace.
        </div>
      ) : null}

      {!(isLoading || error) && entries.length === 0 && (
        <div
          style={{
            padding: "40px 20px",
            textAlign: "center",
            color: "var(--text-tertiary)",
            fontSize: 14,
          }}
        >
          No tool calls in this task yet.
        </div>
      )}

      {!(isLoading || error) && entries.length > 0 && (
        <div style={{ overflow: "auto", flex: 1, padding: "0 20px 20px" }}>
          {keyedEntries.map(({ entry, key }, i) => (
            <EntryRow key={key} index={i} entry={entry} />
          ))}
        </div>
      )}
    </>
  );
}

function keyedTaskLogEntries(entries: TaskLogEntry[]) {
  return keyedByOccurrence(entries, (entry) =>
    [
      entry.task_id,
      entry.started_at ?? 0,
      entry.completed_at ?? 0,
      entry.agent_slug,
      entry.tool_name,
      entry.error ? "error" : "ok",
    ].join(":"),
  ).map(({ key, value: entry }) => ({ entry, key }));
}

function EntryRow({ index, entry }: { index: number; entry: TaskLogEntry }) {
  const isError = Boolean(entry.error);
  const snippet = entry.error || entry.result || "";
  const displaySnippet =
    snippet.length > 400 ? `${snippet.slice(0, 400)}…` : snippet;
  return (
    <div
      style={{
        padding: "10px 0",
        borderBottom: "1px solid var(--border-light)",
        fontSize: 13,
        display: "flex",
        flexDirection: "column",
        gap: 4,
      }}
    >
      <div style={{ display: "flex", gap: 12, alignItems: "baseline" }}>
        <span
          style={{
            color: "var(--text-tertiary)",
            fontSize: 11,
            minWidth: 96,
            fontFamily: "var(--font-mono)",
          }}
        >
          #{index + 1} {formatEpochMsTime(entry.started_at)}
        </span>
        <span
          style={{
            fontWeight: 600,
            fontFamily: "var(--font-mono)",
            color: isError ? "var(--danger, #c0392b)" : undefined,
          }}
        >
          {entry.tool_name || "(unknown)"}
        </span>
        {entry.agent_slug ? (
          <span style={{ fontSize: 11, color: "var(--text-secondary)" }}>
            @{entry.agent_slug}
          </span>
        ) : null}
        {typeof entry.completed_at === "number" &&
          typeof entry.started_at === "number" && (
            <span
              style={{
                fontSize: 11,
                color: "var(--text-tertiary)",
                marginLeft: "auto",
                fontFamily: "var(--font-mono)",
              }}
            >
              {entry.completed_at - entry.started_at}ms
            </span>
          )}
      </div>
      {snippet ? (
        <div
          style={{
            fontSize: 12,
            color: isError ? "var(--danger, #c0392b)" : "var(--text-secondary)",
            paddingLeft: 108,
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
          }}
        >
          {displaySnippet}
        </div>
      ) : null}
    </div>
  );
}

function formatEpochMs(ms?: number): string {
  if (!ms) return "—";
  const diff = Date.now() - ms;
  if (diff < 0) return new Date(ms).toLocaleString();
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  if (day < 30) return `${day}d ago`;
  return new Date(ms).toLocaleDateString();
}

function formatEpochMsTime(ms?: number): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleTimeString();
}

function formatBytes(n: number): string {
  if (!n) return "—";
  if (n < 1024) return `${n}B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)}K`;
  return `${(n / (1024 * 1024)).toFixed(1)}M`;
}
