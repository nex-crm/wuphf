import { useCallback, useState } from "react";

import {
  type NotebookEntry,
  promoteEntry,
  type ReviewComment,
  type ReviewState,
} from "../../api/notebook";
import ByLineStrip from "./ByLineStrip";
import DraftStamp from "./DraftStamp";
import EntryBody from "./EntryBody";
import InlineReviewThread from "./InlineReviewThread";
import PosterityLine from "./PosterityLine";
import PromoteButton from "./PromoteButton";
import PromotedBackCallout from "./PromotedBackCallout";

/**
 * Full notebook-entry article view. Composes the ruled-paper surface with
 * DRAFT stamp, sticky byline, markdown body, promoted-back callout,
 * inline review thread, actions footer, and posterity line.
 *
 * Lane B/C will hand back a richer entry payload; for now the component
 * renders whatever the API adapter returns (real or mocked) without
 * reshaping.
 */

interface NotebookEntryProps {
  entry: NotebookEntry;
  /** Comments threaded against the active review, if any. */
  reviewComments?: ReviewComment[];
  reviewState?: ReviewState | null;
  onNavigateCatalog?: () => void;
  onNavigateAgent?: (agentSlug: string) => void;
  onNavigateWiki?: (wikiPath: string) => void;
}

function statusToReviewState(
  status: NotebookEntry["status"],
): ReviewState | null {
  switch (status) {
    case "in-review":
      return "in-review";
    case "changes-requested":
      return "changes-requested";
    case "promoted":
      return "approved";
    default:
      return null;
  }
}

export default function NotebookEntryView({
  entry,
  reviewComments = [],
  reviewState,
  onNavigateCatalog,
  onNavigateAgent,
  onNavigateWiki,
}: NotebookEntryProps) {
  const [pending, setPending] = useState(
    entry.status === "in-review" || entry.status === "changes-requested",
  );
  const [promoteError, setPromoteError] = useState<string | null>(null);

  const handlePromote = useCallback(async () => {
    setPromoteError(null);
    setPending(true);
    try {
      await promoteEntry(entry.agent_slug, entry.entry_slug, {
        proposed_wiki_path: `drafts/${entry.agent_slug}-${entry.entry_slug}`,
        reviewer_slug: entry.reviewer_slug,
      });
    } catch (err: unknown) {
      setPending(false);
      setPromoteError(err instanceof Error ? err.message : "Promote failed");
    }
  }, [entry.agent_slug, entry.entry_slug, entry.reviewer_slug]);

  const effectiveReviewState = reviewState ?? statusToReviewState(entry.status);

  // DRAFT stamp only appears when the entry has never been reviewed/promoted.
  const showDraftStamp = entry.status === "draft";

  return (
    <main
      className="nb-article"
      aria-label={`Draft: ${entry.title}. Not yet reviewed.`}
      id="nb-entry-main"
    >
      <nav className="nb-crumb" aria-label="Breadcrumb">
        <a
          href="#/apps/notebooks"
          onClick={(e) => {
            if (onNavigateCatalog) {
              e.preventDefault();
              onNavigateCatalog();
            }
          }}
        >
          Notebooks
        </a>{" "}
        /{" "}
        <a
          href={`#/notebooks/${encodeURIComponent(entry.agent_slug)}`}
          onClick={(e) => {
            if (onNavigateAgent) {
              e.preventDefault();
              onNavigateAgent(entry.agent_slug);
            }
          }}
        >
          {entry.agent_slug}
        </a>{" "}
        / <strong>{entry.entry_slug}</strong>
      </nav>

      {showDraftStamp && <DraftStamp />}

      <h1 className="nb-entry-title">{entry.title}</h1>
      {entry.subtitle && (
        <div className="nb-entry-subtitle">{entry.subtitle}</div>
      )}

      <ByLineStrip
        authorSlug={entry.agent_slug}
        status={entry.status}
        lastEditedTs={entry.last_edited_ts}
        revisions={entry.revisions}
        reviewerSlug={entry.reviewer_slug}
      />

      <EntryBody markdown={entry.body_md} onWikiNavigate={onNavigateWiki} />

      {entry.promoted_back && (
        <PromotedBackCallout
          link={entry.promoted_back}
          onNavigate={onNavigateWiki}
        />
      )}

      <InlineReviewThread
        reviewerSlug={entry.reviewer_slug}
        state={effectiveReviewState}
        comments={reviewComments}
      />

      <PromoteButton
        reviewerSlug={entry.reviewer_slug}
        pending={pending}
        onPromote={handlePromote}
      />
      {promoteError && (
        <p className="nb-error" role="alert">
          Could not submit: {promoteError}
        </p>
      )}

      <PosterityLine
        authorSlug={entry.agent_slug}
        reviewerSlug={entry.reviewer_slug}
        filePath={entry.file_path}
      />
    </main>
  );
}
