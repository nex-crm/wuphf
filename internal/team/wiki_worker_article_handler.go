package team

// wiki_worker_article_handler.go holds the GET /wiki/article HTTP handler,
// split out of wiki_worker.go to keep that file under the repo's 1500-LOC
// budget (scripts/check-file-size.sh). The handler is part of the same
// package and behaves identically to its prior in-file definition.

import (
	"errors"
	"log"
	"net/http"
	"strings"
)

// handleWikiArticle returns the rich article metadata for the UI: content +
// title + revisions + contributors + backlinks + word count.
//
//	GET /wiki/article?path=team/people/nazz.md
//
// Response shape matches web/src/api/wiki.ts WikiArticle.
func (b *Broker) handleWikiArticle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateArticlePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	reader := sanitizeReader(r.URL.Query().Get("reader"))
	meta, err := worker.Repo().BuildArticle(r.Context(), relPath, reader, b.WikiReadLog())
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// Attach any visual artifacts whose promotion target points at this
	// article. Best-effort: a listing failure surfaces the article with an
	// empty AttachedArtifacts slice rather than 500'ing the read.
	if attached, listErr := worker.ListRichArtifacts(RichArtifactFilter{PromotedWikiPath: relPath}); listErr == nil {
		out := make([]RichArtifact, 0, len(attached))
		for _, a := range attached {
			out = append(out, a.WithDerivedPromotion())
		}
		meta.AttachedArtifacts = out
	} else {
		log.Printf("wiki article: attached artifact list failed for %s: %v", relPath, listErr)
	}
	// Ghost brief handling: in Demand mode, fire synthesis on first open when
	// facts meet the threshold. In both modes, always surface in-flight state
	// so the "generating..." badge works regardless of how synthesis was triggered.
	if meta.Ghost {
		if synth := b.EntitySynthesizer(); synth != nil {
			// Derive kind and slug from "team/{kind}/{slug}.md".
			parts := strings.SplitN(strings.TrimPrefix(relPath, "team/"), "/", 2)
			if len(parts) == 2 {
				kind := EntityKind(parts[0])
				slug := strings.TrimSuffix(parts[1], ".md")
				if synth.Mode() == SynthesisModeDemand {
					if factLog := b.FactLog(); factLog != nil {
						if facts, _ := factLog.List(kind, slug); len(facts) >= synth.Threshold() {
							if _, enqErr := synth.EnqueueSynthesis(kind, slug, ArchivistAuthor); enqErr != nil && !errors.Is(enqErr, ErrSynthesisQueueSaturated) && !errors.Is(enqErr, ErrSynthesizerStopped) {
								log.Printf("wiki: demand-pull enqueue %s/%s: %v", kind, slug, enqErr)
							}
						}
					}
				}
				// Always reflect in-flight state — auto-mode syntheses triggered at
				// ingest also benefit from the badge.
				meta.SynthesisQueued = synth.IsInflightOrQueued(kind, slug)
			}
		}
	}
	writeJSON(w, http.StatusOK, meta)
}
