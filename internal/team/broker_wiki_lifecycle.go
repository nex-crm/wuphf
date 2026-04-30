package team

import (
	"context"
	"log"
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
// resolved memory backend is "markdown". Runs once. Never crashes the
// broker on wiki init failure — the worker is advisory; writes simply fail
// with ErrWorkerStopped until a user runs `wuphf` with git installed.
func (b *Broker) ensureWikiWorker() {
	if config.ResolveMemoryBackend("") != config.MemoryBackendMarkdown {
		return
	}
	b.mu.Lock()
	if b.wikiWorker != nil {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	repo := NewRepo()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := repo.Init(ctx); err != nil {
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
			log.Printf("wiki: double-fault (repo corrupt + backup missing): %v", restoreErr)
			return
		}
	}

	idx := NewWikiIndex(repo.Root())

	worker := NewWikiWorkerWithIndex(repo, b, idx)
	worker.Start(context.Background())

	// Wire the extraction loop: artifact commits → extract_entities_lite →
	// WikiIndex. DLQ lives under <wiki>/.dlq/. Extractor failures never
	// fail the commit path — DLQ absorbs everything per §11.13.
	dlq := NewDLQ(repo.Root())
	extractor := NewExtractor(brokerQueryProvider{}, worker, dlq, idx)
	worker.SetExtractor(extractor)

	b.mu.Lock()
	b.wikiWorker = worker
	b.wikiIndex = idx
	b.wikiExtractor = extractor
	b.wikiDLQ = dlq
	b.readLog = NewReadLog(WikiRootDir())
	b.mu.Unlock()

	// Skill status reconciliation: now that the wiki worker is wired,
	// prefer the on-disk SKILL.md frontmatter status over the potentially
	// stale broker-state.json snapshot. This closes the race window where a
	// restart after an archive (or approve) call that missed saveLocked would
	// silently revert the in-memory status.
	b.reconcileSkillStatusFromDisk()

	// Boot reconcile: walk the full wiki tree and populate the index from
	// existing markdown + jsonl. Runs async so it does not delay broker
	// startup. The per-commit ReconcilePath calls keep the index live once
	// the reconcile finishes. If reconcile fails the index is empty but
	// readable — it will self-heal on the next ReconcilePath call.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
	b.startLintCron(context.Background(), idx, worker)

	// Stage A skill-compile cron. Walks the wiki and asks the LLM to extract
	// candidate skills. Cron runs at WUPHF_SKILL_COMPILE_INTERVAL (default
	// 30m); cooldown gates back-to-back ticks via WUPHF_SKILL_COMPILE_COOLDOWN
	// (default 25m). Set the interval to "0" or "disabled" to silence the cron.
	b.startSkillCompileCron(context.Background())
	b.startSkillCompileEventListener(context.Background())

	// Stage B synthesizer: lazily constructed alongside the Stage A scanner so
	// the compile cron drives both passes from a single trigger. Tests can
	// inject a fake via SetSkillSynthesizer.
	b.ensureSkillSynthesizer()
}
