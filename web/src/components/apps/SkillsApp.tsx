import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  approveSkill,
  type CompileResponse,
  type CompileResult,
  compileSkills,
  getSkills,
  invokeSkill,
  rejectSkill,
  type Skill,
  type SkillStatus,
  undoRejectSkill,
} from "../../api/client";
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

function SkillActions({
  status,
  skillName,
}: {
  status: SkillStatus;
  skillName: string;
}) {
  const [invokeState, setInvokeState] = useState<"idle" | "invoking" | "done">(
    "idle",
  );
  const [actionPending, setActionPending] = useState(false);
  const queryClient = useQueryClient();

  const handleInvoke = useCallback(() => {
    if (!skillName) return;
    setInvokeState("invoking");
    invokeSkill(skillName, {})
      .then(() => {
        setInvokeState("done");
        setTimeout(() => setInvokeState("idle"), 1500);
      })
      .catch((e: Error) => {
        setInvokeState("idle");
        showNotice(`Invoke failed: ${e.message}`, "error");
      });
  }, [skillName]);

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
  // active
  const invokeLabel =
    invokeState === "invoking"
      ? "Invoking..."
      : invokeState === "done"
        ? "✓ Invoked"
        : "⚡ Invoke";
  return (
    <button
      type="button"
      className="btn btn-primary btn-sm"
      disabled={invokeState !== "idle"}
      onClick={handleInvoke}
    >
      {invokeLabel}
    </button>
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
