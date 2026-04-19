package operations

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBlueprintsIncludeWikiSchema verifies every shipped operations
// blueprint ships a non-empty wiki_schema. This is the guard rail that
// catches a future PR accidentally stripping the schema from one of the
// six domains — without it, the onboarding hook silently no-ops and the
// /wiki UI lands on an empty dir for that blueprint.
//
// Expected domains and minimum counts come from the parallelization
// design doc. Minimums protect against a blueprint getting trimmed to a
// single article placeholder — every blueprint should ship at least 3
// thematic dirs and 3 bootstrap articles.
func TestBlueprintsIncludeWikiSchema(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	for _, id := range []string{
		"bookkeeping-invoicing-service",
		"local-business-ai-package",
		"multi-agent-workflow-consulting",
		"niche-crm",
		"paid-discord-community",
		"youtube-factory",
	} {
		t.Run(id, func(t *testing.T) {
			bp, err := LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load %s: %v", id, err)
			}
			if bp.WikiSchema == nil {
				t.Fatalf("blueprint %s: wiki_schema is nil — every shipped blueprint must ship one", id)
			}
			if len(bp.WikiSchema.Dirs) < 3 {
				t.Errorf("blueprint %s: expected >=3 thematic dirs, got %d", id, len(bp.WikiSchema.Dirs))
			}
			if len(bp.WikiSchema.Bootstrap) < 3 {
				t.Errorf("blueprint %s: expected >=3 bootstrap articles, got %d", id, len(bp.WikiSchema.Bootstrap))
			}
			for _, item := range bp.WikiSchema.Bootstrap {
				if strings.TrimSpace(item.Path) == "" {
					t.Errorf("blueprint %s: bootstrap item has empty path", id)
				}
				if !strings.HasPrefix(item.Path, "team/") {
					t.Errorf("blueprint %s: bootstrap path %q not under team/", id, item.Path)
				}
				if strings.TrimSpace(item.Skeleton) == "" {
					t.Errorf("blueprint %s: bootstrap %q has empty skeleton", id, item.Path)
				}
			}
		})
	}
}
