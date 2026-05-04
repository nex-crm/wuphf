// biome-ignore-all lint/a11y/useKeyWithClickEvents: Pointer handler is paired with an existing modal, image, or routed-control keyboard path; preserving current interaction model.
// biome-ignore-all lint/a11y/noStaticElementInteractions: Intentional wrapper/backdrop or SVG hover target; interactive child controls and keyboard paths are handled nearby.
import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { getMessages, type Message, post } from "../../api/client";
import { type NotebookSearchHit, searchNotebook } from "../../api/notebook";
import { searchWiki, type WikiSearchHit } from "../../api/wiki";
import { useChannels } from "../../hooks/useChannels";
import { useOfficeMembers } from "../../hooks/useMembers";
import { router } from "../../lib/router";
import { useAppStore } from "../../stores/app";
import { SLASH_COMMANDS } from "../messages/Autocomplete";
import { Kbd } from "../ui/Kbd";
import { openProviderSwitcher } from "../ui/ProviderSwitcher";
import { showNotice } from "../ui/Toast";

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

function navigateToNotebookEntry(agentSlug: string, entrySlug: string): void {
  void router.navigate({
    to: "/notebooks/$agentSlug/$entrySlug",
    params: { agentSlug, entrySlug },
  });
}

function navigateToNotebookCatalog(): void {
  void router.navigate({ to: "/notebooks" });
}

interface PaletteItem {
  id: string;
  group: "Channels" | "Agents" | "Commands" | "Messages" | "Wiki" | "Notebooks";
  icon: string;
  label: string;
  desc?: string;
  meta?: string;
  run: () => void;
}

interface MessageHit extends Message {
  matchedChannel: string;
}

function formatTime(ts: string): string {
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  } catch {
    return ts;
  }
}

function highlightMatch(text: string, query: string): ReactNode {
  if (!query) return text;
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const regex = new RegExp(`(${escaped})`, "gi");
  const parts = text.split(regex);
  let offset = 0;
  return parts.map((part) => {
    const key = `${part}-${offset}`;
    offset += part.length;
    const isMatch =
      regex.test(part) && part.toLowerCase() === query.toLowerCase();
    regex.lastIndex = 0;
    return isMatch ? <mark key={key}>{part}</mark> : part;
  });
}

function prettyWikiPath(path: string): string {
  return path.replace(/^team\//, "").replace(/\.md$/, "");
}

function parseNotebookPath(
  path: string,
): { agent: string; entry: string } | null {
  // `agents/<slug>/<entry>.md` — split and validate the shape without regex
  // capture groups that trip up some static analyzers.
  if (!(path.startsWith("agents/") && path.endsWith(".md"))) return null;
  const stripped = path.slice("agents/".length, -3);
  const firstSlash = stripped.indexOf("/");
  if (firstSlash <= 0 || firstSlash === stripped.length - 1) return null;
  const agent = stripped.slice(0, firstSlash);
  const entry = stripped.slice(firstSlash + 1);
  if (entry.includes("/")) return null;
  return { agent, entry };
}

// biome-ignore lint/complexity/noExcessiveLinesPerFunction: Existing function length is baselined for a focused follow-up refactor.
export function SearchModal() {
  const searchOpen = useAppStore((s) => s.searchOpen);
  const setSearchOpen = useAppStore((s) => s.setSearchOpen);
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);
  const composerSearchInitialQuery = useAppStore(
    (s) => s.composerSearchInitialQuery,
  );
  const setComposerSearchInitialQuery = useAppStore(
    (s) => s.setComposerSearchInitialQuery,
  );
  const { data: channels = [] } = useChannels();
  const { data: members = [] } = useOfficeMembers();

  const [query, setQuery] = useState("");
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [messageHits, setMessageHits] = useState<MessageHit[]>([]);
  const [wikiHits, setWikiHits] = useState<WikiSearchHit[]>([]);
  const [notebookHits, setNotebookHits] = useState<NotebookSearchHit[]>([]);
  const [searching, setSearching] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const close = useCallback(() => setSearchOpen(false), [setSearchOpen]);

  const runSearch = useCallback(
    async (raw: string) => {
      const trimmed = raw.trim();
      const needle = trimmed.toLowerCase();
      if (needle.length < 2 || channels.length === 0) {
        setMessageHits([]);
        setWikiHits([]);
        setNotebookHits([]);
        return;
      }
      setSearching(true);
      try {
        const messagesP = Promise.all(
          channels.map(async (ch) => {
            try {
              const { messages } = await getMessages(ch.slug, null, 100);
              return messages
                .filter((m) => m.content?.toLowerCase().includes(needle))
                .map((m): MessageHit => ({ ...m, matchedChannel: ch.slug }));
            } catch {
              return [] as MessageHit[];
            }
          }),
        ).then((messageGroups) =>
          messageGroups
            .flat()
            .sort(
              (a, b) =>
                new Date(b.timestamp).getTime() -
                new Date(a.timestamp).getTime(),
            )
            .slice(0, 8),
        );

        const wikiP = searchWiki(trimmed).then((hits) => hits.slice(0, 8));

        const agentSlugs = members
          .map((m) => m.slug)
          .filter(
            (s): s is string =>
              typeof s === "string" &&
              s !== "human" &&
              s !== "you" &&
              s !== "system",
          );
        const notebookP = Promise.all(
          agentSlugs.map((slug) =>
            searchNotebook(slug, trimmed).catch(
              () => [] as NotebookSearchHit[],
            ),
          ),
        ).then((notebookGroups) => notebookGroups.flat().slice(0, 8));

        const [msg, wiki, nb] = await Promise.all([
          messagesP,
          wikiP,
          notebookP,
        ]);
        setMessageHits(msg);
        setWikiHits(wiki);
        setNotebookHits(nb);
      } finally {
        setSearching(false);
      }
    },
    [channels, members],
  );

  const handleQueryChange = useCallback(
    (value: string) => {
      setQuery(value);
      setSelectedIdx(0);
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => runSearch(value), 250);
    },
    [runSearch],
  );

  useEffect(() => {
    if (searchOpen) {
      const t = setTimeout(() => inputRef.current?.focus(), 50);
      if (composerSearchInitialQuery) {
        handleQueryChange(composerSearchInitialQuery);
        setComposerSearchInitialQuery("");
      }
      return () => clearTimeout(t);
    }
    setQuery("");
    setMessageHits([]);
    setWikiHits([]);
    setNotebookHits([]);
    setSelectedIdx(0);
  }, [
    searchOpen,
    composerSearchInitialQuery,
    handleQueryChange,
    setComposerSearchInitialQuery,
  ]);

  // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
  const items = useMemo<PaletteItem[]>(() => {
    const q = query.trim().toLowerCase();
    const list: PaletteItem[] = [];

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
          close();
        },
      });
    }

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
          close();
        },
      });
    }

    for (const c of SLASH_COMMANDS) {
      const hay = `${c.name} ${c.desc}`.toLowerCase();
      if (q && !hay.includes(q.replace(/^\//, ""))) continue;
      list.push({
        id: `cmd:${c.name}`,
        group: "Commands",
        icon: c.icon,
        label: c.name,
        desc: c.desc,
        run: () => {
          dispatchPaletteCommand(c.name, { setSearchOpen });
          close();
        },
      });
    }

    if (q.length >= 2) {
      for (const hit of messageHits) {
        const snippet =
          hit.content.length > 100
            ? `${hit.content.slice(0, 100)}...`
            : hit.content;
        list.push({
          id: `msg:${hit.id}:${hit.matchedChannel}`,
          group: "Messages",
          icon: "💬",
          label: `${hit.from}: ${snippet}`,
          desc: `#${hit.matchedChannel} · ${formatTime(hit.timestamp)}`,
          run: () => {
            navigateToChannel(hit.matchedChannel);
            close();
          },
        });
      }

      for (const hit of wikiHits) {
        list.push({
          id: `wiki:${hit.path}:${hit.line}`,
          group: "Wiki",
          icon: "📖",
          label: prettyWikiPath(hit.path),
          desc: hit.snippet.trim().slice(0, 120),
          meta: `L${hit.line}`,
          run: () => {
            navigateToWikiArticle(hit.path);
            close();
          },
        });
      }

      for (const hit of notebookHits) {
        const parsed = parseNotebookPath(hit.path);
        const label = parsed ? `${parsed.agent} · ${parsed.entry}` : hit.path;
        list.push({
          id: `nb:${hit.path}:${hit.line}`,
          group: "Notebooks",
          icon: "📓",
          label,
          desc: hit.snippet.trim().slice(0, 120),
          meta: `L${hit.line}`,
          run: () => {
            if (parsed) {
              navigateToNotebookEntry(parsed.agent, parsed.entry);
            } else {
              navigateToNotebookCatalog();
            }
            close();
          },
        });
      }
    }

    return list;
  }, [
    query,
    channels,
    members,
    messageHits,
    wikiHits,
    notebookHits,
    setActiveAgentSlug,
    setSearchOpen,
    close,
  ]);

  useEffect(() => {
    setSelectedIdx((idx) => Math.min(idx, Math.max(items.length - 1, 0)));
  }, [items.length]);

  useEffect(() => {
    if (!searchOpen) return;
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        close();
        return;
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIdx((i) =>
          items.length === 0 ? 0 : (i + 1) % items.length,
        );
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIdx((i) =>
          items.length === 0 ? 0 : (i - 1 + items.length) % items.length,
        );
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        const item = items[selectedIdx];
        if (item) item.run();
      }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [searchOpen, items, selectedIdx, close]);

  const grouped = useMemo(() => {
    const out: {
      group: PaletteItem["group"];
      items: { item: PaletteItem; flatIdx: number }[];
    }[] = [];
    items.forEach((item, idx) => {
      const last = out[out.length - 1];
      if (last && last.group === item.group) {
        last.items.push({ item, flatIdx: idx });
      } else {
        out.push({ group: item.group, items: [{ item, flatIdx: idx }] });
      }
    });
    return out;
  }, [items]);

  function handleOverlayClick(e: React.MouseEvent) {
    if (e.target === e.currentTarget) close();
  }

  if (!searchOpen) return null;

  return (
    <div className="search-overlay" onClick={handleOverlayClick}>
      <div className="search-modal card cmd-palette">
        <div className="search-input-wrap">
          <svg
            aria-hidden="true"
            focusable="false"
            className="search-input-icon"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            ref={inputRef}
            className="search-input"
            type="text"
            placeholder="Search channels, agents, commands, messages, wiki, notebooks..."
            value={query}
            onChange={(e) => handleQueryChange(e.target.value)}
          />
          {searching ? <span className="search-spinner" /> : null}
        </div>

        <div className="cmd-palette-results">
          {items.length === 0 ? (
            <div className="cmd-palette-empty">
              {query
                ? `No results for "${query}"`
                : "Start typing to search..."}
            </div>
          ) : (
            grouped.map((g) => (
              <div key={g.group} className="cmd-palette-group">
                <div className="cmd-palette-group-title">{g.group}</div>
                {/* biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor. */}
                {g.items.map(({ item, flatIdx }) => (
                  <button
                    key={item.id}
                    type="button"
                    className={`cmd-palette-item${flatIdx === selectedIdx ? " selected" : ""}`}
                    onMouseEnter={() => setSelectedIdx(flatIdx)}
                    onClick={item.run}
                  >
                    <span className="cmd-palette-item-icon">{item.icon}</span>
                    <span className="cmd-palette-item-text">
                      <span className="cmd-palette-item-label">
                        {item.group === "Messages" ||
                        item.group === "Wiki" ||
                        item.group === "Notebooks"
                          ? highlightMatch(item.label, query.trim())
                          : item.label}
                      </span>
                      {item.desc ? (
                        <span className="cmd-palette-item-desc">
                          {item.group === "Wiki" || item.group === "Notebooks"
                            ? highlightMatch(item.desc, query.trim())
                            : item.desc}
                        </span>
                      ) : null}
                    </span>
                    {item.meta ? (
                      <span className="cmd-palette-item-meta">{item.meta}</span>
                    ) : null}
                  </button>
                ))}
              </div>
            ))
          )}
        </div>

        <div className="cmd-palette-footer">
          <span>
            <Kbd size="sm">↑</Kbd>
            <Kbd size="sm">↓</Kbd> navigate
          </span>
          <span>
            <Kbd size="sm">↵</Kbd> open
          </span>
          <span>
            <Kbd size="sm">esc</Kbd> close
          </span>
        </div>
      </div>
    </div>
  );
}

interface CommandDeps {
  setSearchOpen: (open: boolean) => void;
}

function dispatchPaletteCommand(name: string, deps: CommandDeps) {
  switch (name) {
    case "/clear":
      showNotice("Messages cleared", "info");
      return;
    case "/help":
      useAppStore.getState().setComposerHelpOpen(true);
      return;
    case "/ask":
    case "/remember":
      showNotice(
        `${name} requires arguments — type it in the composer.`,
        "info",
      );
      return;
    case "/search":
      deps.setSearchOpen(true);
      return;
    case "/requests":
      navigateToApp("requests");
      return;
    case "/policies":
      navigateToApp("policies");
      return;
    case "/skills":
      navigateToApp("skills");
      return;
    case "/calendar":
      navigateToApp("calendar");
      return;
    case "/tasks":
      navigateToApp("tasks");
      return;
    case "/recover":
    case "/doctor":
      navigateToApp("health-check");
      return;
    case "/threads":
      navigateToApp("threads");
      return;
    case "/provider":
      openProviderSwitcher();
      return;
    case "/focus":
      post("/focus-mode", { focus_mode: true })
        .then(() => showNotice("Switched to delegation mode", "success"))
        .catch((e: Error) =>
          showNotice(`Failed to switch mode: ${e.message}`, "error"),
        );
      return;
    case "/collab":
      post("/focus-mode", { focus_mode: false })
        .then(() => showNotice("Switched to collaborative mode", "success"))
        .catch((e: Error) =>
          showNotice(`Failed to switch mode: ${e.message}`, "error"),
        );
      return;
    case "/pause":
      post("/signals", { kind: "pause", summary: "Human paused all agents" })
        .then(() => showNotice("All agents paused", "success"))
        .catch((e: Error) => showNotice(`Pause failed: ${e.message}`, "error"));
      return;
    case "/resume":
      post("/signals", { kind: "resume", summary: "Human resumed agents" })
        .then(() => showNotice("Agents resumed", "success"))
        .catch((e: Error) =>
          showNotice(`Resume failed: ${e.message}`, "error"),
        );
      return;
    case "/reset":
      post("/reset", {})
        .then(() => {
          navigateToChannel("general");
          showNotice("Office reset", "success");
        })
        .catch((e: Error) => showNotice(`Reset failed: ${e.message}`, "error"));
      return;
    default:
      showNotice(
        `${name} requires arguments — type it in the composer.`,
        "info",
      );
      return;
  }
}
