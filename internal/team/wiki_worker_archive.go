package team

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// EnqueueArchiveSweep runs WikiArchiver.Sweep on the worker's serialized write
// queue. The caller controls the timeout with ctx because a sweep can perform
// many archive commits and legitimately outlive wikiWriteTimeout.
func (w *WikiWorker) EnqueueArchiveSweep(ctx context.Context, readLog *ReadLog, minAge time.Duration) (SweepResult, error) {
	if !w.running.Load() {
		return SweepResult{}, ErrWorkerStopped
	}
	req := wikiWriteRequest{
		Context:        ctx,
		IsArchiveSweep: true,
		ArchiveReadLog: readLog,
		ArchiveMinAge:  minAge,
		ReplyCh:        make(chan wikiWriteResult, 1),
	}
	select {
	case w.requests <- req:
	default:
		return SweepResult{}, ErrQueueSaturated
	}
	select {
	case result := <-req.ReplyCh:
		return result.SweepResult, result.Err
	case <-ctx.Done():
		select {
		case result := <-req.ReplyCh:
			return result.SweepResult, result.Err
		default:
		}
		return SweepResult{}, fmt.Errorf("wiki: archive sweep canceled before completion: %w", ctx.Err())
	}
}

// NotifyArchived fires the same post-commit hooks that process() fires for
// standard wiki writes, but for paths committed by WikiArchiver.Sweep().
// Kept as a public bridge for tests and repair paths; normal sweeps should use
// EnqueueArchiveSweep so CommitArchive also runs on the single-writer queue.
func (w *WikiWorker) NotifyArchived(ctx context.Context, paths []string) {
	w.notifyArchived(ctx, paths)
}

func (w *WikiWorker) notifyArchived(ctx context.Context, paths []string) {
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

func archiveSweepHasPartialResult(result SweepResult) bool {
	return result.Archived > 0 || result.Skipped > 0 || result.Errors > 0 || len(result.ArchivedPaths) > 0
}

func isHardArchiveSweepError(err error) bool {
	return err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
