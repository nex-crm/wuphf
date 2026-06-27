package team

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// agent_path_validation.go holds the agent-scoped path validators shared by
// the rich-artifact and agent-file surfaces. They were previously colocated
// with the (now removed) notebook worker; the validators themselves are
// generic guards over agents/{slug}/... paths and are kept here so the
// surviving callers keep compiling.
//
// Note on the "notebook" name: the user/agent-facing surface is now called
// "visual artifact" (tools visual_artifact_*, routes /visual-artifacts), but
// the on-disk storage dir is deliberately still agents/{slug}/notebook/ and
// these validators still say "notebook". Renaming the path would orphan every
// already-stored artifact, so the legacy dir name is retained for data
// compatibility while only the public surface was renamed.

// ErrNotebookPathNotAuthorOwned is returned when a write path does not live
// under the caller's own agents/{my_slug}/notebook/ subtree.
var ErrNotebookPathNotAuthorOwned = errors.New("notebook_path_not_author_owned: write path must live under agents/{my_slug}/notebook/")

// validateNotebookWritePath enforces that the path lives under
// agents/{slug}/notebook/ where slug is the caller's own agent slug.
func validateNotebookWritePath(slug, relPath string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("notebook: my_slug is required")
	}
	if err := validateNotebookSlug(slug); err != nil {
		return err
	}
	if err := validateNotebookPath(relPath); err != nil {
		return err
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	prefix := "agents/" + slug + "/notebook/"
	if !strings.HasPrefix(clean, prefix) {
		return fmt.Errorf("%w: got path %q, expected prefix %q", ErrNotebookPathNotAuthorOwned, relPath, prefix)
	}
	rest := strings.TrimPrefix(clean, prefix)
	if rest == "" || strings.Contains(rest, "/") {
		// Entries must live directly under notebook/ (no subdirectories).
		return fmt.Errorf("notebook: entries must live directly under %s (no subdirectories); got %q", prefix, relPath)
	}
	return nil
}

// validateNotebookPath allows any agents/{slug}/notebook/{file}.md path,
// regardless of which slug owns it. Used by read/list/search which are
// cross-agent.
func validateNotebookPath(relPath string) error {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return fmt.Errorf("notebook: path is required")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("notebook: path must be relative; got %q", relPath)
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." {
		return fmt.Errorf("notebook: path must not contain ..; got %q", relPath)
	}
	if !strings.HasPrefix(clean, "agents/") {
		return fmt.Errorf("notebook: path must be within agents/; got %q", relPath)
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 4 || parts[2] != "notebook" {
		return fmt.Errorf("notebook: path must match agents/{slug}/notebook/...; got %q", relPath)
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".md") {
		return fmt.Errorf("notebook: path must end with .md; got %q", relPath)
	}
	return nil
}

// validateNotebookSlug guards against slug values that would break filesystem
// paths or be mistaken for directory traversal. Agent slugs are kebab-case
// alphanumerics in WUPHF.
func validateNotebookSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("notebook: slug is required")
	}
	if slug == "." || slug == ".." {
		return fmt.Errorf("notebook: invalid slug %q", slug)
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return fmt.Errorf("notebook: slug %q contains invalid characters (allowed: a-z, A-Z, 0-9, -, _)", slug)
		}
	}
	return nil
}
