import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  approveSkill,
  type CompileResponse,
  type CompileResult,
  compileSkills,
  getOfficeTasks,
  getSkills,
  invokeSkill,
  rejectSkill,
  type Skill,
  type SkillStatus,
  type Task,
  undoRejectSkill,
} from "../../api/client";
import { useAppStore } from "../../stores/app";
import { showNotice, showUndoToast } from "../ui/Toast";

type CompileState = "idle" | "compiling" | "done";

const STATUS_DOT_COLOR: Record<SkillStatus, string> = {
  proposed: "var(--yellow)",
  active: "var(--green)",
  archived: "var(--neutral-400, #85898b)",
};

const STATUS_LABEL: Record<SkillStatus, string> = {
  proposed: "Pending review",
  active: "Active",
  archived: "Archived",
};

function StatusDot({ color }: { color: string }) {
  return (
    <span
      style={{
        display: "inline-block",
        width: 6,
        height: 6,
        borderRadius: "50%",
        background: color,
        marginRight: 6,
        flexShrink: 0,
      }}
      aria-hidden="true"
    />
  );
}

function deriveStatus(skill: Skill): SkillStatus {
  return skill.status ?? "active";
}

function isCompileResult(r: CompileResponse): r is CompileResult {
  return typeof (r as CompileResult).scanned === "number";
}

function CompileButton({
  state,
  onClick,
  className = "btn btn-primary btn-sm",
}: {
  state: CompileState;
  onClick: () => void;
  className?: string;
}) {
  const label =
    state === "compiling"
      ? "Compiling..."
      : state === "done"
        ? "✓ Compiled"
        : "Compile";
  return (
    <button
      type="button"
      className={className}
      disabled={state !== "idle"}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

export function SkillsApp() {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["skills"],
    queryFn: () => getSkills(),
    refetchInterval: 30_000,
  });
  const [compileState, setCompileState] = useState<CompileState>("idle");

  const handleCompile = useCallback(() => {
    setCompileState("compiling");
    compileSkills()
      .then((res) => {
        if (isCompileResult(res)) {
          showNotice(
            `${res.proposed} new proposals · ${res.deduped} skipped · ${res.rejected_by_guard} rejected`,
            "success",
          );
        } else if ("queued" in res) {
          showNotice("Compile queued — already running", "info");
        } else if ("skipped" in res) {
          showNotice(`Compile skipped: ${res.skipped}`, "info");
        }
        setCompileState("done");
        queryClient.invalidateQueries({ queryKey: ["skills"] });
        setTimeout(() => setCompileState("idle"), 2000);
      })
      .catch((e: Error) => {
        setCompileState("idle");
        showNotice(`Compile failed: ${e.message}`, "error");
      });
  }, [queryClient]);

  if (isLoading) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Loading skills...
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          padding: "40px 20px",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Could not load skills.
      </div>
    );
  }

  const skills = data?.skills ?? [];
  const proposed = skills.filter((s) => deriveStatus(s) === "proposed");
  const active = skills.filter((s) => deriveStatus(s) === "active");
  const archived = skills.filter((s) => deriveStatus(s) === "archived");

  proposed.sort((a, b) =>
    String(b.created_at ?? "").localeCompare(String(a.created_at ?? "")),
  );
  active.sort((a, b) =>
    (a.name || "").localeCompare(b.name || "", undefined, {
      sensitivity: "base",
    }),
  );
  archived.sort((a, b) =>
    String(b.updated_at ?? "").localeCompare(String(a.updated_at ?? "")),
  );

  return (
    <>
      <div
        style={{
          padding: "0 0 12px",
          borderBottom: "1px solid var(--border)",
          marginBottom: 16,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 12,
          flexWrap: "wrap",
        }}
      >
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600 }}>Skills</h3>
          {skills.length > 0 && (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                fontSize: 13,
                color: "var(--text-secondary)",
              }}
            >
              <span style={{ display: "inline-flex", alignItems: "center" }}>
                <StatusDot color={STATUS_DOT_COLOR.active} />
                {active.length} active
              </span>
              <span style={{ display: "inline-flex", alignItems: "center" }}>
                <StatusDot color={STATUS_DOT_COLOR.proposed} />
                {proposed.length} pending
              </span>
              <span style={{ display: "inline-flex", alignItems: "center" }}>
                <StatusDot color={STATUS_DOT_COLOR.archived} />
                {archived.length} archived
              </span>
            </div>
          )}
        </div>
        {skills.length > 0 && (
          <CompileButton state={compileState} onClick={handleCompile} />
        )}
      </div>

      {skills.length === 0 ? (
        <div
          style={{
            padding: "40px 20px",
            textAlign: "center",
            color: "var(--text-tertiary)",
            fontSize: 13,
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: 12,
          }}
        >
          <div style={{ maxWidth: 360, lineHeight: 1.5 }}>
            No skills yet. Click <strong>Compile</strong> to ask the LLM to find
            reusable workflows in your wiki.
          </div>
          <CompileButton state={compileState} onClick={handleCompile} />
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
          {proposed.length > 0 && (
            <SkillSection
              title={STATUS_LABEL.proposed}
              count={proposed.length}
              status="proposed"
            >
              {proposed.map((skill) => (
                <SkillCard key={skill.name} skill={skill} />
              ))}
            </SkillSection>
          )}

          <SkillSection
            title={STATUS_LABEL.active}
            count={active.length}
            status="active"
          >
            {active.length === 0 ? (
              <div
                style={{
                  fontSize: 13,
                  color: "var(--text-tertiary)",
                  padding: "8px 0",
                }}
              >
                No active skills.
              </div>
            ) : (
              active.map((skill) => (
                <SkillCard key={skill.name} skill={skill} />
              ))
            )}
          </SkillSection>

          <ArchivedSection skills={archived} />
        </div>
      )}
    </>
  );
}

function SkillSection({
  title,
  count,
  status,
  children,
}: {
  title: string;
  count: number;
  status: SkillStatus;
  children: React.ReactNode;
}) {
  return (
    <section>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          fontSize: 13,
          fontWeight: 500,
          color: "var(--text-secondary)",
          marginBottom: 8,
        }}
      >
        <StatusDot color={STATUS_DOT_COLOR[status]} />
        {title} ({count})
      </div>
      {children}
    </section>
  );
}

function ArchivedSection({ skills }: { skills: Skill[] }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <section>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        style={{
          display: "flex",
          alignItems: "center",
          fontSize: 13,
          fontWeight: 500,
          color: "var(--text-secondary)",
          marginBottom: 8,
          background: "transparent",
          border: "none",
          padding: 0,
          cursor: "pointer",
          fontFamily: "inherit",
        }}
        aria-expanded={expanded}
      >
        <StatusDot color={STATUS_DOT_COLOR.archived} />
        <span
          aria-hidden="true"
          style={{
            display: "inline-block",
            marginRight: 6,
            transition: "transform 0.15s",
            transform: expanded ? "rotate(90deg)" : "rotate(0deg)",
            fontSize: 10,
          }}
        >
          {"▶"}
        </span>
        Archived ({skills.length}){expanded ? null : " — collapsed"}
      </button>
      {expanded ? (
        skills.length === 0 ? (
          <div
            style={{
              fontSize: 13,
              color: "var(--text-tertiary)",
              padding: "8px 0",
            }}
          >
            No archived skills.
          </div>
        ) : (
          skills.map((skill) => <SkillCard key={skill.name} skill={skill} />)
        )
      ) : null}
    </section>
  );
}

const STATUS_BADGE_CLASS: Record<SkillStatus, string> = {
  active: "badge badge-green",
  proposed: "badge badge-yellow",
  archived: "badge badge-muted",
};

function SkillProvenance({ articles }: { articles: string[] }) {
  if (articles.length === 0) return null;
  return (
    <div
      style={{
        fontSize: 12,
        color: "var(--text-tertiary)",
        fontFamily: "var(--font-sans)",
        marginBottom: 8,
      }}
    >
      from{" "}
      <a
        href={`/wiki/${articles[0]}`}
        target="_blank"
        rel="noreferrer"
        style={{ color: "var(--text-tertiary)" }}
      >
        {articles[0]}
      </a>
      {articles.length > 1 ? (
        <span
          style={{
            marginLeft: 6,
            padding: "1px 6px",
            background: "var(--bg-warm, var(--neutral-100))",
            borderRadius: 3,
            fontSize: 11,
          }}
        >
          +{articles.length - 1} more
        </span>
      ) : null}
    </div>
  );
}

type InvokePhase = "idle" | "invoking" | "running" | "done" | "failed";

function isTerminalTaskStatus(s: string | undefined): boolean {
  if (!s) return false;
  return ["done", "completed", "blocked", "cancelled", "canceled"].includes(s);
}

function SkillActions({
  status,
  skillName,
}: {
  status: SkillStatus;
  skillName: string;
}) {
  const [invokePhase, setInvokePhase] = useState<InvokePhase>("idle");
  const [activeTaskId, setActiveTaskId] = useState<string | null>(null);
  const [actionPending, setActionPending] = useState(false);
  const queryClient = useQueryClient();
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);

  // Poll office tasks while a skill_run task is active so we can flip the
  // badge from "running" → "done" / "failed" without relying on SSE alone.
  const isPolling = invokePhase === "running";
  const { data: officeTasks } = useQuery({
    queryKey: ["office-tasks", "skill-run-watch"],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    refetchInterval: isPolling ? 2_500 : false,
    enabled: isPolling,
  });

  const activeTask: Task | undefined = useMemo(() => {
    if (!activeTaskId) return undefined;
    return officeTasks?.tasks.find((t) => t.id === activeTaskId);
  }, [officeTasks, activeTaskId]);

  // When the polled task reaches a terminal state, flip the phase.
  useEffect(() => {
    if (!isPolling || !activeTask) return;
    if (isTerminalTaskStatus(activeTask.status)) {
      const failed =
        activeTask.status === "blocked" ||
        activeTask.status === "cancelled" ||
        activeTask.status === "canceled";
      setInvokePhase(failed ? "failed" : "done");
    }
  }, [isPolling, activeTask]);

  const handleInvoke = useCallback(() => {
    if (!skillName) return;
    setInvokePhase("invoking");
    setActiveTaskId(null);
    invokeSkill(skillName, {})
      .then((res) => {
        const tid = res?.task_id ?? null;
        setActiveTaskId(tid);
        // If the broker didn't return a task_id (older shapes / fast-path),
        // fall back to the previous "✓ Invoked → idle" flash.
        if (!tid) {
          setInvokePhase("done");
          setTimeout(() => setInvokePhase("idle"), 1500);
          return;
        }
        setInvokePhase("running");
      })
      .catch((e: Error) => {
        setInvokePhase("idle");
        showNotice(`Invoke failed: ${e.message}`, "error");
      });
  }, [skillName]);

  const handleViewTask = useCallback(() => {
    if (!activeTaskId) return;
    setCurrentApp("tasks");
  }, [activeTaskId, setCurrentApp]);

  const handleResetInvoke = useCallback(() => {
    setInvokePhase("idle");
    setActiveTaskId(null);
  }, []);

  const handleApprove = useCallback(() => {
    if (!skillName) return;
    setActionPending(true);
    approveSkill(skillName)
      .then(() => {
        showNotice("Approved", "success");
        queryClient.invalidateQueries({ queryKey: ["skills"] });
      })
      .catch((e: Error) => {
        showNotice(`approve failed: ${e.message}`, "error");
      })
      .finally(() => setActionPending(false));
  }, [skillName, queryClient]);

  const handleReject = useCallback(() => {
    if (!skillName) return;
    setActionPending(true);
    rejectSkill(skillName)
      .then((res) => {
        // Optimistic: invalidate so the card disappears, then offer undo.
        queryClient.invalidateQueries({ queryKey: ["skills"] });
        const token = res.undo_token;
        const undoMs = Math.max(1, (res.expires_in ?? 5) * 1000);
        showUndoToast(
          `Rejected ${skillName}`,
          () => {
            undoRejectSkill(token)
              .then(() => {
                showNotice("Restored", "success");
                queryClient.invalidateQueries({ queryKey: ["skills"] });
              })
              .catch((e: Error) => {
                const msg = e.message || "";
                if (/expired|gone|410/i.test(msg)) {
                  showNotice("Undo window expired", "error");
                } else {
                  showNotice(`undo failed: ${msg}`, "error");
                }
              });
          },
          undoMs,
        );
      })
      .catch((e: Error) => {
        showNotice(`reject failed: ${e.message}`, "error");
      })
      .finally(() => setActionPending(false));
  }, [skillName, queryClient]);

  if (status === "archived") {
    return (
      <span style={{ fontSize: 12, color: "var(--text-tertiary)" }}>
        Archived
      </span>
    );
  }
  if (status === "proposed") {
    return (
      <>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          disabled={actionPending}
          onClick={handleApprove}
        >
          Approve
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          disabled={actionPending}
          onClick={handleReject}
        >
          Reject
        </button>
      </>
    );
  }
  // active — show invoke button + live task chip
  return (
    <div
      style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}
    >
      <button
        type="button"
        className="btn btn-primary btn-sm"
        disabled={invokePhase === "invoking" || invokePhase === "running"}
        onClick={handleInvoke}
      >
        {invokePhase === "invoking"
          ? "Invoking..."
          : invokePhase === "running"
            ? "Running..."
            : invokePhase === "done"
              ? "✓ Invoked"
              : invokePhase === "failed"
                ? "Try again"
                : "⚡ Invoke"}
      </button>

      {activeTaskId ? (
        <SkillRunChip
          phase={invokePhase}
          taskId={activeTaskId}
          taskStatus={activeTask?.status}
          taskTitle={activeTask?.title}
          onView={handleViewTask}
          onDismiss={handleResetInvoke}
        />
      ) : null}
    </div>
  );
}

function SkillRunChip({
  phase,
  taskId,
  taskStatus,
  taskTitle,
  onView,
  onDismiss,
}: {
  phase: InvokePhase;
  taskId: string;
  taskStatus?: string;
  taskTitle?: string;
  onView: () => void;
  onDismiss: () => void;
}) {
  const isRunning = phase === "running";
  const isDone = phase === "done";
  const isFailed = phase === "failed";
  const dotColor = isFailed
    ? "var(--red, #c43e3e)"
    : isDone
      ? "var(--green)"
      : "var(--yellow)";
  const label = isRunning
    ? `Running ${taskId}`
    : isFailed
      ? `${taskStatus ?? "failed"} · ${taskId}`
      : isDone
        ? `${taskStatus ?? "done"} · ${taskId}`
        : taskId;
  return (
    <span
      title={taskTitle || taskId}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        padding: "2px 8px",
        fontSize: 12,
        background: "var(--bg-warm, var(--neutral-100))",
        border: "1px solid var(--border-subtle, var(--neutral-200))",
        borderRadius: 999,
        color: "var(--text-secondary)",
      }}
    >
      <span
        aria-hidden="true"
        style={{
          width: 6,
          height: 6,
          borderRadius: "50%",
          background: dotColor,
          animation: isRunning ? "pulse-dot 1.2s ease-in-out infinite" : "none",
        }}
      />
      <span style={{ fontFamily: "var(--font-mono)" }}>{label}</span>
      <button
        type="button"
        onClick={onView}
        style={{
          border: "none",
          background: "transparent",
          padding: 0,
          color: "var(--accent, #1264a3)",
          fontSize: 12,
          cursor: "pointer",
        }}
      >
        View →
      </button>
      {(isDone || isFailed) ? (
        <button
          type="button"
          onClick={onDismiss}
          aria-label="Dismiss"
          style={{
            border: "none",
            background: "transparent",
            padding: 0,
            color: "var(--text-tertiary)",
            fontSize: 14,
            cursor: "pointer",
            lineHeight: 1,
          }}
        >
          ×
        </button>
      ) : null}
    </span>
  );
}

function SkillCard({ skill }: { skill: Skill }) {
  const status = deriveStatus(skill);
  const sourceArticles = skill.metadata?.wuphf?.source_articles ?? [];
  const isArchived = status === "archived";

  return (
    <div
      className="app-card"
      style={{ marginBottom: 8, opacity: isArchived ? 0.6 : 1 }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          marginBottom: 4,
          flexWrap: "wrap",
        }}
      >
        <span style={{ fontSize: 16 }}>{"⚡"}</span>
        <span className="app-card-title" style={{ marginBottom: 0 }}>
          {skill.name || "Untitled"}
        </span>
        <span className={STATUS_BADGE_CLASS[status]}>{status}</span>
        {status === "proposed" ? (
          <span className="badge badge-yellow" style={{ marginLeft: 6 }}>
            AI-suggested
          </span>
        ) : null}
      </div>

      {skill.description ? (
        <div
          style={{
            fontSize: 13,
            color: "var(--text-secondary)",
            marginBottom: 6,
            lineHeight: 1.45,
          }}
        >
          {skill.description}
        </div>
      ) : null}

      {status === "proposed" ? (
        <SkillProvenance articles={sourceArticles} />
      ) : null}

      {skill.source && status !== "proposed" ? (
        <div className="app-card-meta" style={{ marginBottom: 8 }}>
          Source: {skill.source}
        </div>
      ) : null}

      <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
        <SkillActions status={status} skillName={skill.name} />
      </div>
    </div>
  );
}
