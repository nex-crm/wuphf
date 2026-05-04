import { useEffect, useRef } from "react";

import {
  legacyRouteToStatePatch,
  legacyStateToHash,
  parseLegacyHash,
} from "../routes/legacyHash";
import { useAppStore } from "../stores/app";

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
      const route = parseLegacyHash(
        window.location.hash,
        window.location.search,
      );
      const patch = legacyRouteToStatePatch(route);
      ignoreNextStoreSync.current = true;
      if (patch.kind === "dm") {
        enterDM(patch.agentSlug, patch.channelSlug);
      } else if (patch.kind === "app") {
        setCurrentApp(patch.app);
      } else if (patch.kind === "wiki-lookup") {
        setWikiLookupQuery(patch.query);
        setCurrentApp("wiki-lookup");
      } else if (patch.kind === "wiki") {
        setWikiPath(patch.articlePath);
        setCurrentApp("wiki");
      } else if (patch.kind === "notebooks") {
        setNotebookRoute(patch.agentSlug, patch.entrySlug);
        setCurrentApp("notebooks");
      } else if (patch.kind === "reviews") {
        setCurrentApp("reviews");
      } else {
        setCurrentApp(null);
        setCurrentChannel(patch.channel);
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
  ]);

  // Push store changes back into the hash
  useEffect(() => {
    if (ignoreNextStoreSync.current) {
      ignoreNextStoreSync.current = false;
      return;
    }
    const next = legacyStateToHash({
      currentApp,
      currentChannel,
      channelMeta,
      wikiPath,
      wikiLookupQuery,
      notebookAgentSlug,
      notebookEntrySlug,
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
  ]);
}
