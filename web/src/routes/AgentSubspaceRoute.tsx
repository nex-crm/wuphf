import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { useOfficeMembers } from "../hooks/useMembers";
import { useOfficeTasks } from "../hooks/useOfficeTasks";
import { router } from "../lib/router";
import { directChannelSlug } from "../stores/app";
import { ApiError, getScheduler, getText } from "../api/client";
import { formatRelativeTime } from "../lib/format";
import { AgentProfilePanel } from "../components/agents/AgentProfilePanel";
import { AgentSkillsTab } from "../components/agents/AgentSkillsTab";
import { DMView } from "../components/messages/DMView";
import Notebook from "../components/notebook/Notebook";
import { PixelAvatar } from "../components/ui/PixelAvatar";
import {
  AGENT_SUBSPACE_TABS,
  type AgentSubspaceTab,
} from "./useCurrentRoute";

/**
 * v3 MVP — per-agent subspace shell.
 *
 * Uniform tabs (Chat | App | Notebooks | Calendar | Settings) for every
 * agent. Chat reuses DMView, Notebooks reuses the existing Notebook
 * surface scoped to the agent slug, and App / Calendar / Settings are
 * placeholders for v1. Default tab is "chat" when the URL omits the
 * tab segment.
 */

interface AgentSubspaceRouteProps {
  agentSlug: string;
  tab: AgentSubspaceTab;
}

const TAB_LABELS: Record<AgentSubspaceTab, string> = {
  chat: "Chat",
  app: "App",
  notebooks: "Notebooks",
  skills: "Skills",
  calendar: "Calendar",
  settings: "Settings",
};

function navigateToTab(agentSlug: string, tab: AgentSubspaceTab): void {
  if (tab === "chat") {
    void router.navigate({
      to: "/agents/$agentSlug",
      params: { agentSlug },
    });
    return;
  }
  void router.navigate({
    to: "/agents/$agentSlug/$tab",
    params: { agentSlug, tab },
  });
}

function navigateNotebookCatalog(): void {
  void router.navigate({ to: "/notebooks" });
}

function navigateNotebookAgent(slug: string): void {
  void router.navigate({
    to: "/notebooks/$agentSlug",
    params: { agentSlug: slug },
  });
}

function navigateNotebookEntry(
  agentSlug: string,
  entrySlug: string | null,
): void {
  if (!entrySlug) {
    void router.navigate({
      to: "/notebooks/$agentSlug",
      params: { agentSlug },
    });
    return;
  }
  void router.navigate({
    to: "/notebooks/$agentSlug/$entrySlug",
    params: { agentSlug, entrySlug },
  });
}

function navigateWikiArticle(path: string): void {
  void router.navigate({ to: "/wiki/$", params: { _splat: path } });
}

/**
 * v3 MVP — App tab. The agent's bespoke "app surface", inspired by
 * Claude Artifacts + AG-UI: a sandboxed HTML artifact the agent has
 * authored and stored in its own notebook. Distinct from the Chat
 * tab (conversation) and the Notebooks tab (raw markdown drafts).
 *
 * Storage convention: the artifact lives in the agent's notebook at
 *   agents/{slug}/notebook/app.md
 *
 * The body is raw HTML — markdown permits raw HTML blocks, and using
 * the .md extension keeps the artifact on the same notebook surface
 * (validator + author-owned write rules + git history) as every other
 * agent draft. Notebook entries must sit directly under notebook/
 * (no subdirectories), so the artifact is a single flat file rather
 * than an app/ folder. The agent owns its app: it can rewrite the
 * file at any time. The operator sees the result rendered inside an
 * iframe with sandbox="allow-scripts" (no same-origin, no top-level
 * navigation, no form posts) so the artifact cannot exfil
 * credentials or hijack the parent page.
 */
const APP_ARTIFACT_PATH = "agents/{slug}/notebook/app.md";

function buildAppArtifactPath(slug: string): string {
  return APP_ARTIFACT_PATH.replace("{slug}", slug);
}

interface AppArtifactResult {
  html: string | null;
  notFound: boolean;
}

async function fetchAppArtifact(slug: string): Promise<AppArtifactResult> {
  const path = buildAppArtifactPath(slug);
  try {
    const html = await getText("/notebook/read", { slug, path });
    return { html, notFound: false };
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return { html: null, notFound: true };
    }
    throw err;
  }
}

interface AppTabProps {
  agentSlug: string;
  displayName: string;
}

function AppTab({ agentSlug, displayName }: AppTabProps) {
  const { data, isLoading, isError } = useQuery<AppArtifactResult>({
    queryKey: ["agent-app-artifact", agentSlug],
    queryFn: () => fetchAppArtifact(agentSlug),
    refetchInterval: 10_000,
  });

  // Frame-the-frame: the iframe doc is recomputed only when the HTML
  // changes so the iframe is not torn down on every parent re-render.
  const [srcDoc, setSrcDoc] = useState<string | null>(null);
  useEffect(() => {
    setSrcDoc(data?.html ?? null);
  }, [data?.html]);

  return (
    <div
      data-testid="agent-subspace-app"
      style={{
        flex: 1,
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          padding: "8px 16px",
          borderBottom: "1px solid var(--border)",
          fontSize: 11,
          color: "var(--text-tertiary)",
          letterSpacing: 0.4,
          textTransform: "uppercase",
        }}
      >
        <span>Built by @{agentSlug}</span>
        <span aria-hidden="true">·</span>
        <span style={{ fontFamily: "var(--font-mono, monospace)" }}>
          {buildAppArtifactPath(agentSlug)}
        </span>
      </div>
      <div style={{ flex: 1, minHeight: 0, position: "relative" }}>
        {isLoading ? (
          <ArtifactStatus body="Loading the latest artifact…" />
        ) : isError ? (
          <ArtifactStatus body="Could not load this agent's app artifact." />
        ) : data?.notFound || !srcDoc ? (
          <ArtifactEmpty agentSlug={agentSlug} displayName={displayName} />
        ) : (
          <iframe
            title={`${displayName} app artifact`}
            srcDoc={srcDoc}
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
            style={{
              width: "100%",
              height: "100%",
              border: "none",
              background: "var(--bg, #fff)",
            }}
          />
        )}
      </div>
    </div>
  );
}

function ArtifactStatus({ body }: { body: string }) {
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: "var(--text-tertiary)",
        fontSize: 13,
      }}
    >
      {body}
    </div>
  );
}

function ArtifactEmpty({
  agentSlug,
  displayName,
}: {
  agentSlug: string;
  displayName: string;
}) {
  const path = buildAppArtifactPath(agentSlug);
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 8,
        padding: 32,
        color: "var(--text-tertiary)",
        textAlign: "center",
      }}
    >
      <strong style={{ fontSize: 15, color: "var(--text-secondary)" }}>
        {displayName} hasn&apos;t built an app yet.
      </strong>
      <span style={{ fontSize: 13, maxWidth: 460 }}>
        Agents render their bespoke app surface here. When @{agentSlug} writes
        HTML to its notebook at <code>{path}</code>, it shows up live.
      </span>
    </div>
  );
}

interface CalendarTabProps {
  agentSlug: string;
}

/**
 * v3 MVP — Calendar tab. Agent-scoped list view: this agent's tasks
 * with due dates plus scheduler jobs (cron/triggers) that name the
 * agent. Real data, no real grid yet — that's v1.
 */
function CalendarTab({ agentSlug }: CalendarTabProps) {
  const { data: tasks = [] } = useOfficeTasks();
  const { data: schedulerData } = useQuery({
    queryKey: ["scheduler"],
    queryFn: () => getScheduler(),
    refetchInterval: 15_000,
  });

  const ownedTasks = useMemo(() => {
    return tasks
      .filter((t) => t.owner === agentSlug)
      .filter((t) => Boolean(t.due_at))
      .sort((a, b) => (a.due_at ?? "").localeCompare(b.due_at ?? ""));
  }, [tasks, agentSlug]);

  const jobs = useMemo(() => {
    const all = schedulerData?.jobs ?? [];
    const needle = agentSlug.toLowerCase();
    // SchedulerJob does not carry a first-class agent slug; match the slug
    // field, the job name, the target_id, and the label so a job named
    // "planner.nightly-close" still surfaces on Planner's calendar.
    return all.filter((j) => {
      const haystack = [j.slug, j.name, j.label, j.target_id]
        .map((s) => (s ?? "").toLowerCase());
      return haystack.some((s) => s.includes(needle));
    });
  }, [schedulerData, agentSlug]);

  return (
    <div
      data-testid="agent-subspace-calendar"
      style={{
        flex: 1,
        overflowY: "auto",
        padding: 20,
        display: "flex",
        flexDirection: "column",
        gap: 18,
      }}
    >
      <section>
        <h3
          style={{
            margin: 0,
            fontSize: 12,
            letterSpacing: 0.6,
            textTransform: "uppercase",
            color: "var(--text-tertiary)",
          }}
        >
          Scheduled tasks
        </h3>
        {ownedTasks.length === 0 ? (
          <div
            style={{
              padding: "12px 0",
              fontSize: 13,
              color: "var(--text-tertiary)",
            }}
          >
            No tasks with due dates yet. Tasks with a `due_at` will appear here.
          </div>
        ) : (
          <ul
            style={{
              listStyle: "none",
              padding: 0,
              margin: "10px 0 0",
              display: "flex",
              flexDirection: "column",
              gap: 6,
            }}
          >
            {ownedTasks.map((t) => (
              <li
                key={t.id}
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  gap: 12,
                  padding: "8px 12px",
                  background: "var(--bg-card)",
                  border: "1px solid var(--border)",
                  borderRadius: 6,
                }}
              >
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: 2,
                    minWidth: 0,
                  }}
                >
                  <strong
                    style={{
                      fontSize: 13,
                      color: "var(--text)",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {t.title || t.id}
                  </strong>
                  <span
                    style={{
                      fontSize: 11,
                      color: "var(--text-tertiary)",
                    }}
                  >
                    {t.status} · #{t.channel}
                  </span>
                </div>
                <span style={{ fontSize: 12, color: "var(--text-secondary)" }}>
                  {t.due_at
                    ? new Date(t.due_at).toLocaleDateString()
                    : "no date"}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section>
        <h3
          style={{
            margin: 0,
            fontSize: 12,
            letterSpacing: 0.6,
            textTransform: "uppercase",
            color: "var(--text-tertiary)",
          }}
        >
          Scheduler jobs
        </h3>
        {jobs.length === 0 ? (
          <div
            style={{
              padding: "12px 0",
              fontSize: 13,
              color: "var(--text-tertiary)",
            }}
          >
            No scheduler jobs target @{agentSlug}.
          </div>
        ) : (
          <ul
            style={{
              listStyle: "none",
              padding: 0,
              margin: "10px 0 0",
              display: "flex",
              flexDirection: "column",
              gap: 6,
            }}
          >
            {jobs.map((j) => (
              <li
                key={j.id}
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  gap: 12,
                  padding: "8px 12px",
                  background: "var(--bg-card)",
                  border: "1px solid var(--border)",
                  borderRadius: 6,
                }}
              >
                <div
                  style={{ display: "flex", flexDirection: "column", gap: 2 }}
                >
                  <strong style={{ fontSize: 13, color: "var(--text)" }}>
                    {j.label || j.name || j.slug || j.id || "(unnamed job)"}
                  </strong>
                  <span style={{ fontSize: 11, color: "var(--text-tertiary)" }}>
                    {j.schedule_expr || j.cron ||
                      (j.interval_minutes
                        ? `every ${j.interval_minutes} min`
                        : "manual")}
                  </span>
                </div>
                <span style={{ fontSize: 12, color: "var(--text-secondary)" }}>
                  {j.next_run
                    ? formatRelativeTime(j.next_run)
                    : "no next run"}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

/**
 * v3 MVP — Settings tab. Reuses the AgentProfilePanel which already
 * shows role, instructions, harness, permissions, channels, tasks, and
 * runs. The close handler is a no-op here since the tab is the surface,
 * not a popover.
 */
function SettingsTab({ agentSlug }: { agentSlug: string }) {
  const { data: members = [] } = useOfficeMembers();
  const agent = members.find((m) => m.slug === agentSlug);
  if (!agent) {
    return (
      <div
        data-testid="agent-subspace-settings"
        style={{ padding: 24, color: "var(--text-tertiary)", fontSize: 13 }}
      >
        Loading agent profile…
      </div>
    );
  }
  return (
    <div
      data-testid="agent-subspace-settings"
      style={{ flex: 1, overflowY: "auto" }}
    >
      <AgentProfilePanel agent={agent} onClose={() => {}} />
    </div>
  );
}

export function AgentSubspaceRoute({
  agentSlug,
  tab,
}: AgentSubspaceRouteProps) {
  const { data: members = [] } = useOfficeMembers();
  const agent = useMemo(
    () => members.find((m) => m.slug === agentSlug),
    [members, agentSlug],
  );
  const displayName = agent?.name || agentSlug;
  const role = agent?.role ?? null;
  const channelSlug = useMemo(() => directChannelSlug(agentSlug), [agentSlug]);

  return (
    <div
      className="agent-subspace"
      data-testid="agent-subspace"
      data-agent-slug={agentSlug}
      data-tab={tab}
      style={{
        display: "flex",
        flexDirection: "column",
        flex: 1,
        minHeight: 0,
      }}
    >
      <header
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          padding: "10px 16px",
          borderBottom: "1px solid var(--border)",
          background: "var(--bg-card)",
        }}
      >
        <PixelAvatar slug={agentSlug} size={28} />
        <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
          <strong style={{ fontSize: 14, color: "var(--text)" }}>
            {displayName}
          </strong>
          {role ? (
            <span style={{ fontSize: 11, color: "var(--text-tertiary)" }}>
              {role}
            </span>
          ) : null}
        </div>
      </header>

      <nav
        role="tablist"
        aria-label={`${displayName} subspace`}
        data-testid="agent-subspace-tabs"
        style={{
          display: "flex",
          gap: 2,
          padding: "0 12px",
          borderBottom: "1px solid var(--border)",
          background: "var(--bg-elevated, var(--bg-card))",
        }}
      >
        {AGENT_SUBSPACE_TABS.map((id) => {
          const active = id === tab;
          return (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={active}
              data-testid={`agent-subspace-tab-${id}`}
              onClick={() => navigateToTab(agentSlug, id)}
              style={{
                padding: "10px 14px",
                background: "transparent",
                border: "none",
                borderBottom: active
                  ? "2px solid var(--accent)"
                  : "2px solid transparent",
                color: active ? "var(--text)" : "var(--text-secondary)",
                cursor: "pointer",
                fontSize: 13,
                fontWeight: active ? 600 : 500,
              }}
            >
              {TAB_LABELS[id]}
            </button>
          );
        })}
      </nav>

      <div
        className="agent-subspace-body"
        style={{
          display: "flex",
          flexDirection: "column",
          flex: 1,
          minHeight: 0,
          overflow: "hidden",
        }}
      >
        {tab === "chat" && (
          <DMView agentSlug={agentSlug} channelSlug={channelSlug} />
        )}
        {tab === "notebooks" && (
          <Notebook
            agentSlug={agentSlug}
            entrySlug={null}
            onOpenCatalog={navigateNotebookCatalog}
            onOpenAgent={navigateNotebookAgent}
            onOpenEntry={navigateNotebookEntry}
            onNavigateWiki={navigateWikiArticle}
          />
        )}
        {tab === "app" && (
          <AppTab agentSlug={agentSlug} displayName={displayName} />
        )}
        {tab === "skills" && (
          <AgentSkillsTab agentSlug={agentSlug} displayName={displayName} />
        )}
        {tab === "calendar" && <CalendarTab agentSlug={agentSlug} />}
        {tab === "settings" && <SettingsTab agentSlug={agentSlug} />}
      </div>
    </div>
  );
}

export default AgentSubspaceRoute;
