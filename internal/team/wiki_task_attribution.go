package team

import (
	"net/http"
	"strings"
)

// Wiki content IS the artifacts agents produce. The one thing the wiki did not
// surface was WHICH task produced a given article — this resolves that link so
// the article view can show "Produced for <task>" naturally, in both
// directions we already record:
//
//   - a task that declared the article as its delivered work product
//     (teamTask.Artifact == ref), and
//   - a visual artifact that names its originating task (RichArtifact
//     .RelatedTaskID), keyed by the artifact id or its promoted wiki path.
//
// ref is either a visual-artifact id ("ra_…") or a wiki-relative article path
// ("team/playbooks/launch.md").

// ArticleAttribution names the task an article was produced for.
type ArticleAttribution struct {
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"`
	Owner     string `json:"owner,omitempty"`
}

// resolveArticleAttribution returns the producing task for an article ref.
func (b *Broker) resolveArticleAttribution(ref string) (ArticleAttribution, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ArticleAttribution{}, false
	}

	// 1. A task that claims this ref as its delivered artifact wins — it is the
	//    most direct, human-or-CEO-recorded statement of provenance.
	taskID := ""
	b.mu.Lock()
	for i := range b.tasks {
		if strings.TrimSpace(b.tasks[i].Artifact) == ref {
			taskID = b.tasks[i].ID
			break
		}
	}
	b.mu.Unlock()

	// 2. Otherwise fall back to the visual artifact's own RelatedTaskID,
	//    resolved by id (ra_…) or by promoted wiki path.
	if taskID == "" {
		if worker := b.WikiWorker(); worker != nil {
			if strings.HasPrefix(ref, "ra_") {
				if art, _, err := worker.RichArtifact(ref); err == nil {
					taskID = strings.TrimSpace(art.RelatedTaskID)
				}
			} else if arts, err := worker.ListRichArtifacts(RichArtifactFilter{PromotedWikiPath: ref}); err == nil && len(arts) > 0 {
				taskID = strings.TrimSpace(arts[0].RelatedTaskID)
			}
		}
	}
	if taskID == "" {
		return ArticleAttribution{}, false
	}

	// Resolve the title/owner. A ref can point at a task that no longer exists
	// (deleted) — then there is nothing to attribute.
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			return ArticleAttribution{
				TaskID:    taskID,
				TaskTitle: b.tasks[i].Title,
				Owner:     b.tasks[i].Owner,
			}, true
		}
	}
	return ArticleAttribution{}, false
}

// handleArticleAttribution serves GET /article-attribution?ref=<id|path>.
// Always 200; a null attribution means "no producing task found".
func (b *Broker) handleArticleAttribution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	att, ok := b.resolveArticleAttribution(r.URL.Query().Get("ref"))
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"attribution": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attribution": att})
}
