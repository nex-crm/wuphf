package wuphf_test

import (
	"testing"

	wuphf "github.com/nex-crm/wuphf"
	"github.com/nex-crm/wuphf/internal/operations"
)

// Verifies the //go:embed all:templates/... directive in templates_embed.go
// actually pulls the blueprint YAML tree into the binary, and that the
// init() wired it into the operations loader's fallback FS. Without this
// test, a future refactor that drops the embed or skips the init wiring
// would silently revert the "`npx wuphf` shows only From scratch" bug.
func TestTemplatesEmbedWiresOperationsFallback(t *testing.T) {
	// Minimal guard against the embed going empty (e.g. someone builds
	// with a broken working tree).
	tfs, ok := wuphf.TemplatesFS()
	if !ok {
		t.Fatal("TemplatesFS() returned ok=false — embed is empty?")
	}
	if tfs == nil {
		t.Fatal("TemplatesFS() returned nil fs when ok=true")
	}

	// Passing "" as repoRoot forces ListBlueprints to go through the
	// fallback FS. If the init in templates_embed.go didn't run (or the
	// embed is missing), this returns 0 blueprints.
	blueprints, err := operations.ListBlueprints("")
	if err != nil {
		t.Fatalf("ListBlueprints(\"\"): %v", err)
	}
	if len(blueprints) == 0 {
		t.Fatal("expected shipped blueprints from embed, got 0 — embed or init wiring broken")
	}
}
