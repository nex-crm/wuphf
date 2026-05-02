import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  approveSkill,
  archiveSkill,
  type CompileResponse,
  type CompileResult,
  compileSkills,
  createTasks,
  disableSkill,
  enableSkill,
  getOfficeTasks,
  getSkillsList,
  invokeSkill,
  patchSkill,
  rejectSkill,
  restoreArchivedSkill,
  type Skill,
  type SkillStatus,
  type Task,
  undoRejectSkill,
} from "../../api/client";
import { useTeamLeadSlug } from "../../hooks/useConfig";
import { useOfficeMembers } from "../../hooks/useMembers";
import { useAppStore } from "../../stores/app";
import { confirm as confirmDialog } from "../ui/ConfirmDialog";
import { LightningIcon } from "../ui/LightningIcon";
import { SidePanel } from "../ui/SidePanel";
import { showNotice, showUndoToast } from "../ui/Toast";

type CompileState = "idle" | "compiling" | "done";

const STATUS_DOT_STYLE: Record<
  SkillStatus,
  { background: string; border?: string }
> = {
  proposed: { background: "var(--yellow)" },
  active: { background: "var(--green)" },
  disabled: {
    background: "transparent",
    border: "1.5px solid var(--neutral-400, #85898b)",
  },
  archived: { background: "var(--neutral-400, #85898b)" },
};

const STATUS_LABEL: Record<SkillStatus, string> = {
  proposed: "Pending review",
  active: "Active",
  disabled: "Disabled",
  archived: "Archived",
};

const OWNER_FILTER_KEY = "skillsAppOwnerFilter";
type OwnerFilterValue = "all" | "lead-routable" | string;

function readOwnerFilter(): OwnerFilterValue {
  try {
    const stored = window.localStorage.getItem(OWNER_FILTER_KEY);
    if (stored) return stored as OwnerFilterValue;
  } catch {
    // localStorage may be unavailable (SSR, privacy mode); fall back.
  }
  return "all";
}

function writeOwnerFilter(value: OwnerFilterValue): void {
  try {
    window.localStorage.setItem(OWNER_FILTER_KEY, value);
  } catch {
    // ignore
  }
}

function StatusDot({ status }: { status: SkillStatus }) {
  const style = STATUS_DOT_STYLE[status];
  return (
    <span
      style={{
        display: "inline-block",
        width: 6,
        height: 6,
        borderRadius: "50%",
        background: style.background,
        border: style.border,
        boxSizing: "border-box",
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

interface OwnersChipProps {
  slugs?: string[];
}

/**
 * Renders owner slugs. Empty/missing slugs render as "lead-routable"
 * so ownership stays legible when the backing field is empty.
 */
export function OwnersChip({ slugs }: OwnersChipProps) {
  const list = (slugs ?? []).filter((s) => s.trim().length > 0);
  if (list.length === 0) {
    return (
      <span
        className="owners-chip owners-chip--lead"
        title="Lead-routable: any agent can route through the team lead"
      >
        lead-routable
      </span>
    );
  }
  return (
    <span
      className="owners-chip"
      title={`Scoped to: ${list.map((s) => `@${s}`).join(", ")}`}
    >
      {list.map((s) => `@${s}`).join(", ")}
    </span>
  );
}

export function SkillsApp() {
  const queryClient = useQueryClient();
  const [previewSkill, setPreviewSkill] = useState<Skill | null>(null);
  const [previewDirty, setPreviewDirty] = useState(false);
  // Pause background refetch while the editor is open so it cannot replace
  // `originalContent` mid-edit and break patchSkill old_string matching.
  const previewOpen = previewSkill !== null;
  const { data, isLoading, error } = useQuery({
    queryKey: ["skills", "all"],
    queryFn: () => getSkillsList("all"),
    refetchInterval: previewOpen ? false : 30_000,
  });
  const { data: officeMembers } = useOfficeMembers();
  const leadSlug = useTeamLeadSlug(officeMembers);
  const [compileState, setCompileState] = useState<CompileState>("idle");
  const [ownerFilter, setOwnerFilter] = useState<OwnerFilterValue>("all");

  // Hydrate filter after officeMembers loads; if the saved slug no longer
  // exists, fall back to "all" instead of rendering an empty owner result.
  useEffect(() => {
    if (officeMembers === undefined) return;
    const stored = readOwnerFilter();
    const valid =
      stored === "all" ||
      stored === "lead-routable" ||
      officeMembers.some((m) => m.slug === stored);
    setOwnerFilter(valid ? stored : "all");
    if (!valid) writeOwnerFilter("all");
  }, [officeMembers]);

  const handleOwnerFilterChange = useCallback((value: OwnerFilterValue) => {
    setOwnerFilter(value);
    writeOwnerFilter(value);
  }, []);

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

  const handlePreview = useCallback((skill: Skill) => {
    setPreviewSkill(skill);
    setPreviewDirty(false);
  }, []);

  const handlePreviewClose = useCallback(() => {
    const closePreview = () => {
      setPreviewSkill(null);
      setPreviewDirty(false);
    };
    if (previewDirty) {
      confirmDialog({
        title: "Discard edits?",
        message: "You have unsaved edits. Discard them and close?",
        confirmLabel: "Discard",
        danger: true,
        onConfirm: closePreview,
      });
      return;
    }
    closePreview();
  }, [previewDirty]);

  const handlePreviewSaved = useCallback(
    (updated: Skill) => {
      setPreviewSkill(updated);
      setPreviewDirty(false);
      queryClient.invalidateQueries({ queryKey: ["skills"] });
    },
    [queryClient],
  );

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

  const allSkills = data?.skills ?? [];
  const filteredSkills = allSkills.filter((sk) =>
    matchesOwnerFilter(sk, ownerFilter),
  );

  const proposed = filteredSkills.filter((s) => deriveStatus(s) === "proposed");
  const active = filteredSkills.filter((s) => deriveStatus(s) === "active");
  const disabled = filteredSkills.filter((s) => deriveStatus(s) === "disabled");
  const archived = filteredSkills.filter((s) => deriveStatus(s) === "archived");

  proposed.sort((a, b) =>
    String(b.created_at ?? "").localeCompare(String(a.created_at ?? "")),
  );
  active.sort((a, b) =>
    (a.name || "").localeCompare(b.name || "", undefined, {
      sensitivity: "base",
    }),
  );
  disabled.sort((a, b) =>
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
          {allSkills.length > 0 && (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                fontSize: 13,
                color: "var(--text-secondary)",
                flexWrap: "wrap",
              }}
            >
              <span style={{ display: "inline-flex", alignItems: "center" }}>
                <StatusDot status="active" />
                {active.length} active
              </span>
              <span style={{ display: "inline-flex", alignItems: "center" }}>
                <StatusDot status="proposed" />
                {proposed.length} pending
              </span>
              <span style={{ display: "inline-flex", alignItems: "center" }}>
                <StatusDot status="disabled" />
                {disabled.length} disabled
              </span>
              <span style={{ display: "inline-flex", alignItems: "center" }}>
                <StatusDot status="archived" />
                {archived.length} archived
              </span>
            </div>
          )}
        </div>
        {allSkills.length > 0 && (
          <CompileButton state={compileState} onClick={handleCompile} />
        )}
      </div>

      {allSkills.length === 0 ? (
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
        <>
          <OwnerFilterBar
            value={ownerFilter}
            onChange={handleOwnerFilterChange}
            members={officeMembers ?? []}
          />
          <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
            <SkillSection
              title={STATUS_LABEL.proposed}
              count={proposed.length}
              status="proposed"
              emptyHidden={true}
            >
              {proposed.map((skill) => (
                <SkillCard
                  key={skill.name}
                  skill={skill}
                  onPreview={handlePreview}
                  leadSlug={leadSlug}
                />
              ))}
            </SkillSection>

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
                  <SkillCard
                    key={skill.name}
                    skill={skill}
                    onPreview={handlePreview}
                    leadSlug={leadSlug}
                  />
                ))
              )}
            </SkillSection>

            <DisabledSection
              skills={disabled}
              onPreview={handlePreview}
              leadSlug={leadSlug}
            />

            <ArchivedSection
              skills={archived}
              onPreview={handlePreview}
              leadSlug={leadSlug}
            />
          </div>
        </>
      )}

      <SidePanel
        open={previewSkill !== null}
        onClose={handlePreviewClose}
        title={previewSkill?.title || previewSkill?.name || "Skill"}
        subtitle={previewSkill?.name}
      >
        {previewSkill ? (
          <SkillPreviewBody
            skill={previewSkill}
            onDirtyChange={setPreviewDirty}
            onSaved={handlePreviewSaved}
          />
        ) : null}
      </SidePanel>
    </>
  );
}

function matchesOwnerFilter(skill: Skill, filter: OwnerFilterValue): boolean {
  const owners = (skill.owner_agents ?? []).filter((s) => s.trim().length > 0);
  if (filter === "all") return true;
  if (filter === "lead-routable") return owners.length === 0;
  return owners.includes(filter);
}

function OwnerFilterBar({
  value,
  onChange,
  members,
}: {
  value: OwnerFilterValue;
  onChange: (v: OwnerFilterValue) => void;
  members: { slug: string; name?: string }[];
}) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        marginBottom: 14,
        position: "sticky",
        top: 0,
        zIndex: 5,
        background: "var(--bg-card, #fff)",
        paddingBottom: 8,
      }}
    >
      <label
        htmlFor="skills-owner-filter"
        style={{
          fontSize: 12,
          color: "var(--text-secondary)",
          fontWeight: 500,
        }}
      >
        Owner
      </label>
      <select
        id="skills-owner-filter"
        value={value}
        onChange={(e) => onChange(e.target.value as OwnerFilterValue)}
        aria-label="Filter skills by owner"
        style={{
          background: "var(--bg-card, #fff)",
          border: "1px solid var(--border)",
          color: "var(--text)",
          fontFamily: "inherit",
          fontSize: 13,
          padding: "6px 10px",
          borderRadius: 6,
          minHeight: 32,
        }}
      >
        <option value="all">All</option>
        <option value="lead-routable">Lead-routable only</option>
        {members.map((m) => (
          <option key={m.slug} value={m.slug}>
            @{m.slug}
            {m.name ? ` (${m.name})` : ""}
          </option>
        ))}
      </select>
    </div>
  );
}

function SkillSection({
  title,
  count,
  status,
  children,
  emptyHidden = false,
}: {
  title: string;
  count: number;
  status: SkillStatus;
  children: React.ReactNode;
  emptyHidden?: boolean;
}) {
  if (emptyHidden && count === 0) return null;
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
        <StatusDot status={status} />
        {title} ({count})
      </div>
      {children}
    </section>
  );
}

function DisabledSection({
  skills,
  onPreview,
  leadSlug,
}: {
  skills: Skill[];
  onPreview: (s: Skill) => void;
  leadSlug: string;
}) {
  // Per design review: disabled is recoverable, so default expanded.
  const [expanded, setExpanded] = useState(true);

  return (
    <section>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
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
      >
        <StatusDot status="disabled" />
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
        Disabled ({skills.length})
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
            Nothing disabled. You can pause a skill anytime by clicking Disable.
          </div>
        ) : (
          skills.map((skill) => (
            <SkillCard
              key={skill.name}
              skill={skill}
              onPreview={onPreview}
              leadSlug={leadSlug}
            />
          ))
        )
      ) : null}
    </section>
  );
}

function ArchivedSection({
  skills,
  onPreview,
  leadSlug,
}: {
  skills: Skill[];
  onPreview: (s: Skill) => void;
  leadSlug: string;
}) {
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
        <StatusDot status="archived" />
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
          skills.map((skill) => (
            <SkillCard
              key={skill.name}
              skill={skill}
              onPreview={onPreview}
              leadSlug={leadSlug}
            />
          ))
        )
      ) : null}
    </section>
  );
}

const STATUS_BADGE_CLASS: Record<SkillStatus, string> = {
  active: "badge badge-green",
  proposed: "badge badge-yellow",
  disabled: "badge badge-neutral",
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

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Existing function length is baselined for a focused follow-up refactor.
function SkillActions({
  status,
  skillName,
  onSuggestChanges,
}: {
  status: SkillStatus;
  skillName: string;
  onSuggestChanges?: () => void;
}) {
  const [invokePhase, setInvokePhase] = useState<InvokePhase>("idle");
  const [activeTaskId, setActiveTaskId] = useState<string | null>(null);
  const [actionPending, setActionPending] = useState(false);
  const queryClient = useQueryClient();
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);

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

  useEffect(() => {
    if (!(isPolling && activeTask)) return;
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
        queryClient.invalidateQueries({ queryKey: ["skills"] });
        const token = res.undo_token;
        const undoMs = Math.max(1, (res.expires_in ?? 6) * 1000);
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

  const handleDisable = useCallback(() => {
    if (!skillName) return;
    // Do not hide the live run chip while the polling task is still settling.
    if (invokePhase !== "idle") return;
    setActionPending(true);
    disableSkill(skillName)
      .then(() => {
        queryClient.invalidateQueries({ queryKey: ["skills"] });
        showUndoToast(
          `${skillName} disabled`,
          () => {
            enableSkill(skillName)
              .then(() => {
                showNotice("Re-enabled", "success");
                queryClient.invalidateQueries({ queryKey: ["skills"] });
              })
              .catch((e: Error) => {
                showNotice(`undo failed: ${e.message}`, "error");
              });
          },
          5_000,
        );
      })
      .catch((e: Error) => {
        showNotice(`Couldn't disable: ${e.message}`, "error");
      })
      .finally(() => setActionPending(false));
  }, [skillName, queryClient, invokePhase]);

  const handleEnable = useCallback(() => {
    if (!skillName) return;
    setActionPending(true);
    enableSkill(skillName)
      .then(() => {
        showNotice(`${skillName} enabled`, "success");
        queryClient.invalidateQueries({ queryKey: ["skills"] });
      })
      .catch((e: Error) => {
        showNotice(`Couldn't enable: ${e.message}`, "error");
      })
      .finally(() => setActionPending(false));
  }, [skillName, queryClient]);

  const handleArchive = useCallback(() => {
    if (!skillName) return;
    if (invokePhase !== "idle") return;
    setActionPending(true);
    archiveSkill(skillName)
      .then(() => {
        queryClient.invalidateQueries({ queryKey: ["skills"] });
        showUndoToast(
          `${skillName} archived`,
          () => {
            restoreArchivedSkill(skillName)
              .then(() => {
                showNotice("Restored — skill is now pending review", "success");
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
          5_000,
        );
      })
      .catch((e: Error) => {
        showNotice(`Couldn't archive: ${e.message}`, "error");
      })
      .finally(() => setActionPending(false));
  }, [skillName, queryClient, invokePhase]);

  if (status === "archived") {
    return (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
        <span style={{ fontSize: 12, color: "var(--text-tertiary)" }}>
          Archived
        </span>
        <button
          type="button"
          className="btn btn-sm"
          disabled={actionPending}
          aria-label={`Restore ${skillName} to pending review`}
          onClick={() => {
            setActionPending(true);
            restoreArchivedSkill(skillName)
              .then(() => {
                showNotice("Restored — skill is now pending review", "success");
                queryClient.invalidateQueries({ queryKey: ["skills"] });
              })
              .catch((e: Error) => {
                showNotice(`Couldn't restore: ${e.message}`, "error");
              })
              .finally(() => setActionPending(false));
          }}
        >
          Restore
        </button>
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
          aria-label={`Approve ${skillName}`}
        >
          Approve
        </button>
        {onSuggestChanges ? (
          <button
            type="button"
            className="btn-text"
            disabled={actionPending}
            onClick={onSuggestChanges}
            aria-label={`Suggest changes to ${skillName}`}
          >
            Suggest changes
          </button>
        ) : null}
        <button
          type="button"
          className="btn-text btn-text--danger"
          disabled={actionPending}
          onClick={handleReject}
          aria-label={`Reject ${skillName}`}
        >
          Reject
        </button>
      </>
    );
  }
  if (status === "disabled") {
    return (
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
        }}
      >
        <button
          type="button"
          className="btn btn-primary btn-sm"
          disabled={actionPending}
          onClick={handleEnable}
          aria-label={`Enable ${skillName}`}
        >
          Enable
        </button>
        <button
          type="button"
          className="btn-text btn-text--danger"
          disabled={actionPending}
          onClick={handleArchive}
          aria-label={`Archive ${skillName}`}
        >
          Archive
        </button>
      </div>
    );
  }
  // active
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        flexWrap: "wrap",
      }}
    >
      <button
        type="button"
        className="btn btn-primary btn-sm"
        disabled={
          invokePhase === "invoking" ||
          invokePhase === "running" ||
          actionPending
        }
        onClick={handleInvoke}
        aria-label={`Invoke ${skillName}`}
        style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
      >
        {invokePhase === "invoking" ? (
          "Invoking..."
        ) : invokePhase === "running" ? (
          "Running..."
        ) : invokePhase === "done" ? (
          "✓ Invoked"
        ) : invokePhase === "failed" ? (
          "Try again"
        ) : (
          <>
            <LightningIcon size={14} />
            Invoke
          </>
        )}
      </button>

      <button
        type="button"
        className="btn-text"
        disabled={actionPending || invokePhase !== "idle"}
        onClick={handleDisable}
        aria-label={`Disable ${skillName}`}
      >
        Disable
      </button>

      <button
        type="button"
        className="btn-text btn-text--danger"
        disabled={actionPending || invokePhase !== "idle"}
        onClick={handleArchive}
        aria-label={`Archive ${skillName}`}
      >
        Archive
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
        aria-label={`View task ${taskId}`}
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
      {isDone || isFailed ? (
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

function SuggestChangesExpander({
  skillName,
  leadSlug,
  onClose,
}: {
  skillName: string;
  leadSlug: string;
  onClose: () => void;
}) {
  const [text, setText] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = useCallback(() => {
    const trimmed = text.trim();
    if (!trimmed) return;
    setSubmitting(true);
    createTasks(
      [
        {
          title: `Revise skill: ${skillName}`,
          assignee: leadSlug,
          details: trimmed,
        },
      ],
      { createdBy: "human" },
    )
      .then(() => {
        showNotice(
          `Sent to @${leadSlug}. They'll revise and re-propose.`,
          "success",
        );
        onClose();
      })
      .catch((e: Error) => {
        showNotice(`Couldn't send: ${e.message}`, "error");
      })
      .finally(() => setSubmitting(false));
  }, [text, skillName, leadSlug, onClose]);

  return (
    <div
      className="skill-suggest-expander"
      style={{
        marginTop: 10,
        padding: 12,
        background: "var(--bg-warm, var(--neutral-50))",
        border: "1px solid var(--border)",
        borderRadius: 6,
      }}
    >
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="What needs to change? Be specific."
        rows={4}
        disabled={submitting}
        aria-label={`Suggested revisions for ${skillName}`}
        style={{
          width: "100%",
          minHeight: 80,
          maxHeight: 240,
          padding: 10,
          border: "1px solid var(--border)",
          borderRadius: 4,
          fontFamily: "inherit",
          fontSize: 13,
          color: "var(--text)",
          background: "var(--bg-card, #fff)",
          resize: "vertical",
          boxSizing: "border-box",
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            if (text.trim()) handleSubmit();
          }
          if (e.key === "Escape") {
            e.preventDefault();
            onClose();
          }
        }}
      />
      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          gap: 8,
          marginTop: 8,
        }}
      >
        <button
          type="button"
          className="btn-text"
          onClick={onClose}
          disabled={submitting}
        >
          Cancel
        </button>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={handleSubmit}
          disabled={submitting || !text.trim()}
        >
          {submitting ? "Sending..." : `Send to @${leadSlug} for revision`}
        </button>
      </div>
    </div>
  );
}

interface SkillPreviewBodyProps {
  skill: Skill;
  /** Notifies the parent panel when the editor has unsaved changes. */
  onDirtyChange?: (dirty: boolean) => void;
  /** Called after a successful save with the updated skill. */
  onSaved?: (updated: Skill) => void;
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
function SkillPreviewBody({
  skill,
  onDirtyChange,
  onSaved,
}: SkillPreviewBodyProps) {
  const owners = skill.owner_agents ?? [];
  const isProposed = skill.status === "proposed";

  // baseline = the content the editor is "based on" (the last server-known
  // state). It only updates when the user opens a different skill OR
  // explicitly saves. Comparing draft against baseline determines dirty
  // state; comparing against `skill.content` would let a background
  // refetch silently overwrite the user's typing.
  const [baseline, setBaseline] = useState(skill.content ?? "");
  const [draft, setDraft] = useState(skill.content ?? "");
  const [saving, setSaving] = useState(false);

  // Reset baseline + draft when the user navigates to a DIFFERENT skill.
  // Keyed on skill.name only so post-save updates to skill.content (from
  // the parent passing back the updated Skill) don't blow away chars the
  // user typed in the gap between save resolution and effect run.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see comment
  useEffect(() => {
    const next = skill.content ?? "";
    setBaseline(next);
    setDraft(next);
    onDirtyChange?.(false);
  }, [skill.name]);

  const dirty = isProposed && draft !== baseline;

  // Keep the parent informed of dirty-state so the SidePanel close path
  // can prompt for unsaved edits.
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleSave = useCallback(() => {
    if (!(isProposed && dirty && skill.name)) return;
    setSaving(true);
    // Snapshot the draft we're committing — a chained edit landing while
    // the request is in flight would otherwise change `draft` under us
    // and confuse the post-save baseline.
    const committed = draft;
    const oldString = baseline;
    patchSkill(skill.name, {
      old_string: oldString,
      new_string: committed,
      replace_all: false,
    })
      .then((res) => {
        showNotice("Saved.", "success");
        // Sync baseline to the saved body BEFORE notifying the parent. If
        // the user typed more chars while the request was in flight they
        // remain in `draft`, the dirty flag flips back on, and the
        // editor naturally surfaces the new unsaved delta — instead of
        // the parent's effect resetting `draft` and silently losing them.
        setBaseline(committed);
        const updated: Skill = res.skill ?? { ...skill, content: committed };
        onSaved?.(updated);
      })
      .catch((e: Error) => {
        showNotice(`Couldn't save: ${e.message}`, "error");
      })
      .finally(() => setSaving(false));
  }, [isProposed, dirty, skill, baseline, draft, onSaved]);

  const handleRevert = useCallback(() => {
    setDraft(baseline);
  }, [baseline]);

  return (
    <div
      style={{
        fontSize: 13,
        lineHeight: 1.55,
        color: "var(--text)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
          marginBottom: 12,
        }}
      >
        <OwnersChip slugs={owners} />
        {skill.status ? (
          <span className={STATUS_BADGE_CLASS[skill.status]}>
            {skill.status}
          </span>
        ) : null}
      </div>
      {skill.description ? (
        <p style={{ marginTop: 0, marginBottom: 12 }}>{skill.description}</p>
      ) : null}
      {skill.trigger ? (
        <p
          style={{
            marginTop: 0,
            marginBottom: 12,
            color: "var(--text-secondary)",
            fontStyle: "italic",
          }}
        >
          Trigger: {skill.trigger}
        </p>
      ) : null}

      {isProposed ? (
        <>
          <label
            htmlFor="skill-body-editor"
            style={{
              display: "block",
              fontSize: 12,
              fontWeight: 500,
              color: "var(--text-secondary)",
              marginBottom: 6,
            }}
          >
            SKILL.md body
            <span
              style={{
                fontWeight: 400,
                color: "var(--text-tertiary)",
                marginLeft: 6,
              }}
            >
              (frontmatter is read-only in v1)
            </span>
          </label>
          <textarea
            id="skill-body-editor"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            disabled={saving}
            spellCheck={false}
            aria-label={`Edit body for ${skill.name}`}
            style={{
              width: "100%",
              minHeight: 240,
              maxHeight: "60vh",
              padding: 12,
              background: "var(--bg-warm, var(--neutral-50))",
              border: "1px solid var(--border)",
              borderRadius: 6,
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              lineHeight: 1.5,
              color: "var(--text)",
              resize: "vertical",
              boxSizing: "border-box",
            }}
          />
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "flex-end",
              gap: 8,
              marginTop: 8,
            }}
          >
            {dirty ? (
              <span
                aria-live="polite"
                style={{
                  fontSize: 12,
                  color: "var(--text-tertiary)",
                  marginRight: "auto",
                }}
              >
                Unsaved changes.
              </span>
            ) : null}
            <button
              type="button"
              className="btn-text"
              onClick={handleRevert}
              disabled={saving || !dirty}
            >
              Revert
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={handleSave}
              disabled={saving || !dirty}
              aria-label={`Save edits to ${skill.name}`}
            >
              {saving ? "Saving..." : "Save"}
            </button>
          </div>
          <p
            style={{
              marginTop: 12,
              marginBottom: 0,
              fontSize: 12,
              color: "var(--text-tertiary)",
            }}
          >
            Saving leaves this proposal pending. Approve or reject from the
            interview to promote it.
          </p>
        </>
      ) : skill.content ? (
        <pre
          style={{
            background: "var(--bg-warm, var(--neutral-50))",
            border: "1px solid var(--border)",
            borderRadius: 6,
            padding: 12,
            fontSize: 12,
            fontFamily: "var(--font-mono)",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            margin: 0,
          }}
        >
          {skill.content}
        </pre>
      ) : (
        <div style={{ color: "var(--text-tertiary)", fontSize: 13 }}>
          No body content available.
        </div>
      )}
    </div>
  );
}

function ProposedPreviewBody({ skill }: { skill: Skill }) {
  const body = skill.content ?? "";
  const truncated = body.length > 500 ? `${body.slice(0, 500)}…` : body;
  if (!(truncated || skill.description || skill.trigger)) return null;
  return (
    <div
      style={{
        marginTop: 4,
        marginBottom: 8,
        paddingLeft: 10,
        borderLeft: "3px solid var(--neutral-200, #cfd1d2)",
      }}
    >
      {skill.trigger ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            fontStyle: "italic",
            marginBottom: 6,
          }}
        >
          Trigger: {skill.trigger}
        </div>
      ) : null}
      {truncated ? (
        <div
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            whiteSpace: "pre-wrap",
            fontFamily: "var(--font-mono)",
            lineHeight: 1.5,
          }}
        >
          {truncated}
        </div>
      ) : null}
    </div>
  );
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
function SkillCard({
  skill,
  onPreview,
  leadSlug,
}: {
  skill: Skill;
  onPreview: (s: Skill) => void;
  leadSlug: string;
}) {
  const status = deriveStatus(skill);
  const sourceArticles = skill.metadata?.wuphf?.source_articles ?? [];
  const isArchived = status === "archived";
  const isDisabled = status === "disabled";
  const isProposed = status === "proposed";
  const cardSlugId = `skill-${skill.name || "untitled"}-name`;
  const [suggestOpen, setSuggestOpen] = useState(false);

  return (
    <article
      className={[
        "app-card",
        isProposed ? "app-card--proposed" : "",
        isDisabled ? "app-card--disabled" : "",
      ]
        .filter(Boolean)
        .join(" ")}
      aria-labelledby={cardSlugId}
      style={{
        marginBottom: 8,
        opacity: isArchived ? 0.6 : 1,
      }}
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
        <LightningIcon size={16} />
        <span
          className="app-card-title"
          id={cardSlugId}
          style={{ marginBottom: 0 }}
        >
          {skill.name || "Untitled"}
        </span>
        <span className={STATUS_BADGE_CLASS[status]}>{status}</span>
        {isProposed ? (
          <span className="badge badge-yellow" style={{ marginLeft: 6 }}>
            AI-suggested
          </span>
        ) : null}
        <OwnersChip slugs={skill.owner_agents} />
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

      {isProposed ? (
        <>
          {skill.similar_to_existing ? (
            <div
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 6,
                padding: "3px 8px",
                marginBottom: 8,
                background: "var(--bg-warm, var(--neutral-50))",
                border: "1px solid var(--border)",
                borderRadius: 4,
                fontSize: 12,
                color: "var(--text-secondary)",
              }}
              title={`Similarity score: ${Math.round(skill.similar_to_existing.score * 100)}% (${skill.similar_to_existing.method ?? ""})`}
            >
              <span aria-hidden="true">≈</span>
              Similar to:{" "}
              <strong style={{ fontFamily: "var(--font-mono)" }}>
                {skill.similar_to_existing.slug}
              </strong>
            </div>
          ) : null}
          <ProposedPreviewBody skill={skill} />
          <button
            type="button"
            onClick={() => onPreview(skill)}
            className="btn-text"
            style={{
              padding: "2px 0",
              fontSize: 12,
              color: "var(--accent, #1264a3)",
              marginBottom: 8,
            }}
            aria-label={`View full SKILL.md for ${skill.name}`}
          >
            View full SKILL.md →
          </button>
          <SkillProvenance articles={sourceArticles} />
        </>
      ) : null}

      {skill.source && !isProposed ? (
        <div className="app-card-meta" style={{ marginBottom: 8 }}>
          Source: {skill.source}
        </div>
      ) : null}

      <div
        style={{
          display: "flex",
          gap: 8,
          marginTop: 10,
          alignItems: "center",
          flexWrap: "wrap",
        }}
      >
        <SkillActions
          status={status}
          skillName={skill.name}
          onSuggestChanges={
            isProposed ? () => setSuggestOpen((v) => !v) : undefined
          }
        />
      </div>

      {isProposed && suggestOpen ? (
        <SuggestChangesExpander
          skillName={skill.name}
          leadSlug={leadSlug}
          onClose={() => setSuggestOpen(false)}
        />
      ) : null}
    </article>
  );
}
