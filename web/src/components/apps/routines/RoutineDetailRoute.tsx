import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getScheduler,
  getSchedulerActivity,
  getSchedulerRuns,
  patchSchedulerJob,
  runSchedulerJob,
  type SchedulerActivity,
  type SchedulerJob,
  type SchedulerRun,
} from "../../../api/client";
import { formatRelativeTime } from "../../../lib/format";
import { router } from "../../../lib/router";
import { RoutineEditPanel } from "./RoutineEditPanel";
import { RoutineRevisionsTab } from "./RoutineRevisionsTab";
import { PreviousRuns } from "./RoutineRunsList";
import {
  detailButtonStyle,
  Instructions,
  OwnerSummary,
  Section,
  scheduleSubtext,
} from "./routineDetailShared";
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
 * Full-page routine detail surface. Wires to `/routines/$routineSlug`.
 * The page owns the header (title + Edit / Pause / Run actions) and the
 * tabbed body; each tab module lives in its own file so this surface
 * stays under the file-size lint budget as we add more.
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

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Page-level component coordinates header, tabs, and mutations; refactor is tracked separately.
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

      <DetailHeader
        routine={routine}
        color={color}
        slug={slug}
        editing={editing}
        enabled={enabled}
        togglePending={toggleMutation.isPending}
        runPending={runMutation.isPending}
        onToggleEdit={() => setEditing((v) => !v)}
        onToggleEnabled={() => toggleMutation.mutate()}
        onRunNow={() => runMutation.mutate()}
      />

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

interface DetailHeaderProps {
  routine: SchedulerJob;
  color: string;
  slug: string;
  editing: boolean;
  enabled: boolean;
  togglePending: boolean;
  runPending: boolean;
  onToggleEdit: () => void;
  onToggleEnabled: () => void;
  onRunNow: () => void;
}

function DetailHeader({
  routine,
  color,
  slug,
  editing,
  enabled,
  togglePending,
  runPending,
  onToggleEdit,
  onToggleEnabled,
  onRunNow,
}: DetailHeaderProps) {
  return (
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
          onClick={onToggleEdit}
          data-testid="routine-edit-toggle"
          aria-pressed={editing}
          style={{
            ...detailButtonStyle,
            background: editing ? "var(--accent-bg)" : "var(--bg-card)",
            color: editing ? "var(--accent)" : "var(--text-secondary)",
            borderColor: editing ? "var(--accent)" : "var(--border)",
          }}
        >
          {editing ? "Stop editing" : "Edit"}
        </button>
        <button
          type="button"
          onClick={onToggleEnabled}
          disabled={togglePending}
          aria-pressed={enabled}
          style={{
            ...detailButtonStyle,
            background: enabled ? "var(--bg-card)" : "var(--accent)",
            color: enabled ? "var(--text-secondary)" : "white",
            borderColor: enabled ? "var(--border)" : "var(--accent)",
          }}
        >
          {togglePending ? "…" : enabled ? "Pause routine" : "Resume routine"}
        </button>
        <button
          type="button"
          onClick={onRunNow}
          disabled={runPending || !enabled}
          style={detailButtonStyle}
        >
          {runPending ? "…" : "Run now"}
        </button>
      </div>
    </header>
  );
}

interface TabbedBodyProps {
  routine: SchedulerJob;
  runsQuery: { data: SchedulerRun[] | undefined; isLoading: boolean };
}

type DetailTab = "overview" | "runs" | "activity" | "revisions" | "triggers";

function TabbedBody({ routine, runsQuery }: TabbedBodyProps) {
  const [tab, setTab] = useState<DetailTab>("overview");
  const slug = routine.slug ?? "";
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
      {tab === "activity" && <ActivityTab slug={slug} />}
      {tab === "revisions" && <RoutineRevisionsTab slug={slug} />}
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
      <button type="button" onClick={goBack} style={detailButtonStyle}>
        Back to all routines
      </button>
    </div>
  );
}
