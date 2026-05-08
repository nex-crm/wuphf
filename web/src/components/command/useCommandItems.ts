import { useMemo } from "react";

import { fetchCatalog } from "../../api/wiki";
import { useChannels } from "../../hooks/useChannels";
import { useOfficeMembers } from "../../hooks/useMembers";
import { router } from "../../lib/router";
import { useAppStore } from "../../stores/app";
import { openProviderSwitcher } from "../ui/ProviderSwitcher";
import { showNotice } from "../ui/Toast";
import type { CommandItem } from "./commandTypes";

// ── Navigation helpers ─────────────────────────────────────────────────

function navigateToChannel(channelSlug: string): void {
  void router.navigate({
    to: "/channels/$channelSlug",
    params: { channelSlug },
  });
}

function navigateToApp(appId: string): void {
  void router.navigate({ to: "/apps/$appId", params: { appId } });
}

function navigateToWikiArticle(path: string): void {
  void router.navigate({ to: "/wiki/$", params: { _splat: path } });
}

function navigateToWiki(): void {
  void router.navigate({ to: "/wiki" });
}

function navigateToTasks(): void {
  void router.navigate({ to: "/tasks" });
}

function prettyWikiPath(path: string): string {
  return path.replace(/^team\//, "").replace(/\.md$/, "");
}

// ── Static action commands ─────────────────────────────────────────────

const STATIC_ACTIONS: Omit<CommandItem, "run">[] = [
  {
    id: "action:search-wiki",
    group: "Actions",
    icon: "📖",
    label: "Search wiki",
    desc: "Find articles across the team knowledge base",
    aliases: ["wiki", "knowledge", "kb", "articles"],
  },
  {
    id: "action:start-task",
    group: "Actions",
    icon: "✅",
    label: "Open task board",
    desc: "View and manage all team tasks",
    aliases: ["tasks", "board", "todo", "kanban"],
  },
  {
    id: "action:open-requests",
    group: "Actions",
    icon: "🔔",
    label: "Open requests",
    desc: "Review pending approval and action requests",
    aliases: ["requests", "approvals", "pending"],
  },
  {
    id: "action:open-settings",
    group: "Actions",
    icon: "⚙️",
    label: "Open settings",
    desc: "Configure providers, themes, and workspace options",
    aliases: ["settings", "config", "preferences"],
  },
  {
    id: "action:open-health",
    group: "Actions",
    icon: "🩺",
    label: "Open provider doctor",
    desc: "Check runtime health and access diagnostics",
    aliases: ["doctor", "health", "diagnostics", "debug", "recover"],
  },
  {
    id: "action:open-skills",
    group: "Actions",
    icon: "⚡",
    label: "Open skills",
    desc: "Browse and manage agent skills",
    aliases: ["skills", "automation"],
  },
  {
    id: "action:open-calendar",
    group: "Actions",
    icon: "📅",
    label: "Open calendar",
    desc: "View scheduled agent tasks and reminders",
    aliases: ["calendar", "schedule"],
  },
  {
    id: "action:open-policies",
    group: "Actions",
    icon: "📜",
    label: "Open policies",
    desc: "Review agent delegation and approval policies",
    aliases: ["policies", "rules", "guardrails"],
  },
  {
    id: "action:switch-provider",
    group: "Actions",
    icon: "🔌",
    label: "Switch provider",
    desc: "Change the active AI provider or model",
    aliases: ["provider", "model", "llm", "openai", "anthropic"],
  },
  {
    id: "action:copy-link",
    group: "Actions",
    icon: "🔗",
    label: "Copy current link",
    desc: "Copy the URL for this page to the clipboard",
    aliases: ["copy", "link", "url", "share"],
  },
];

// ── Filter helper ──────────────────────────────────────────────────────

/**
 * Returns true when the item matches the query string. Matches against
 * label, desc, meta, and any aliases. Case-insensitive, no tokenisation.
 */
export function matchesQuery(
  item: Omit<CommandItem, "run">,
  query: string,
): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  const hay = [
    item.label,
    item.desc ?? "",
    item.meta ?? "",
    ...(item.aliases ?? []),
  ]
    .join(" ")
    .toLowerCase();
  return hay.includes(q);
}

// ── Wiki catalog items ─────────────────────────────────────────────────

interface WikiCatalogEntry {
  path: string;
  title: string;
}

/**
 * Builds a flat list of CommandItems from the wiki catalog. Called
 * lazily in the hook only when query.length >= 2.
 */
function wikiCatalogItems(
  catalog: WikiCatalogEntry[],
  query: string,
  onClose: () => void,
): CommandItem[] {
  return catalog
    .filter((entry) => {
      const q = query.toLowerCase();
      return (
        entry.title.toLowerCase().includes(q) ||
        prettyWikiPath(entry.path).toLowerCase().includes(q)
      );
    })
    .slice(0, 8)
    .map((entry) => ({
      id: `wiki:${entry.path}`,
      group: "Wiki" as const,
      icon: "📖",
      label: entry.title || prettyWikiPath(entry.path),
      desc: prettyWikiPath(entry.path),
      run: () => {
        navigateToWikiArticle(entry.path);
        onClose();
      },
    }));
}

// ── Main hook ──────────────────────────────────────────────────────────

interface UseCommandItemsOptions {
  query: string;
  /** Called after an item's run() fires to close the palette. */
  onClose: () => void;
  /**
   * Pre-fetched wiki catalog. Populated externally so the hook stays
   * testable without a real network.
   */
  wikiCatalog?: WikiCatalogEntry[];
}

export function useCommandItems({
  query,
  onClose,
  wikiCatalog = [],
}: UseCommandItemsOptions): CommandItem[] {
  const { data: channels = [] } = useChannels();
  const { data: members = [] } = useOfficeMembers();
  const setSearchOpen = useAppStore((s) => s.setSearchOpen);
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);

  // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Item builder covers all palette categories in a single pass; each branch is a simple navigation or action call.
  return useMemo(() => {
    const q = query.trim().toLowerCase();
    const list: CommandItem[] = [];

    // ── Static actions ─────────────────────────────────────────────────
    for (const action of STATIC_ACTIONS) {
      if (!matchesQuery(action, q)) continue;
      let run: () => void;
      switch (action.id) {
        case "action:search-wiki":
          run = () => {
            navigateToWiki();
            onClose();
          };
          break;
        case "action:start-task":
          run = () => {
            navigateToTasks();
            onClose();
          };
          break;
        case "action:open-requests":
          run = () => {
            navigateToApp("requests");
            onClose();
          };
          break;
        case "action:open-settings":
          run = () => {
            navigateToApp("settings");
            onClose();
          };
          break;
        case "action:open-health":
          run = () => {
            navigateToApp("health-check");
            onClose();
          };
          break;
        case "action:open-skills":
          run = () => {
            navigateToApp("skills");
            onClose();
          };
          break;
        case "action:open-calendar":
          run = () => {
            navigateToApp("calendar");
            onClose();
          };
          break;
        case "action:open-policies":
          run = () => {
            navigateToApp("policies");
            onClose();
          };
          break;
        case "action:switch-provider":
          run = () => {
            openProviderSwitcher();
            onClose();
          };
          break;
        case "action:copy-link":
          run = () => {
            const url = window.location.href;
            navigator.clipboard
              .writeText(url)
              .then(() => showNotice("Link copied to clipboard", "success"))
              .catch(() =>
                showNotice("Failed to copy link to clipboard", "error"),
              );
            onClose();
          };
          break;
        default:
          run = onClose;
      }
      list.push({ ...action, run });
    }

    // ── Agents ─────────────────────────────────────────────────────────
    for (const m of members) {
      if (
        !m.slug ||
        m.slug === "human" ||
        m.slug === "you" ||
        m.slug === "system"
      )
        continue;
      const hay = `${m.slug} ${m.name ?? ""} ${m.role ?? ""}`.toLowerCase();
      if (q && !hay.includes(q.replace(/^@/, ""))) continue;
      list.push({
        id: `ag:${m.slug}`,
        group: "Agents",
        icon: m.emoji || "🤖",
        label: m.name || m.slug,
        desc: m.role,
        meta: `@${m.slug}`,
        run: () => {
          setActiveAgentSlug(m.slug);
          onClose();
        },
      });
    }

    // ── Channels ───────────────────────────────────────────────────────
    for (const ch of channels) {
      const hay =
        `${ch.slug} ${ch.name ?? ""} ${ch.description ?? ""}`.toLowerCase();
      if (q && !hay.includes(q.replace(/^#/, ""))) continue;
      list.push({
        id: `ch:${ch.slug}`,
        group: "Channels",
        icon: "#",
        label: ch.name || ch.slug,
        desc: ch.description,
        meta: `#${ch.slug}`,
        run: () => {
          navigateToChannel(ch.slug);
          onClose();
        },
      });
    }

    // ── Wiki (catalog — rendered when query has 2+ chars) ──────────────
    if (q.length >= 2) {
      const wikiItems = wikiCatalogItems(wikiCatalog, q, onClose);
      list.push(...wikiItems);
    }

    // ── Open search modal for deeper search ────────────────────────────
    // When query is >= 2 chars, surface a "Search everywhere" escape hatch
    // that re-opens the full SearchModal with the query pre-filled.
    if (q.length >= 2) {
      list.push({
        id: "action:search-everywhere",
        group: "Actions",
        icon: "🔎",
        label: `Search "${query.trim()}" everywhere`,
        desc: "Search channels, messages, notebooks, and wiki",
        run: () => {
          setSearchOpen(true);
          onClose();
        },
      });
    }

    return list;
  }, [
    query,
    channels,
    members,
    wikiCatalog,
    onClose,
    setActiveAgentSlug,
    setSearchOpen,
  ]);
}

// Exported for direct use in async context (e.g. fetching catalog before render).
export { fetchCatalog };
