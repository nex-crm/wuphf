import { useEffect, useMemo, useState } from "react";

import {
  fetchRichArtifact,
  type RichArtifactDetail,
} from "../api/richArtifacts";
import { extractRichArtifactIds } from "../lib/richArtifactReferences";

/**
 * useInlineArtifacts resolves every `visual-artifact:<id>` marker inside a
 * piece of markdown content into the underlying RichArtifactDetail records,
 * preserving document order. Failures (404 / network) are dropped silently —
 * the caller is expected to strip the standalone marker lines so a missing
 * artifact degrades to "nothing visible" rather than literal text.
 *
 * Originally lifted out of WikiArticle, reused on the notebook entry surface
 * and the Issue document surface. Same regex + same fetcher across all
 * three so the "Making Software"-style inline embed renders identically
 * wherever the agent dropped a marker.
 *
 * Dependency stability: extractRichArtifactIds returns a fresh array on
 * every render, so the effect keys off a joined-string of ids instead of
 * the array reference. Rich-artifact ids cannot contain commas, so the
 * join round-trips losslessly.
 */
export function useInlineArtifacts(
  content: string | null,
): RichArtifactDetail[] {
  const idsKey = useMemo(
    () => (content ? extractRichArtifactIds(content).join(",") : ""),
    [content],
  );
  const [details, setDetails] = useState<RichArtifactDetail[]>([]);
  useEffect(() => {
    let cancelled = false;
    // Clear the previous container's artifacts up-front so a navigation
    // does not flash the old inline embeds while the new batch is still
    // in flight.
    setDetails([]);
    const ids = idsKey ? idsKey.split(",") : [];
    if (ids.length === 0) {
      return () => {
        cancelled = true;
      };
    }
    void Promise.all(
      ids.map((id) =>
        fetchRichArtifact(id).catch((): RichArtifactDetail | null => null),
      ),
    ).then((results) => {
      if (cancelled) return;
      setDetails(results.filter((d): d is RichArtifactDetail => d !== null));
    });
    return () => {
      cancelled = true;
    };
  }, [idsKey]);
  return details;
}
