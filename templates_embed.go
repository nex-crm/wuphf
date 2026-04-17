package wuphf

import (
	"embed"
	"io/fs"

	"github.com/nex-crm/wuphf/internal/operations"
)

// templatesBundle ships the blueprint YAML tree with the binary so that
// installs without a repo checkout (`npx wuphf`, `curl | bash`) still see
// the full operations and employee blueprint catalog. Without this embed
// the web onboarding wizard silently degrades to "From scratch" only,
// because resolveTemplatesRepoRoot walks the filesystem looking for a
// templates/ directory and returns "" when run outside a checkout.
//
//go:embed all:templates/operations all:templates/employees
var templatesBundle embed.FS

// TemplatesFS returns the embedded templates FS rooted so callers see
// paths like "templates/operations/<id>/blueprint.yaml" — the same layout
// as the on-disk tree. Returns ok=false if the embed is empty (e.g. a
// partial checkout that excludes templates/).
func TemplatesFS() (fs.FS, bool) {
	if _, err := fs.Stat(templatesBundle, "templates/operations"); err != nil {
		return nil, false
	}
	return templatesBundle, true
}

func init() {
	if tfs, ok := TemplatesFS(); ok {
		operations.SetFallbackFS(tfs)
	}
}
