import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getScheduler,
  getSchedulerActivity,
  getSchedulerRevisions,
  getSchedulerRuns,
  patchSchedulerJob,
  restoreSchedulerRevision,
  runSchedulerJob,
  type SchedulerActivity,
  type SchedulerJob,
  type SchedulerRevision,
  type SchedulerRun,
} from "../../../api/client";
import { formatRelativeTime } from "../../../lib/format";
import { resolveObjectRoute } from "../../../lib/objectRoutes";
import { router } from "../../../lib/router";
import { RoutineEditPanel } from "./RoutineEditPanel";
import {
  lastRunBadge,
  routineColor,
  routineKey,
  routineLabel,
  routineOwner,
  routineSchedule,
} from "./routineModel";

interface RoutineDetailRouteProps {
  routineSlug: string;
}

/**
 * Full-page routine detail surface. Wires to `/routines/$routineSlug`. The
 * drawer-shaped view was useful for a quick peek; this is the canonical
 * place to read instructions and dig into run history.
 */
export function RoutineDetailRoute({ routineSlug }: RoutineDetailRouteProps) {
  const scheduler = useQuery({
    queryKey: ["scheduler"],
    queryFn: () => getScheduler(),
    refetchInterval: 15_000,
  });

  const routine = useMemo<SchedulerJob | null>(() => {
    const jobs = scheduler.data?.jobs ?? [];
    return jobs.find((j) => routineKey(j) === routineSlug) ?? null;
  }, [scheduler.data, routineSlug]);

  if (scheduler.isLoading) {
    return <CenteredMessage>Loading routine…</CenteredMessage>;
  }
  if (scheduler.error) {
    return (
      <CenteredMessage>
        Could not load this routine. Check your connection and try again.
      </CenteredMessage>
    );
  }
  if (!routine) {
    return <NotFound slug={routineSlug} />;
  }

  return <RoutineDetailBody routine={routine} />;
}

interface RoutineDetailBodyProps {
  routine: SchedulerJob;
}

function RoutineDetailBody({ routine }: RoutineDetailBodyProps) {
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState(false);
  const slug = routine.slug ?? routine.id ?? "";
  const color = routineColor(slug);
  const enabled = routine.enabled !== false;

  const runsQuery = useQuery({
    queryKey: ["scheduler-runs", slug],
    queryFn: () => getSchedulerRuns(slug),
    enabled: slug !== "",
    refetchInterval: 15_000,
  });

  const toggleMutation = useMutation({
    mutationFn: () => patchSchedulerJob(slug, { enabled: !enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
    },
  });

  const runMutation = useMutation({
    mutationFn: () => runSchedulerJob(slug),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
      void queryClient.invalidateQueries({
        queryKey: ["scheduler-runs", slug],
      });
    },
  });

  function goBack(): void {
    void router.navigate({ to: "/apps/$appId", params: { appId: "routines" } });
  }

  return (
    <div
      data-testid="routine-detail-route"
      className="routine-detail-page"
      style={{
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        padding: "var(--space-5) var(--space-6)",
        gap: "var(--space-5)",
        maxWidth: 960,
        margin: "0 auto",
        width: "100%",
      }}
    >
      <BackLink onClick={goBack} />

      <header
        style={{
          display: "flex",
          alignItems: "flex-start",
          gap: "var(--space-4)",
          paddingBottom: "var(--space-4)",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <span
          aria-hidden="true"
          style={{
            width: 6,
            alignSelf: "stretch",
            background: color,
            borderRadius: 3,
            minHeight: 48,
          }}
        />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "var(--text-2xs)",
              letterSpacing: "0.16em",
              textTransform: "uppercase",
              color: "var(--text-tertiary)",
              marginBottom: 4,
            }}
          >
            Routine
          </div>
          <h1
            style={{
              margin: 0,
              fontSize: "var(--text-2xl)",
              fontWeight: 600,
              color: "var(--text)",
              letterSpacing: "-0.015em",
            }}
            data-testid="routine-detail-title"
          >
            {routineLabel(routine)}
          </h1>
          <div
            style={{
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
              fontFamily: "var(--font-mono)",
              marginTop: 6,
              wordBreak: "break-all",
            }}
            title={slug}
          >
            {slug}
          </div>
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "var(--space-2)",
            flexShrink: 0,
          }}
        >
          <button
            type="button"
            onClick={() => setEditing((v) => !v)}
            data-testid="routine-edit-toggle"
            aria-pressed={editing}
            style={{
              ...buttonStyle,
              background: editing ? "var(--accent-bg)" : "var(--bg-card)",
              color: editing ? "var(--accent)" : "var(--text-secondary)",
              borderColor: editing ? "var(--accent)" : "var(--border)",
            }}
          >
            {editing ? "Stop editing" : "Edit"}
          </button>
          <button
            type="button"
            onClick={() => toggleMutation.mutate()}
            disabled={toggleMutation.isPending}
            aria-pressed={enabled}
            style={{
              ...buttonStyle,
              background: enabled ? "var(--bg-card)" : "var(--accent)",
              color: enabled ? "var(--text-secondary)" : "white",
              borderColor: enabled ? "var(--border)" : "var(--accent)",
            }}
          >
            {toggleMutation.isPending
              ? "…"
              : enabled
                ? "Pause routine"
                : "Resume routine"}
          </button>
          <button
            type="button"
            onClick={() => runMutation.mutate()}
            disabled={runMutation.isPending || !enabled}
            style={buttonStyle}
          >
            {runMutation.isPending ? "…" : "Run now"}
          </button>
        </div>
      </header>

      {editing ? (
        <RoutineEditPanel
          routine={routine}
          onCancel={() => setEditing(false)}
          onSaved={() => setEditing(false)}
        />
      ) : (
        <TabbedBody routine={routine} runsQuery={runsQuery} />
      )}
    </div>
  );
}

interface TabbedBodyProps {
  routine: SchedulerJob;
  runsQuery: { data: SchedulerRun[] | undefined; isLoading: boolean };
}

type DetailTab = "overview" | "runs" | "activity" | "revisions" | "triggers";

function TabbedBody({ routine, runsQuery }: TabbedBodyProps) {
  const [tab, setTab] = useState<DetailTab>("overview");
  return (
    <>
      <div
        role="tablist"
        aria-label="Routine sections"
        style={{
          display: "flex",
          gap: "var(--space-1)",
          borderBottom: "1px solid var(--border)",
          paddingBottom: 0,
          marginTop: "calc(-1 * var(--space-2))",
        }}
      >
        <TabButton
          id="overview"
          active={tab}
          onClick={setTab}
          label="Overview"
        />
        <TabButton id="runs" active={tab} onClick={setTab} label="Runs" />
        <TabButton
          id="activity"
          active={tab}
          onClick={setTab}
          label="Activity"
        />
        <TabButton
          id="revisions"
          active={tab}
          onClick={setTab}
          label="Revisions"
        />
        <TabButton
          id="triggers"
          active={tab}
          onClick={setTab}
          label="Triggers"
        />
      </div>

      {tab === "overview" && <OverviewTab routine={routine} />}
      {tab === "runs" && (
        <Section title="Previous runs">
          <PreviousRuns
            runs={runsQuery.data}
            loading={runsQuery.isLoading}
            fallbackLastRun={routine.last_run}
            fallbackStatus={routine.last_run_status}
          />
        </Section>
      )}
      {tab === "activity" && <ActivityTab slug={routine.slug ?? ""} />}
      {tab === "revisions" && <RevisionsTab slug={routine.slug ?? ""} />}
      {tab === "triggers" && <TriggersTab routine={routine} />}
    </>
  );
}

interface TabButtonProps {
  id: DetailTab;
  active: DetailTab;
  onClick: (next: DetailTab) => void;
  label: string;
}

function TabButton({ id, active, onClick, label }: TabButtonProps) {
  const isActive = id === active;
  return (
    <button
      type="button"
      role="tab"
      aria-selected={isActive}
      onClick={() => onClick(id)}
      data-testid={`routine-tab-${id}`}
      style={{
        padding: "var(--space-2) var(--space-3)",
        fontSize: "var(--text-sm)",
        fontWeight: 500,
        border: "none",
        background: "transparent",
        color: isActive ? "var(--text)" : "var(--text-secondary)",
        cursor: "pointer",
        borderBottom: isActive
          ? "2px solid var(--accent)"
          : "2px solid transparent",
        marginBottom: -1,
      }}
    >
      {label}
    </button>
  );
}

function OverviewTab({ routine }: { routine: SchedulerJob }) {
  const owner = routineOwner(routine);
  const schedule = routineSchedule(routine);
  const lastRun = lastRunBadge(routine);
  return (
    <>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
          gap: "var(--space-5)",
        }}
        className="routine-detail-grid"
      >
        <Section title="Schedule">
          <div
            style={{
              fontSize: "var(--text-lg)",
              fontFamily:
                schedule.kind === "cron" ? "var(--font-mono)" : undefined,
              color: "var(--text)",
            }}
          >
            {schedule.text}
          </div>
          <div
            style={{
              fontSize: "var(--text-sm)",
              color: "var(--text-tertiary)",
              marginTop: 4,
            }}
          >
            {scheduleSubtext(routine)}
          </div>
        </Section>

        <Section title="Owner">
          <OwnerSummary owner={owner} />
          {lastRun && (
            <span
              className={`badge badge-${
                lastRun.tone === "ok"
                  ? "green"
                  : lastRun.tone === "fail"
                    ? "red"
                    : "neutral"
              }`}
              style={{ marginTop: "var(--space-2)" }}
            >
              Last run: {lastRun.text}
            </span>
          )}
        </Section>
      </div>

      <Section title="Instructions">
        <Instructions routine={routine} />
      </Section>
    </>
  );
}

function ActivityTab({ slug }: { slug: string }) {
  const query = useQuery({
    queryKey: ["scheduler-activity", slug],
    queryFn: () => getSchedulerActivity(slug),
    enabled: slug !== "",
    refetchInterval: 20_000,
  });
  if (query.isLoading) {
    return (
      <Section title="Activity">
        <div
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          Loading activity…
        </div>
      </Section>
    );
  }
  const events = query.data ?? [];
  if (events.length === 0) {
    return (
      <Section title="Activity">
        <div
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          No lifecycle events recorded yet. Create, edit, pause, or trigger this
          routine to see entries here.
        </div>
      </Section>
    );
  }
  return (
    <Section title="Activity">
      <ol
        data-testid="routine-activity-list"
        style={{
          margin: 0,
          padding: 0,
          listStyle: "none",
          display: "flex",
          flexDirection: "column",
          gap: 0,
          border: "1px solid var(--border-light)",
          borderRadius: "var(--radius-sm)",
          overflow: "hidden",
          background: "var(--bg-card)",
        }}
      >
        {events.map((ev) => (
          <ActivityRow key={`${ev.at}-${ev.kind}`} event={ev} />
        ))}
      </ol>
    </Section>
  );
}

function ActivityRow({ event }: { event: SchedulerActivity }) {
  const glyph = activityGlyph(event.kind);
  const at = new Date(event.at);
  return (
    <li
      style={{
        display: "grid",
        gridTemplateColumns: "32px 1fr auto",
        gap: "var(--space-3)",
        alignItems: "center",
        padding: "var(--space-3) var(--space-4)",
        borderBottom: "1px solid var(--border-light)",
      }}
    >
      <span
        aria-hidden="true"
        style={{
          width: 28,
          height: 28,
          borderRadius: "50%",
          background: "var(--bg)",
          border: "1px solid var(--border)",
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          fontSize: "var(--text-sm)",
        }}
      >
        {glyph}
      </span>
      <div style={{ minWidth: 0 }}>
        <div style={{ color: "var(--text)", fontSize: "var(--text-sm)" }}>
          {event.summary}
        </div>
        {event.detail && (
          <div
            style={{
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
            }}
          >
            {event.detail}
          </div>
        )}
      </div>
      <div
        style={{
          fontSize: "var(--text-xs)",
          color: "var(--text-tertiary)",
          textAlign: "right",
          fontFamily: "var(--font-mono)",
        }}
      >
        {Number.isNaN(at.getTime()) ? event.at : formatRelativeTime(event.at)}
        <div>{event.actor || ""}</div>
      </div>
    </li>
  );
}

function activityGlyph(kind: string): string {
  switch (kind) {
    case "created":
      return "✨";
    case "edited":
      return "✎";
    case "paused":
      return "⏸";
    case "resumed":
      return "▶";
    case "triggered":
      return "⟳";
    case "throttled":
      return "⚙";
    case "revision_restored":
      return "↺";
    case "archived":
      return "🗄";
    default:
      return "•";
  }
}

function RevisionsTab({ slug }: { slug: string }) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<number | null>(null);
  const query = useQuery({
    queryKey: ["scheduler-revisions", slug],
    queryFn: () => getSchedulerRevisions(slug),
    enabled: slug !== "",
    refetchInterval: 30_000,
  });
  const restoreMutation = useMutation({
    mutationFn: (version: number) => restoreSchedulerRevision(slug, version),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["scheduler"] });
      void queryClient.invalidateQueries({
        queryKey: ["scheduler-revisions", slug],
      });
      void queryClient.invalidateQueries({
        queryKey: ["scheduler-activity", slug],
      });
    },
  });

  if (query.isLoading) {
    return (
      <Section title="Revisions">
        <div
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          Loading revisions…
        </div>
      </Section>
    );
  }
  const revisions = query.data ?? [];
  if (revisions.length === 0) {
    return (
      <Section title="Revisions">
        <div
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          No revisions saved yet. A revision is created the first time the
          routine is saved and on every subsequent edit.
        </div>
      </Section>
    );
  }
  const current = revisions[0];
  const selectedRev =
    selected !== null
      ? (revisions.find((r) => r.version === selected) ?? null)
      : null;
  return (
    <Section title="Revisions">
      <div
        data-testid="routine-revisions-list"
        style={{
          display: "flex",
          flexDirection: "column",
          border: "1px solid var(--border-light)",
          borderRadius: "var(--radius-sm)",
          overflow: "hidden",
          background: "var(--bg-card)",
        }}
      >
        {revisions.map((rev) => {
          const isCurrent = rev.version === current.version;
          const isOpen = selected === rev.version;
          return (
            <div
              key={rev.version}
              style={{ borderBottom: "1px solid var(--border-light)" }}
            >
              <button
                type="button"
                onClick={() => setSelected(isOpen ? null : rev.version)}
                aria-expanded={isOpen}
                style={{
                  display: "grid",
                  gridTemplateColumns: "80px 1fr auto auto",
                  gap: "var(--space-3)",
                  alignItems: "center",
                  padding: "var(--space-3) var(--space-4)",
                  width: "100%",
                  background: "transparent",
                  border: "none",
                  cursor: "pointer",
                  textAlign: "left",
                }}
              >
                <span
                  className={`badge badge-${isCurrent ? "green" : "neutral"}`}
                >
                  v{rev.version}
                </span>
                <div style={{ minWidth: 0 }}>
                  <div
                    style={{ color: "var(--text)", fontSize: "var(--text-sm)" }}
                  >
                    {rev.change_note || "(no change note)"}
                  </div>
                  <div
                    style={{
                      fontSize: "var(--text-xs)",
                      color: "var(--text-tertiary)",
                    }}
                  >
                    {formatRelativeTime(rev.created_at)}
                    {rev.author && ` · ${rev.author}`}
                    {isCurrent && " · current"}
                  </div>
                </div>
                {!isCurrent && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      restoreMutation.mutate(rev.version);
                    }}
                    disabled={restoreMutation.isPending}
                    style={{
                      padding: "3px var(--space-2)",
                      fontSize: "var(--text-xs)",
                      fontWeight: 500,
                      border: "1px solid var(--border)",
                      borderRadius: "var(--radius-sm)",
                      background: "var(--bg)",
                      color: "var(--text-secondary)",
                      cursor: "pointer",
                    }}
                  >
                    Restore
                  </button>
                )}
                <span
                  aria-hidden="true"
                  style={{
                    fontSize: "var(--text-xs)",
                    color: "var(--text-tertiary)",
                    transform: isOpen ? "rotate(90deg)" : "none",
                    transition: "transform 120ms ease",
                    width: 10,
                  }}
                >
                  ▸
                </span>
              </button>
              {isOpen && <RevisionDiff current={current} target={rev} />}
            </div>
          );
        })}
      </div>
      {selectedRev && selectedRev.version !== current.version && (
        <div
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            marginTop: "var(--space-2)",
          }}
        >
          Comparing v{selectedRev.version} against v{current.version} (current).
          Restoring will save the current state as a new revision first.
        </div>
      )}
    </Section>
  );
}

interface RevisionDiffProps {
  current: SchedulerRevision;
  target: SchedulerRevision;
}

function RevisionDiff({ current, target }: RevisionDiffProps) {
  const rows: Array<{
    label: string;
    current: string;
    target: string;
    changed: boolean;
  }> = [
    diffRow("Label", current.label, target.label),
    diffRow(
      "Schedule expr",
      current.schedule_expr ?? "",
      target.schedule_expr ?? "",
    ),
    diffRow(
      "Interval (min)",
      String(current.interval_minutes ?? 0),
      String(target.interval_minutes ?? 0),
    ),
    diffRow(
      "Target",
      `${current.target_type ?? ""}:${current.target_id ?? ""}`,
      `${target.target_type ?? ""}:${target.target_id ?? ""}`,
    ),
    diffRow("Payload", current.payload ?? "", target.payload ?? ""),
    diffRow(
      "Enabled",
      current.enabled ? "true" : "false",
      target.enabled ? "true" : "false",
    ),
  ];
  return (
    <div
      data-testid="routine-revision-diff"
      style={{
        padding: "var(--space-3) var(--space-4)",
        background: "var(--bg)",
        borderTop: "1px solid var(--border-light)",
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
        fontSize: "var(--text-sm)",
      }}
    >
      {rows.map((row) => (
        <div
          key={row.label}
          style={{
            display: "grid",
            gridTemplateColumns: "140px 1fr 1fr",
            gap: "var(--space-2)",
            alignItems: "start",
            opacity: row.changed ? 1 : 0.5,
          }}
        >
          <span
            style={{
              color: "var(--text-tertiary)",
              fontFamily: "var(--font-mono)",
              fontSize: "var(--text-xs)",
            }}
          >
            {row.label}
          </span>
          <span
            style={{
              color: row.changed ? "var(--text)" : "var(--text-tertiary)",
              fontFamily: "var(--font-mono)",
              fontSize: "var(--text-xs)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            <em
              style={{
                color: "var(--text-tertiary)",
                fontFamily: "var(--font-sans)",
                marginRight: 6,
                fontStyle: "normal",
                fontSize: "var(--text-2xs)",
                textTransform: "uppercase",
                letterSpacing: "0.08em",
              }}
            >
              this revision
            </em>
            {row.target || "—"}
          </span>
          <span
            style={{
              color: row.changed ? "var(--text)" : "var(--text-tertiary)",
              fontFamily: "var(--font-mono)",
              fontSize: "var(--text-xs)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            <em
              style={{
                color: "var(--text-tertiary)",
                fontFamily: "var(--font-sans)",
                marginRight: 6,
                fontStyle: "normal",
                fontSize: "var(--text-2xs)",
                textTransform: "uppercase",
                letterSpacing: "0.08em",
              }}
            >
              current
            </em>
            {row.current || "—"}
          </span>
        </div>
      ))}
    </div>
  );
}

function diffRow(label: string, current: string, target: string) {
  return { label, current, target, changed: current !== target };
}

function TriggersTab({ routine }: { routine: SchedulerJob }) {
  const schedule = routineSchedule(routine);
  return (
    <Section title="Triggers">
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--space-3)",
        }}
      >
        <TriggerCard
          icon="⏱"
          title="Schedule"
          status="active"
          body={
            <div>
              <div
                style={{
                  fontFamily:
                    schedule.kind === "cron" ? "var(--font-mono)" : undefined,
                  color: "var(--text)",
                }}
              >
                {schedule.text}
              </div>
              <div
                style={{
                  fontSize: "var(--text-xs)",
                  color: "var(--text-tertiary)",
                  marginTop: 4,
                }}
              >
                {scheduleSubtext(routine)}
              </div>
            </div>
          }
        />
        <TriggerCard
          icon="🌐"
          title="Webhook"
          status="coming-soon"
          body="External callers will be able to fire this routine over HTTP with an HMAC signature."
        />
        <TriggerCard
          icon="✦"
          title="Context change"
          status="coming-soon"
          body="Fire the routine when a watched context value (entity attribute, tag, status) changes in the graph."
        />
      </div>
    </Section>
  );
}

interface TriggerCardProps {
  icon: string;
  title: string;
  status: "active" | "coming-soon";
  body: React.ReactNode;
}

function TriggerCard({ icon, title, status, body }: TriggerCardProps) {
  const isComingSoon = status === "coming-soon";
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "44px 1fr auto",
        gap: "var(--space-3)",
        alignItems: "start",
        padding: "var(--space-4)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-sm)",
        background: "var(--bg-card)",
        opacity: isComingSoon ? 0.7 : 1,
      }}
    >
      <span
        aria-hidden="true"
        style={{
          width: 40,
          height: 40,
          borderRadius: 8,
          background: "var(--bg)",
          border: "1px solid var(--border)",
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          fontSize: 18,
        }}
      >
        {icon}
      </span>
      <div style={{ minWidth: 0 }}>
        <div
          style={{
            fontSize: "var(--text-base)",
            fontWeight: 600,
            color: "var(--text)",
          }}
        >
          {title}
        </div>
        <div
          style={{
            fontSize: "var(--text-sm)",
            color: "var(--text-secondary)",
            marginTop: 4,
          }}
        >
          {body}
        </div>
      </div>
      <span
        className={`badge badge-${isComingSoon ? "yellow" : "green"}`}
        style={{ alignSelf: "start" }}
      >
        {isComingSoon ? "Coming soon" : "Active"}
      </span>
    </div>
  );
}

// ── Sections + helpers ────────────────────────────────────────────

const buttonStyle: React.CSSProperties = {
  padding: "5px var(--space-3)",
  fontSize: "var(--text-sm)",
  fontWeight: 500,
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-sm)",
  background: "var(--bg-card)",
  color: "var(--text-secondary)",
  cursor: "pointer",
  transition: "background 120ms ease, border-color 120ms ease",
};

interface SectionProps {
  title: string;
  children: React.ReactNode;
}

function Section({ title, children }: SectionProps) {
  return (
    <section
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
      }}
    >
      <h2
        style={{
          fontSize: "var(--text-2xs)",
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.12em",
          color: "var(--text-tertiary)",
          margin: 0,
          fontFamily: "var(--font-mono)",
        }}
      >
        {title}
      </h2>
      {children}
    </section>
  );
}

function scheduleSubtext(routine: SchedulerJob): string {
  const next = routine.next_run || routine.due_at;
  if (!next) return "No next run scheduled";
  return `Next run ${formatRelativeTime(next)} · ${new Date(next).toLocaleString()}`;
}

interface OwnerSummaryProps {
  owner: ReturnType<typeof routineOwner>;
}

function OwnerSummary({ owner }: OwnerSummaryProps) {
  if (owner.kind === "system") {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--space-1)",
        }}
      >
        <span
          className="badge badge-neutral"
          style={{ alignSelf: "flex-start" }}
        >
          system
        </span>
        <span
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          Managed by the broker run-loop. Disable to pause it.
        </span>
      </div>
    );
  }
  if (owner.kind === "workflow") {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--space-1)",
        }}
      >
        <span
          className="badge badge-yellow"
          style={{ alignSelf: "flex-start" }}
        >
          workflow
        </span>
        <span
          style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
        >
          Executed by the workflow runner.
        </span>
      </div>
    );
  }
  if (owner.kind === "unassigned" || !owner.slug) {
    return (
      <span
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        Unassigned
      </span>
    );
  }
  const route = resolveObjectRoute({ kind: "agent", slug: owner.slug });
  return (
    <a
      href={route.href}
      style={{
        fontSize: "var(--text-lg)",
        color: "var(--accent)",
        textDecoration: "none",
        fontWeight: 500,
      }}
    >
      {owner.slug}
    </a>
  );
}

interface InstructionsProps {
  routine: SchedulerJob;
}

function Instructions({ routine }: InstructionsProps) {
  const rows: Array<{ label: string; value: string | undefined }> = [
    { label: "Kind", value: routine.kind },
    { label: "Target type", value: routine.target_type },
    { label: "Target id", value: routine.target_id },
    { label: "Provider", value: routine.provider },
  ];
  const populated = rows.filter(
    (r) => typeof r.value === "string" && r.value.trim() !== "",
  );
  const payload = routine.payload?.trim();

  if (populated.length === 0 && !payload) {
    return (
      <div
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        No additional instructions on file. The agent infers what to do from the
        routine slug.
      </div>
    );
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-2)",
        padding: "var(--space-4)",
        background: "var(--bg-card)",
        border: "1px solid var(--border-light)",
        borderRadius: "var(--radius-sm)",
      }}
    >
      {populated.map((row) => (
        <div
          key={row.label}
          style={{
            display: "grid",
            gridTemplateColumns: "140px 1fr",
            gap: "var(--space-3)",
            fontSize: "var(--text-sm)",
          }}
        >
          <span
            style={{
              color: "var(--text-tertiary)",
              fontFamily: "var(--font-mono)",
              fontSize: "var(--text-xs)",
            }}
          >
            {row.label}
          </span>
          <span
            style={{ color: "var(--text)", fontFamily: "var(--font-mono)" }}
          >
            {row.value}
          </span>
        </div>
      ))}
      {payload && (
        <details style={{ marginTop: "var(--space-2)" }}>
          <summary
            style={{
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
              cursor: "pointer",
              fontFamily: "var(--font-mono)",
            }}
          >
            Payload
          </summary>
          <pre
            style={{
              margin: "var(--space-2) 0 0",
              padding: "var(--space-3)",
              background: "var(--bg)",
              border: "1px solid var(--border-light)",
              borderRadius: "var(--radius-sm)",
              fontSize: "var(--text-xs)",
              fontFamily: "var(--font-mono)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {prettyPayload(payload)}
          </pre>
        </details>
      )}
    </div>
  );
}

function prettyPayload(raw: string): string {
  try {
    const parsed: unknown = JSON.parse(raw);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return raw;
  }
}

interface PreviousRunsProps {
  runs: SchedulerRun[] | undefined;
  loading: boolean;
  fallbackLastRun?: string;
  fallbackStatus?: string;
}

function PreviousRuns({
  runs,
  loading,
  fallbackLastRun,
  fallbackStatus,
}: PreviousRunsProps) {
  if (loading) {
    return (
      <div
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        Loading run history…
      </div>
    );
  }

  if ((!runs || runs.length === 0) && fallbackLastRun) {
    const fallback: SchedulerRun = {
      slug: "",
      started_at: fallbackLastRun,
      status: fallbackStatus || "ok",
    };
    return <RunRows runs={[fallback]} />;
  }

  if (!runs || runs.length === 0) {
    return (
      <div
        style={{ fontSize: "var(--text-sm)", color: "var(--text-tertiary)" }}
      >
        No runs recorded yet.
      </div>
    );
  }

  return <RunRows runs={runs} />;
}

function runRowKey(run: SchedulerRun): string {
  return `${run.started_at}-${run.status}-${run.triggered_by ?? ""}`;
}

function RunRows({ runs }: { runs: SchedulerRun[] }) {
  return (
    <div
      data-testid="routine-runs"
      style={{
        display: "flex",
        flexDirection: "column",
        border: "1px solid var(--border-light)",
        borderRadius: "var(--radius-sm)",
        overflow: "hidden",
        background: "var(--bg-card)",
      }}
    >
      {runs.map((run, idx) => (
        <RunRow key={runRowKey(run)} run={run} isFirst={idx === 0} />
      ))}
    </div>
  );
}

interface RunStatusVisual {
  glyph: string;
  label: string;
  tone: "green" | "red" | "neutral" | "yellow";
}

function describeRunStatus(raw: string): RunStatusVisual {
  const s = (raw || "").toLowerCase();
  if (s === "ok" || s === "success" || s === "completed" || s === "done") {
    return { glyph: "✓", label: "Completed", tone: "green" };
  }
  if (s === "failed" || s === "error") {
    return { glyph: "✗", label: "Failed", tone: "red" };
  }
  if (s === "triggered") {
    return { glyph: "⟳", label: "Triggered", tone: "yellow" };
  }
  if (s === "running" || s === "in_progress") {
    return { glyph: "•", label: "Running", tone: "yellow" };
  }
  return { glyph: "—", label: raw || "Unknown", tone: "neutral" };
}

function RunRow({ run, isFirst }: { run: SchedulerRun; isFirst: boolean }) {
  const [open, setOpen] = useState(isFirst);
  const status = describeRunStatus(run.status);
  const start = new Date(run.started_at);
  const finished = run.finished_at ? new Date(run.finished_at) : null;
  const duration =
    finished && !Number.isNaN(start.getTime())
      ? formatDuration(start, finished)
      : null;

  return (
    <div
      data-testid="routine-run-row"
      data-run-status={status.tone}
      style={{
        borderBottom: "1px solid var(--border-light)",
      }}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        style={{
          display: "grid",
          gridTemplateColumns: "120px 1fr auto auto",
          gap: "var(--space-3)",
          alignItems: "center",
          padding: "var(--space-3) var(--space-4)",
          fontSize: "var(--text-sm)",
          width: "100%",
          background: "transparent",
          border: "none",
          cursor: "pointer",
          color: "inherit",
          textAlign: "left",
        }}
      >
        <span
          className={`badge badge-${status.tone}`}
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: 4,
            justifySelf: "start",
          }}
        >
          <span aria-hidden="true">{status.glyph}</span> {status.label}
        </span>
        <div style={{ minWidth: 0 }}>
          <div style={{ color: "var(--text)" }}>
            {Number.isNaN(start.getTime())
              ? run.started_at
              : formatRelativeTime(run.started_at)}
          </div>
          {(run.output_summary || run.message) && (
            <div
              style={{
                fontSize: "var(--text-xs)",
                color: "var(--text-tertiary)",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
              title={run.output_summary || run.message}
            >
              {run.output_summary || run.message}
            </div>
          )}
        </div>
        <span
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            fontFamily: "var(--font-mono)",
          }}
        >
          {duration ?? "—"}
        </span>
        <span
          aria-hidden="true"
          style={{
            fontSize: "var(--text-xs)",
            color: "var(--text-tertiary)",
            transform: open ? "rotate(90deg)" : "none",
            transition: "transform 120ms ease",
            display: "inline-block",
            width: 10,
          }}
        >
          ▸
        </span>
      </button>

      {open && <RunDetailPanel run={run} />}
    </div>
  );
}

function RunDetailPanel({ run }: { run: SchedulerRun }) {
  const start = new Date(run.started_at);
  const finished = run.finished_at ? new Date(run.finished_at) : null;
  const targetHref = targetLinkFor(run);

  return (
    <div
      data-testid="routine-run-detail"
      style={{
        padding: "var(--space-4)",
        background: "var(--bg)",
        borderTop: "1px solid var(--border-light)",
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-3)",
        fontSize: "var(--text-sm)",
      }}
    >
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "140px 1fr",
          gap: "var(--space-3) var(--space-4)",
          rowGap: "var(--space-2)",
        }}
      >
        <DetailRow label="Started">
          {Number.isNaN(start.getTime())
            ? run.started_at
            : start.toLocaleString()}
        </DetailRow>
        {finished && (
          <DetailRow label="Finished">{finished.toLocaleString()}</DetailRow>
        )}
        {run.triggered_by && (
          <DetailRow label="Triggered by">
            <span style={{ fontFamily: "var(--font-mono)" }}>
              {run.triggered_by}
            </span>
          </DetailRow>
        )}
        {(run.target_type || run.target_id) && (
          <DetailRow label="Target">
            {targetHref ? (
              <a
                href={targetHref}
                style={{
                  color: "var(--accent)",
                  textDecoration: "none",
                  fontFamily: "var(--font-mono)",
                }}
              >
                {run.target_type}:{run.target_id}
              </a>
            ) : (
              <span style={{ fontFamily: "var(--font-mono)" }}>
                {[run.target_type, run.target_id].filter(Boolean).join(":")}
              </span>
            )}
          </DetailRow>
        )}
      </div>

      {run.output_summary && (
        <DetailBlock label="Summary">
          <div style={{ color: "var(--text)", lineHeight: 1.55 }}>
            {run.output_summary}
          </div>
        </DetailBlock>
      )}

      {run.events && run.events.length > 0 && (
        <DetailBlock label="Events">
          <ol
            style={{
              margin: 0,
              padding: 0,
              listStyle: "none",
              display: "flex",
              flexDirection: "column",
              gap: 2,
            }}
          >
            {run.events.map((ev, i) => (
              <li
                key={`${i}-${ev.slice(0, 24)}`}
                style={{
                  display: "grid",
                  gridTemplateColumns: "24px 1fr",
                  gap: "var(--space-2)",
                  fontFamily: "var(--font-mono)",
                  fontSize: "var(--text-xs)",
                  color: "var(--text-secondary)",
                }}
              >
                <span style={{ color: "var(--text-tertiary)" }}>
                  {String(i + 1).padStart(2, "0")}
                </span>
                <span>{ev}</span>
              </li>
            ))}
          </ol>
        </DetailBlock>
      )}

      {run.error && (
        <DetailBlock label="Error" tone="red">
          <pre
            style={{
              margin: 0,
              padding: "var(--space-3)",
              background: "var(--red-bg)",
              color: "var(--red)",
              border: "1px solid var(--red)",
              borderRadius: "var(--radius-sm)",
              fontSize: "var(--text-xs)",
              fontFamily: "var(--font-mono)",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {run.error}
          </pre>
        </DetailBlock>
      )}

      {run.message && !run.output_summary && !run.error && (
        <DetailBlock label="Message">
          <div style={{ color: "var(--text-secondary)" }}>{run.message}</div>
        </DetailBlock>
      )}

      {!(run.output_summary || run.error || run.message) &&
        (!run.events || run.events.length === 0) && (
          <div
            style={{
              padding: "var(--space-3) 0",
              fontSize: "var(--text-xs)",
              color: "var(--text-tertiary)",
              fontStyle: "italic",
            }}
          >
            The runner didn't emit a detail trace for this fire. Status and
            timing above are all we have on file.
          </div>
        )}
    </div>
  );
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <>
      <span
        style={{
          color: "var(--text-tertiary)",
          fontFamily: "var(--font-mono)",
          fontSize: "var(--text-xs)",
        }}
      >
        {label}
      </span>
      <span style={{ color: "var(--text)" }}>{children}</span>
    </>
  );
}

function DetailBlock({
  label,
  tone,
  children,
}: {
  label: string;
  tone?: "red";
  children: React.ReactNode;
}) {
  return (
    <section style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <h4
        style={{
          margin: 0,
          fontSize: "var(--text-2xs)",
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.12em",
          color: tone === "red" ? "var(--red)" : "var(--text-tertiary)",
          fontFamily: "var(--font-mono)",
        }}
      >
        {label}
      </h4>
      {children}
    </section>
  );
}

/**
 * Resolve a click-through link for the run's target. Workflow runs deep-link
 * into the workflow registry; agent runs into the agent profile. Everything
 * else falls back to a plain text rendering.
 */
function targetLinkFor(run: SchedulerRun): string | null {
  if (!(run.target_id && run.target_type)) return null;
  if (run.target_type === "agent") {
    const route = resolveObjectRoute({ kind: "agent", slug: run.target_id });
    return route.href;
  }
  if (run.target_type === "workflow") {
    return `#/apps/skills?workflow=${encodeURIComponent(run.target_id)}`;
  }
  return null;
}

function formatDuration(start: Date, end: Date): string {
  const ms = end.getTime() - start.getTime();
  if (ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.round((ms % 60_000) / 1000);
  return `${minutes}m ${seconds}s`;
}

interface BackLinkProps {
  onClick: () => void;
}

function BackLink({ onClick }: BackLinkProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        background: "transparent",
        border: "none",
        padding: 0,
        color: "var(--text-secondary)",
        fontSize: "var(--text-sm)",
        cursor: "pointer",
        alignSelf: "flex-start",
        fontFamily: "var(--font-mono)",
        letterSpacing: "0.04em",
      }}
      data-testid="routine-detail-back"
    >
      ← All routines
    </button>
  );
}

function CenteredMessage({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        padding: "var(--space-7) var(--space-5)",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: "var(--text-sm)",
      }}
    >
      {children}
    </div>
  );
}

interface NotFoundProps {
  slug: string;
}

function NotFound({ slug }: NotFoundProps) {
  function goBack(): void {
    void router.navigate({ to: "/apps/$appId", params: { appId: "routines" } });
  }
  return (
    <div
      data-testid="routine-detail-not-found"
      style={{
        padding: "var(--space-7) var(--space-5)",
        textAlign: "center",
        color: "var(--text-tertiary)",
        fontSize: "var(--text-md)",
        lineHeight: 1.6,
        maxWidth: 520,
        margin: "0 auto",
      }}
    >
      <div
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: "var(--text-2xs)",
          letterSpacing: "0.16em",
          textTransform: "uppercase",
          marginBottom: "var(--space-2)",
        }}
      >
        Missing
      </div>
      <div
        style={{
          fontSize: "var(--text-xl)",
          fontWeight: 600,
          color: "var(--text-secondary)",
          marginBottom: "var(--space-2)",
          letterSpacing: "-0.01em",
        }}
      >
        Routine not found
      </div>
      <div
        style={{ fontSize: "var(--text-sm)", marginBottom: "var(--space-4)" }}
      >
        No routine with slug{" "}
        <code style={{ fontFamily: "var(--font-mono)" }}>{slug}</code> exists in
        this office. It may have been deleted, or the link is from a different
        workspace.
      </div>
      <button type="button" onClick={goBack} style={buttonStyle}>
        Back to all routines
      </button>
    </div>
  );
}
