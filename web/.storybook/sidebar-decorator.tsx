/**
 * Sidebar story decorator.
 *
 * Mounts the REAL Sidebar / AgentList / ChannelList / AppList / IssuesGroup /
 * WorkspaceSummary etc. — no DOM duplication. We give them:
 *
 *   1. A fresh QueryClient seeded with sample data (members, channels, tasks,
 *      usage, inbox items, reviews) so the live hooks (`useOfficeMembers`,
 *      `useChannels`, `useQuery`) resolve synchronously to non-empty payloads.
 *   2. A memory-history TanStack Router instance that uses the actual route
 *      tree from `lib/router` so `useCurrentRoute` / `useCurrentApp` resolve
 *      the same route IDs the production code expects.
 *   3. A pinned `rootRoute.component` that just renders whatever the story
 *      handed us via context — so RouterProvider mounts the story content
 *      instead of trying to load the real RootRoute shell.
 *   4. Pre-set sidebar-open Zustand state so all four collapsible sections
 *      (Agents / Channels / Issues / Tools) start expanded.
 *
 * Net effect: stories render the real components and exercise the real CSS,
 * which is what we want when "sidebar in storybook is not from this reality"
 * is the alternative.
 */

import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
} from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  RouterProvider,
} from "@tanstack/react-router";

import type { OfficeMember } from "../src/api/client";
import type { Channel } from "../src/api/client";
import type { Task } from "../src/api/tasks";
import { createAppRouter, rootRoute } from "../src/lib/router";
import { useAppStore } from "../src/stores/app";

// Render whatever the consumer placed in StoryOutletContext. RouterProvider
// will mount this once the rootRoute matches.
const StoryOutletContext = createContext<ReactNode>(null);

function StoryShell() {
  return <>{useContext(StoryOutletContext)}</>;
}

// Attach once at module load. The live app's main.tsx also calls
// rootRoute.update — but main.tsx never runs in Storybook, so this is
// the only assignment that matters here.
rootRoute.update({ component: StoryShell });

const DEFAULT_MEMBERS: OfficeMember[] = [
  {
    slug: "atlas",
    name: "Atlas",
    role: "engineer",
    status: "active",
    task: "writing migration plan",
    online: true,
    provider: "claude-code",
  },
  {
    slug: "lina",
    name: "Lina",
    role: "designer",
    status: "active",
    task: "wireframing the inbox",
    online: true,
    provider: "codex",
  },
  {
    slug: "sage",
    name: "Sage",
    role: "writer",
    status: "active",
    task: "drafting the FAQ",
    online: true,
    provider: "opencode",
  },
  {
    slug: "ops",
    name: "Ops",
    role: "ops",
    status: "lurking",
    task: "watching CI",
    online: false,
    provider: "hermes-agent",
  },
];

const DEFAULT_CHANNELS: Channel[] = [
  { slug: "architecture", name: "architecture", description: "How we build" },
  { slug: "deploys", name: "deploys", description: "Shipping log" },
  { slug: "wiki", name: "wiki", description: "Wiki edits" },
  { slug: "incidents", name: "incidents", description: "Postmortems" },
];

const DEFAULT_TASKS: Task[] = Array.from({ length: 8 }, (_, i) => ({
  id: `task-${i + 1}`,
  title: `Open task ${i + 1}`,
  status: "active",
  owner: ["atlas", "lina", "sage", "ops"][i % 4],
})) as Task[];

const DEFAULT_ISSUES: Task[] = [
  {
    id: "issue-1",
    title: "Auth token rotation",
    status: "review",
    pipeline_stage: "review",
  } as Task,
  {
    id: "issue-2",
    title: "Calendar sync drift",
    status: "blocked",
    pipeline_stage: "running",
    blocked: true,
  } as Task,
];

const DEFAULT_USAGE = {
  agents: {
    atlas: { total_tokens: 6200, cost_usd: 0.31 },
    lina: { total_tokens: 4800, cost_usd: 0.28 },
    sage: { total_tokens: 3200, cost_usd: 0.25 },
  },
  total: { total_tokens: 14200, cost_usd: 0.84 },
};

export interface SidebarContextProps {
  /** Hash URL to navigate the memory router to (e.g. "/channels/architecture"). */
  initialUrl?: string;
  /** Override sample members. */
  members?: OfficeMember[];
  /** Override sample channels. */
  channels?: Channel[];
  /** Override sample tasks. */
  tasks?: Task[];
  /** Override sidebar Issues entries. */
  issues?: Task[];
  /** Unread counts per channel slug. */
  unreadByChannel?: Record<string, number>;
  children: ReactNode;
}

/**
 * Wrap a story in providers so the real sidebar components render with
 * non-empty data.
 */
export function SidebarContext({
  initialUrl = "/channels/architecture",
  members = DEFAULT_MEMBERS,
  channels = DEFAULT_CHANNELS,
  tasks = DEFAULT_TASKS,
  issues = DEFAULT_ISSUES,
  unreadByChannel = { deploys: 2, incidents: 12 },
  children,
}: SidebarContextProps) {
  const queryClient = useMemo(() => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: {
          staleTime: Number.POSITIVE_INFINITY,
          gcTime: Number.POSITIVE_INFINITY,
          refetchOnMount: false,
          refetchOnWindowFocus: false,
          refetchOnReconnect: false,
          retry: false,
        },
      },
    });
    qc.setQueryData(["office-members"], { members });
    qc.setQueryData(["channels"], { channels });
    qc.setQueryData(["office-tasks-active"], { tasks });
    qc.setQueryData(["office-tasks"], { tasks });
    qc.setQueryData(["usage"], DEFAULT_USAGE);
    qc.setQueryData(["issues", "sidebar"], { tasks: issues });
    qc.setQueryData(["inbox-badge"], {
      items: [
        { kind: "request", id: "r1" },
        { kind: "review", id: "r2" },
        { kind: "task", id: "t1", task: { state: "decision" } },
      ],
    });
    qc.setQueryData(["reviews-badge"], [
      { id: "rv1", state: "pending" },
      { id: "rv2", state: "in-review" },
    ]);
    qc.setQueryData(["requests", ""], { requests: [] });
    return qc;
  }, [members, channels, tasks, issues]);

  const router = useMemo(
    () =>
      createAppRouter(
        createMemoryHistory({ initialEntries: [initialUrl] }),
      ),
    [initialUrl],
  );

  // Stories should always render at the design-default width, not whatever
  // the live app last persisted via useResizablePane. Clear once on mount.
  useEffect(() => {
    try {
      localStorage.removeItem("wuphf-sidebar-width");
    } catch {}
  }, []);

  // Sidebar groups start expanded so reviewers can see them without
  // hunting for toggles. Reset on unmount so we don't leak state to the
  // app's live store between stories (the store is a singleton).
  useEffect(() => {
    const initial = useAppStore.getState();
    useAppStore.setState({
      sidebarAgentsOpen: true,
      sidebarChannelsOpen: true,
      sidebarIssuesOpen: true,
      sidebarAppsOpen: true,
      sidebarCollapsed: false,
      unreadByChannel,
    });
    return () => {
      useAppStore.setState({
        sidebarAgentsOpen: initial.sidebarAgentsOpen,
        sidebarChannelsOpen: initial.sidebarChannelsOpen,
        sidebarIssuesOpen: initial.sidebarIssuesOpen,
        sidebarAppsOpen: initial.sidebarAppsOpen,
        sidebarCollapsed: initial.sidebarCollapsed,
        unreadByChannel: initial.unreadByChannel,
      });
    };
  }, [unreadByChannel]);

  return (
    <QueryClientProvider client={queryClient}>
      <StoryOutletContext.Provider value={children}>
        <RouterProvider router={router} />
      </StoryOutletContext.Provider>
    </QueryClientProvider>
  );
}
