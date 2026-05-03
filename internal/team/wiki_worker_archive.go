package team

import (
	"context"
	"time"
)

// NotifyArchived fires the same post-commit hooks that process() fires for
// standard wiki writes, but for paths committed by WikiArchiver.Sweep().
// Sweep calls CommitArchive directly (bypassing process()) to keep the
// single-writer invariant; this method bridges the gap so SSE subscribers,
// the section cache, the search index, and the backup mirror stay in sync
// after a sweep.
func (w *WikiWorker) NotifyArchived(ctx context.Context, paths []string) {
	if len(paths) == 0 {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	for _, p := range paths {
		w.publisher.PublishWikiEvent(wikiWriteEvent{
			Path:       p,
			AuthorSlug: "archivist",
			Timestamp:  ts,
		})
		w.maybeReconcileIndex(ctx, p)
	}
	if notifier, ok := w.publisher.(wikiSectionsNotifier); ok {
		notifier.EnqueueSectionsRefresh()
	}
	w.maybeScheduleBackup(ctx)
}
