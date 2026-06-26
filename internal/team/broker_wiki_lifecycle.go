package team

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// Wiki worker lifecycle. ensureWikiWorker initialises the markdown-
// backend pipeline (repo + index + worker + extractor + DLQ + lint
// cron + skill-compile cron + Stage B synthesizer) once on broker
// Start(). The init is intentionally never broker-fatal — markdown
// writes degrade to ErrWorkerStopped until a user runs the binary in
// an environment with `git` installed and a writable wiki root.
//
// This is the largest single concern in broker.go that wasn't already
// in its own file. Pulling it out of the Start() side keeps the wiki
// boot path reviewable in isolation: read this file end-to-end and you
// know what happens during the markdown-backend cold start.

// ensureWikiWorker initializes the markdown-backend wiki worker when the
// resolved memory backend is "markdown". Idempotent on success; on failure
// (git missing, fsck + backup double-fault, etc.) it logs and returns so
// the next caller can retry. Never crashes the broker — the worker is
// advisory; writes simply fail with ErrWorkerStopped until init succeeds.
//
// Retry semantics matter: a transient repo.Init failure (e.g. parent dir
// permissions flap, git temporarily missing from PATH) used to consume
// sync.Once and leave wikiWorker permanently nil. Now any caller can
// retry — handlers in broker_notebook.go / broker_review.go invoke this
// before checking WikiWorker so a 503 self-heals on the next request.
func (b *Broker) ensureWikiWorker() {
	if config.ResolveMemoryBackend("") != config.MemoryBackendMarkdown {
		return
	}
	b.wikiInitMu.Lock()
	defer b.wikiInitMu.Unlock()
	b.mu.Lock()
	already := b.wikiWorker != nil
	b.mu.Unlock()
	if already {
		return
	}
	b.initWikiWorker()
}

// WikiInitErr returns the most recent ensureWikiWorker error, or nil if
// the worker is up or has not yet been attempted. Used by /health and by
// 503 responses so the underlying init failure is visible to operators
// instead of buried in broker stdout.
func (b *Broker) WikiInitErr() error {
	b.wikiInitMu.Lock()
	defer b.wikiInitMu.Unlock()
	return b.wikiInitErr
}

func (b *Broker) initWikiWorker() {
	repo := NewRepo()
	lifecycleCtx := b.brokerLifecycleContext()
	ctx, cancel := context.WithTimeout(lifecycleCtx, 30*time.Second)
	defer cancel()

	if err := repo.Init(ctx); err != nil {
		b.wikiInitErr = fmt.Errorf("repo init: %w", err)
		log.Printf("wiki: init failed, markdown backend unavailable: %v", err)
		return
	}
	// Belt-and-suspenders: recover any dirty tree from a crashed prior run.
	if err := repo.RecoverDirtyTree(ctx); err != nil {
		log.Printf("wiki: recover-dirty-tree failed: %v", err)
	}
	// Double-fault recovery: if fsck fails, try the backup mirror; otherwise
	// leave the worker un-initialized so writes fail cleanly.
	if err := repo.Fsck(ctx); err != nil {
		log.Printf("wiki: fsck failed (%v); attempting restore from backup", err)
		if restoreErr := repo.RestoreFromBackup(ctx); restoreErr != nil {
			b.wikiInitErr = fmt.Errorf("fsck and backup restore failed: %w", errors.Join(err, restoreErr))
			log.Printf("wiki: double-fault (repo corrupt + backup missing): %v", restoreErr)
			return
		}
	}

	idx := NewWikiIndex(repo.Root())

	worker := NewWikiWorkerWithIndex(repo, b, idx)
	worker.Start(lifecycleCtx)

	// Obsidian round-trip: fsnotify watcher that catches external edits to
	// <wiki-root>/team/ and routes them through Repo.Commit under the
	// per-human identity. Spec: WIKI-OBSIDIAN-COMPATIBILITY.md §6. Toggle
	// off with WUPHF_OBSIDIAN_WATCHER=0. Non-fatal on failure.
	b.ensureObsidianWatcher(lifecycleCtx, worker, idx)

	// Wire the extraction loop: artifact commits → extract_entities_lite →
	// WikiIndex. DLQ lives under <wiki>/.dlq/. Extractor failures never
	// fail the commit path — DLQ absorbs everything per §11.13.
	dlq := NewDLQ(repo.Root())
	extractor := NewExtractor(brokerQueryProvider{}, worker, dlq, idx)
	worker.SetExtractor(extractor)

	// S2 feeder 4: route compiled-playbook + team-learnings commits into the
	// immutable source layer (kind=note). The callback fires off-lock on the
	// worker's drain goroutine; captureSource is a non-blocking buffered send,
	// so this never blocks the drain and never re-enters b.mu.
	worker.SetSourceCaptureHook(func(job SourceCaptureJob) {
		b.captureSource(job)
	})

	// PR 2: human "remember" intent classifier → direct team_wiki_write.
	// The writer is started before the broker mutex is taken, so the
	// goroutine is alive by the time the PostMessage hook can fire.
	humanWiki := NewHumanWikiIntentWriter(worker)
	humanWiki.Start(lifecycleCtx)

	b.mu.Lock()
	b.wikiWorker = worker
	b.wikiIndex = idx
	b.wikiExtractor = extractor
	b.wikiDLQ = dlq
	b.readLog = NewReadLog(repo.Root())
	b.humanWikiWriter = humanWiki
	b.mu.Unlock()
	// Init succeeded; clear any cached failure so future calls don't surface
	// stale errors from a previous attempt.
	b.wikiInitErr = nil

	b.backfillAgentFilesForRoster()

	// Skill status reconciliation: now that the wiki worker is wired,
	// prefer the on-disk SKILL.md frontmatter status over the potentially
	// stale broker-state.json snapshot. This closes the race window where a
	// restart after an archive (or approve) call that missed saveLocked would
	// silently revert the in-memory status.
	b.reconcileSkillStatusFromDisk()

	// Skill file backfill: skills that exist in broker state but have no
	// SKILL.md on disk (e.g. created before the wiki write was wired into
	// handlePostSkill, or during a window when wikiWorker was nil) get
	// written here so /wiki/article?path=team/skills/<slug>.md no longer
	// 404s. Runs async because each enqueue blocks on a git commit and the
	// broker should not stall startup waiting for them.
	go b.backfillSkillFilesFromState(lifecycleCtx)

	// Boot reconcile: walk the full wiki tree and populate the index from
	// existing markdown + jsonl. Runs async so it does not delay broker
	// startup. The per-commit ReconcilePath calls keep the index live once
	// the reconcile finishes. If reconcile fails the index is empty but
	// readable — it will self-heal on the next ReconcilePath call.
	go func() {
		bgCtx, cancel := context.WithTimeout(lifecycleCtx, 5*time.Minute)
		defer cancel()
		if err := idx.ReconcileFromMarkdown(bgCtx); err != nil {
			log.Printf("wiki_index: boot reconcile failed: %v", err)
		} else {
			log.Printf("wiki_index: boot reconcile complete")
		}
	}()

	// Daily lint cron. The schedule is controlled by WUPHF_LINT_CRON (default
	// "09:00" local time). Empty string disables the cron (useful in tests).
	// The goroutine is cancelled by the background context when the broker
	// shuts down.
	b.startLintCron(lifecycleCtx, idx, worker)

	// Stage A skill-compile cron. Walks the wiki and asks the LLM to extract
	// candidate skills. Cron runs at WUPHF_SKILL_COMPILE_INTERVAL (default
	// 30m); cooldown gates back-to-back ticks via WUPHF_SKILL_COMPILE_COOLDOWN
	// (default 25m). Set the interval to "0" or "disabled" to silence the cron.
	b.startSkillCompileCron(lifecycleCtx)
	b.startSkillCompileEventListener(lifecycleCtx)
}

// requireWikiWorker is the standard retry-and-503 helper for HTTP handlers
// that need a live wiki worker. It calls ensureWikiWorker (which retries
// init if a prior attempt failed), returns the worker on success, and
// writes a 503 with the underlying init error on failure. Handlers should
// short-circuit when this returns nil. The error label is used in the JSON
// body to name the surface that needs the worker.
func (b *Broker) requireWikiWorker(w http.ResponseWriter, label string) *WikiWorker {
	b.ensureWikiWorker()
	worker := b.WikiWorker()
	if worker != nil {
		return worker
	}
	msg := label + " backend is not active"
	if err := b.WikiInitErr(); err != nil {
		msg = msg + ": " + err.Error()
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": msg})
	return nil
}

func (b *Broker) brokerLifecycleContext() context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lifecycleCtx == nil {
		b.lifecycleCtx, b.lifecycleCancel = context.WithCancel(context.Background())
	}
	return b.lifecycleCtx
}
