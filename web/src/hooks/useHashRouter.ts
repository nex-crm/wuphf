import { useEffect, useRef } from "react";

import {
  type ChannelMeta,
  directChannelSlug,
  isDMChannel,
  useAppStore,
} from "../stores/app";

type Route =
  | { view: "channel"; channel: string }
  | { view: "dm"; agent: string }
  | { view: "app"; app: string }
  | {
      view: "workbench";
      agentSlug: string | null;
      taskId: string | null;
    }
  | { view: "wiki"; articlePath: string | null }
  | { view: "wiki-lookup"; query: string }
  | { view: "notebooks"; agentSlug: string | null; entrySlug: string | null }
  | { view: "reviews" };

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
function parseHash(hash: string): Route {
  const cleaned = hash.replace(/^#\/?/, "");
  const parts = cleaned.split("/").filter(Boolean);
  if (parts[0] === "channels" && parts[1]) {
    return { view: "channel", channel: decodeURIComponent(parts[1]) };
  }
  if (parts[0] === "dm" && parts[1]) {
    return { view: "dm", agent: decodeURIComponent(parts[1]) };
  }
  if (parts[0] === "apps" && parts[1]) {
    const app = decodeURIComponent(parts[1]);
    if (app === "workbench") {
      const agentSlug = parts[2] ? decodeURIComponent(parts[2]) : null;
      const taskId =
        parts[3] === "tasks" && parts[4] ? decodeURIComponent(parts[4]) : null;
      return { view: "workbench", agentSlug, taskId };
    }
    return { view: "app", app: decodeURIComponent(parts[1]) };
  }
  if (parts[0] === "console") {
    return { view: "app", app: "console" };
  }
  if (parts[0] === "threads") {
    return { view: "app", app: "threads" };
  }
  if (parts[0] === "wiki" && parts[1] === "lookup") {
    const params = new URLSearchParams(
      window.location.search.slice(1) || cleaned.split("?")[1] || "",
    );
    const q = params.get("q") || "";
    return { view: "wiki-lookup", query: decodeURIComponent(q) };
  }
  if (parts[0] === "wiki") {
    const rest = parts.slice(1).map(decodeURIComponent).join("/");
    return { view: "wiki", articlePath: rest || null };
  }
  if (parts[0] === "notebooks") {
    const agent = parts[1] ? decodeURIComponent(parts[1]) : null;
    const entry = parts[2] ? decodeURIComponent(parts[2]) : null;
    return { view: "notebooks", agentSlug: agent, entrySlug: entry };
  }
  if (parts[0] === "reviews") {
    return { view: "reviews" };
  }
  return { view: "channel", channel: "general" };
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
function stateToHash(state: {
  currentApp: string | null;
  currentChannel: string;
  channelMeta: Record<string, ChannelMeta>;
  wikiPath: string | null;
  wikiLookupQuery: string | null;
  notebookAgentSlug: string | null;
  notebookEntrySlug: string | null;
  workbenchAgentSlug: string | null;
  workbenchTaskId: string | null;
}): string {
  if (state.currentApp === "wiki-lookup") {
    return state.wikiLookupQuery
      ? `#/wiki/lookup?q=${encodeURIComponent(state.wikiLookupQuery)}`
      : "#/wiki/lookup";
  }
  if (state.currentApp === "wiki") {
    return state.wikiPath
      ? `#/wiki/${state.wikiPath.split("/").map(encodeURIComponent).join("/")}`
      : "#/wiki";
  }
  if (state.currentApp === "notebooks") {
    const parts: string[] = ["notebooks"];
    if (state.notebookAgentSlug)
      parts.push(encodeURIComponent(state.notebookAgentSlug));
    if (state.notebookAgentSlug && state.notebookEntrySlug) {
      parts.push(encodeURIComponent(state.notebookEntrySlug));
    }
    return `#/${parts.join("/")}`;
  }
  if (state.currentApp === "reviews") {
    return "#/reviews";
  }
  if (state.currentApp === "workbench") {
    const parts = ["apps", "workbench"];
    if (state.workbenchAgentSlug) {
      parts.push(encodeURIComponent(state.workbenchAgentSlug));
      if (state.workbenchTaskId) {
        parts.push("tasks", encodeURIComponent(state.workbenchTaskId));
      }
    }
    return `#/${parts.join("/")}`;
  }
  if (state.currentApp) {
    return `#/apps/${encodeURIComponent(state.currentApp)}`;
  }
  const dm = isDMChannel(state.currentChannel, state.channelMeta);
  if (dm) {
    return `#/dm/${encodeURIComponent(dm.agentSlug)}`;
  }
  return `#/channels/${encodeURIComponent(state.currentChannel || "general")}`;
}

/**
 * Two-way sync between the Zustand app store and the location hash.
 *
 *   #/channels/<slug>            ↔ currentChannel=<slug>, currentApp=null
 *   #/dm/<agent>                 ↔ currentChannel=<agent>__human, channelMeta marked type 'D'
 *   #/apps/<id>                  ↔ currentApp=<id>
 *   #/console                    ↔ currentApp='console'
 *   #/wiki[/<path>]              ↔ currentApp='wiki', wikiPath=<path>
 *   #/notebooks[/<agent>[/<e>]]  ↔ currentApp='notebooks', notebookAgentSlug, notebookEntrySlug
 *   #/reviews                    ↔ currentApp='reviews'
 *
 * Lets the user bookmark any screen and share URLs. Silent fallback to
 * the channel view if the hash is malformed.
 */
export function useHashRouter() {
  const currentApp = useAppStore((s) => s.currentApp);
  const currentChannel = useAppStore((s) => s.currentChannel);
  const channelMeta = useAppStore((s) => s.channelMeta);
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel);
  const enterDM = useAppStore((s) => s.enterDM);
  const setLastMessageId = useAppStore((s) => s.setLastMessageId);
  const wikiPath = useAppStore((s) => s.wikiPath);
  const setWikiPath = useAppStore((s) => s.setWikiPath);
  const wikiLookupQuery = useAppStore((s) => s.wikiLookupQuery);
  const setWikiLookupQuery = useAppStore((s) => s.setWikiLookupQuery);
  const notebookAgentSlug = useAppStore((s) => s.notebookAgentSlug);
  const notebookEntrySlug = useAppStore((s) => s.notebookEntrySlug);
  const setNotebookRoute = useAppStore((s) => s.setNotebookRoute);
  const workbenchAgentSlug = useAppStore((s) => s.workbenchAgentSlug);
  const workbenchTaskId = useAppStore((s) => s.workbenchTaskId);
  const setWorkbenchRoute = useAppStore((s) => s.setWorkbenchRoute);

  // Avoid ping-ponging: skip the next hashchange or store-sync when we
  // were the one that caused it.
  const ignoreNextHashChange = useRef(false);
  const ignoreNextStoreSync = useRef(false);

  // Apply current hash on mount and when it changes
  useEffect(() => {
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
    function applyHash() {
      if (ignoreNextHashChange.current) {
        ignoreNextHashChange.current = false;
        return;
      }
      const route = parseHash(window.location.hash);
      ignoreNextStoreSync.current = true;
      if (route.view === "dm") {
        enterDM(route.agent, directChannelSlug(route.agent));
      } else if (route.view === "app") {
        setCurrentApp(route.app);
      } else if (route.view === "workbench") {
        setWorkbenchRoute(route.agentSlug, route.taskId);
      } else if (route.view === "wiki-lookup") {
        setWikiLookupQuery(route.query);
        setCurrentApp("wiki-lookup");
      } else if (route.view === "wiki") {
        setWikiPath(route.articlePath);
        setCurrentApp("wiki");
      } else if (route.view === "notebooks") {
        setNotebookRoute(route.agentSlug, route.entrySlug);
        setCurrentApp("notebooks");
      } else if (route.view === "reviews") {
        setCurrentApp("reviews");
      } else {
        setCurrentApp(null);
        setCurrentChannel(route.channel);
        setLastMessageId(null);
      }
    }

    applyHash();
    window.addEventListener("hashchange", applyHash);
    return () => window.removeEventListener("hashchange", applyHash);
  }, [
    enterDM,
    setCurrentApp,
    setCurrentChannel,
    setLastMessageId,
    setWikiPath,
    setWikiLookupQuery,
    setNotebookRoute,
    setWorkbenchRoute,
  ]);

  // Push store changes back into the hash
  useEffect(() => {
    if (ignoreNextStoreSync.current) {
      ignoreNextStoreSync.current = false;
      return;
    }
    const next = stateToHash({
      currentApp,
      currentChannel,
      channelMeta,
      wikiPath,
      wikiLookupQuery,
      notebookAgentSlug,
      notebookEntrySlug,
      workbenchAgentSlug,
      workbenchTaskId,
    });
    if (next !== window.location.hash) {
      // replaceState does not emit `hashchange`, so do not arm
      // ignoreNextHashChange here. Leaving it set causes the next real hash
      // navigation to be dropped.
      window.history.replaceState(null, "", next);
    }
  }, [
    currentApp,
    currentChannel,
    channelMeta,
    wikiPath,
    wikiLookupQuery,
    notebookAgentSlug,
    notebookEntrySlug,
    workbenchAgentSlug,
    workbenchTaskId,
  ]);
}
