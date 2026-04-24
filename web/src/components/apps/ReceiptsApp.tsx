import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { type AgentLog, getAgentLogs } from "../../api/client";
import { formatRelativeTime, formatTokens, formatUSD } from "../../lib/format";

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
    queryFn: () => getAgentLogs({ limit: 100 }),
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
          What each agent actually did, tool by tool. No claims {"\u2014"} just
          the log.
        </div>
      </div>

      {isLoading && (
        <div style={{ padding: 20, color: "var(--text-tertiary)" }}>
          Loading...
        </div>
      )}

      {error && (
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
      )}

      {!(isLoading || error) && (
        <LogTable logs={data?.logs ?? []} onSelectTask={onSelectTask} />
      )}
    </>
  );
}

function LogTable({
  logs,
  onSelectTask,
}: {
  logs: AgentLog[];
  onSelectTask: (taskId: string) => void;
}) {
  if (logs.length === 0) {
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
            <th style={{ padding: "8px 12px" }}>Action</th>
            <th style={{ padding: "8px 12px" }}>Time</th>
            <th style={{ padding: "8px 12px", textAlign: "right" }}>Tokens</th>
            <th style={{ padding: "8px 12px", textAlign: "right" }}>Cost</th>
          </tr>
        </thead>
        <tbody>
          {logs.map((log) => {
            const totalTokens = log.usage?.total_tokens ?? 0;
            const cost = log.usage?.cost_usd ?? 0;
            return (
              <tr
                key={log.id}
                style={{
                  borderTop: "1px solid var(--border-light)",
                  cursor: log.task ? "pointer" : "default",
                }}
                onClick={() => log.task && onSelectTask(log.task)}
              >
                <td style={{ padding: "10px 20px", fontWeight: 500 }}>
                  {log.agent || "\u2014"}
                </td>
                <td
                  style={{
                    padding: "10px 12px",
                    color: "var(--text-secondary)",
                  }}
                >
                  {log.action || log.content?.slice(0, 60) || "\u2014"}
                </td>
                <td
                  style={{
                    padding: "10px 12px",
                    color: "var(--text-secondary)",
                  }}
                >
                  {log.timestamp ? formatRelativeTime(log.timestamp) : "\u2014"}
                </td>
                <td
                  style={{
                    padding: "10px 12px",
                    textAlign: "right",
                    fontFamily: "var(--font-mono)",
                    fontSize: 12,
                  }}
                >
                  {totalTokens > 0 ? formatTokens(totalTokens) : "\u2014"}
                </td>
                <td
                  style={{
                    padding: "10px 12px",
                    textAlign: "right",
                    fontFamily: "var(--font-mono)",
                    fontSize: 12,
                  }}
                >
                  {cost > 0 ? formatUSD(cost) : "\u2014"}
                </td>
              </tr>
            );
          })}
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
    queryFn: () => getAgentLogs({ task: taskId }),
  });

  const logs = data?.logs ?? [];

  return (
    <>
      <button
        className="btn btn-secondary btn-sm"
        style={{ margin: "12px 20px 0" }}
        onClick={onBack}
      >
        {"\u2190"} Back to receipts
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

      {isLoading && (
        <div style={{ padding: "16px 20px", color: "var(--text-tertiary)" }}>
          Loading...
        </div>
      )}

      {error && (
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
      )}

      {!(isLoading || error) && logs.length === 0 && (
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

      {!(isLoading || error) && logs.length > 0 && (
        <div style={{ overflow: "auto", flex: 1, padding: "0 20px 20px" }}>
          {logs.map((entry, i) => (
            <div
              key={entry.id}
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
                    minWidth: 64,
                  }}
                >
                  #{i + 1}{" "}
                  {entry.timestamp
                    ? new Date(entry.timestamp).toLocaleTimeString()
                    : "\u2014"}
                </span>
                <span
                  style={{ fontWeight: 600, fontFamily: "var(--font-mono)" }}
                >
                  {entry.action || "(unknown)"}
                </span>
                {entry.agent && (
                  <span
                    style={{ fontSize: 11, color: "var(--text-secondary)" }}
                  >
                    @{entry.agent}
                  </span>
                )}
              </div>
              {entry.content && (
                <div
                  style={{
                    fontSize: 12,
                    color: "var(--text-secondary)",
                    paddingLeft: 76,
                  }}
                >
                  {entry.content.slice(0, 200)}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </>
  );
}
