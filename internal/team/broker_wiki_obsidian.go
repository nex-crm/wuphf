package team

// broker_wiki_obsidian.go wires the ObsidianWatcher into the broker's wiki
// lifecycle. Phase 3+4 of WIKI-OBSIDIAN-COMPATIBILITY landed the daemon,
// frontmatter sentinel writer, loose-link normalizer, and image-embed
// ingester as building blocks; this file connects them to the runtime so
// edits made in an Obsidian editor against `<wiki-root>/team/` flow back
// through Repo.Commit under the user's per-human git identity.
//
// Boot order, from initWikiWorker:
//
//	1. Repo.Init succeeds
//	2. WikiWorker.Start(lifecycleCtx)
//	3. <-- ensureObsidianWatcher mounts here -->
//	4. extractor + auxiliary loops
//
// Disabled via WUPHF_OBSIDIAN_WATCHER=0 (off) for operators who want to
// keep the historical "single-writer through WikiWorker" posture while
// they evaluate the feature. Default ON because the spec treats the
// watcher as the load-bearing layer of the Obsidian round-trip; without
// it the rest of Phase 3+4 (sentinel, normalizer, embed) is dead code.

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

// obsidianNormalizerTimeout bounds each signal-index lookup the watcher's
// normalizer issues per loose `[[...]]` it encounters. The index is local
// (sqlite or in-memory) so milliseconds suffice; the timeout exists to keep
// a stuck index from blocking the watcher's commit pipeline.
const obsidianNormalizerTimeout = 250 * time.Millisecond

// ensureObsidianWatcher instantiates and starts the ObsidianWatcher when
// (a) the worker is up, (b) WUPHF_OBSIDIAN_WATCHER is not "0"/"off"/"false".
// Non-fatal: a watcher start failure logs and continues — wiki writes via
// WikiWorker still work; only the external-edit round-trip is degraded.
//
// Wiring:
//
//	identity   → HumanIdentityRegistry.Local() (real git user.name/email,
//	             or the v1.4 "human" fallback when unset)
//	normalizer → signal index → kinded form (Phase 4 §5)
//	embed      → IngestImageEmbeds (Phase 4 §7.2)
func (b *Broker) ensureObsidianWatcher(ctx context.Context, worker *WikiWorker, idx *WikiIndex) {
	if !obsidianWatcherEnabled() {
		return
	}
	if worker == nil || worker.Repo() == nil {
		return
	}
	watcher := NewObsidianWatcher(worker.Repo(), worker)
	watcher.SetIdentity(brokerObsidianIdentity(brokerHumanIdentityRegistry()))
	if idx != nil {
		watcher.SetNormalizer(brokerObsidianNormalizer(NewWikiIndexSignalAdapter(idx)))
	}
	watcher.SetEmbedIngester(IngestImageEmbeds)

	if err := watcher.Start(ctx); err != nil {
		log.Printf("obsidian watcher: start failed (round-trip disabled): %v", err)
		return
	}

	b.mu.Lock()
	b.obsidianWatcher = watcher
	b.mu.Unlock()
}

// obsidianWatcherEnabled returns false only when the env var explicitly
// opts out. Empty / unset → enabled.
func obsidianWatcherEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WUPHF_OBSIDIAN_WATCHER"))) {
	case "0", "off", "false", "no", "disabled":
		return false
	}
	return true
}

// brokerObsidianIdentity adapts the per-human registry to the watcher's
// identity callback. Returns ok=false only when no slug can be resolved at
// all — the v1.4 fallback identity (`human`) IS resolved and counts as a
// valid attribution target, matching how human_commit.go treats it.
func brokerObsidianIdentity(reg *HumanIdentityRegistry) ObsidianWatcherIdentity {
	return func() (string, bool) {
		if reg == nil {
			return "", false
		}
		id := reg.Local()
		slug := strings.TrimSpace(id.Slug)
		if slug == "" {
			return "", false
		}
		return slug, true
	}
}

// brokerObsidianNormalizer adapts a SignalIndex to the watcher's
// loose-link resolver. The watcher hands us either a bare slug (`acme`)
// or a display string (`Acme Corp`) extracted from a non-kinded
// `[[...]]`; we resolve via EntityBySlug first, then EntityByName with
// strict single-match semantics. Ambiguity (zero or multiple matches)
// returns ok=false so the link stays loose — the basename-collision
// guard required by WIKI-OBSIDIAN-COMPATIBILITY §5.
func brokerObsidianNormalizer(idx SignalIndex) ObsidianLooseLinkResolver {
	if idx == nil {
		return nil
	}
	return func(input string) (EntityKind, string, bool) {
		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			return "", "", false
		}

		// 1. Treat the input as a canonical slug. EntityBySlug is keyed by
		// `slug` only (kind is not part of the lookup), so a match here
		// gives us the entity's actual kind too. Each lookup gets its own
		// timeout budget so a slow first call cannot starve the second.
		slugCtx, slugCancel := context.WithTimeout(context.Background(), obsidianNormalizerTimeout)
		ent, ok, err := idx.EntityBySlug(slugCtx, slugify(trimmed))
		slugCancel()
		if err == nil && ok {
			return ent.Kind, ent.Slug, true
		}

		// 2. Treat the input as a display string. EntityByName returns the
		// candidate set; require a single match. Multiple matches (e.g. a
		// person and a project both named "Acme") stay loose.
		nameCtx, nameCancel := context.WithTimeout(context.Background(), obsidianNormalizerTimeout)
		defer nameCancel()
		hits, err := idx.EntityByName(nameCtx, trimmed)
		if err != nil || len(hits) != 1 {
			return "", "", false
		}
		return hits[0].Kind, hits[0].Slug, true
	}
}
