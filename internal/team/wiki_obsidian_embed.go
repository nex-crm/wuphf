package team

// wiki_obsidian_embed.go owns the image-embed ingestion pass the watcher
// runs after the loose-link normalizer and before Repo.Commit. Per
// WIKI-OBSIDIAN-COMPATIBILITY §7.2, an `![[image.png]]` saved next to the
// brief is moved to team/inbox/raw/ (the canonical attachment folder) and
// the embed is rewritten to point at the new path.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// embedReferencePattern matches `![[target]]` and `![[target|alt]]`. Targets
// may contain spaces or directory separators; we constrain only the bracket
// boundaries.
var embedReferencePattern = regexp.MustCompile(`!\[\[([^\]|]+)(\|[^\]]*)?\]\]`)

// IngestImageEmbeds copies any vault-local image referenced by `![[...]]`
// into team/inbox/raw/ and rewrites the brief body to use the canonical
// path. It returns the (possibly unchanged) body plus the list of files
// moved so the caller can attribute them in the commit message.
//
// relPath is the brief's path relative to the wiki root (e.g.
// `team/people/sarah.md`); the function never moves files for non-brief
// paths.
//
// Targets already under `inbox/raw/` are preserved verbatim. Targets that
// do not resolve to a file on disk are left in place — lint catches the
// broken reference downstream.
func IngestImageEmbeds(repo *Repo, relPath, body string) (string, []string, error) {
	if repo == nil {
		return body, nil, errors.New("wiki obsidian embed: nil repo")
	}
	if !isBriefPath(relPath) {
		return body, nil, nil
	}
	root := repo.Root()
	briefDir := filepath.Dir(filepath.Join(root, filepath.FromSlash(relPath)))

	var ingested []string
	var firstErr error

	rewritten := embedReferencePattern.ReplaceAllStringFunc(body, func(match string) string {
		if firstErr != nil {
			return match
		}
		m := embedReferencePattern.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		target := strings.TrimSpace(m[1])
		alt := m[2]
		if target == "" {
			return match
		}
		// Already canonical.
		if strings.HasPrefix(target, "inbox/raw/") {
			return match
		}
		// Path with directory separators that does NOT live under inbox/raw/
		// is out of scope: only bare-filename embeds (Obsidian's default
		// paste behaviour) get auto-promoted to the attachment folder.
		if strings.ContainsRune(target, '/') {
			return match
		}
		src := filepath.Join(briefDir, target)
		info, err := os.Stat(src)
		if err != nil || info.IsDir() {
			return match
		}
		dstRel := "inbox/raw/" + target
		dst := filepath.Join(root, "team", filepath.FromSlash(dstRel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			firstErr = fmt.Errorf("wiki obsidian embed: mkdir inbox/raw: %w", err)
			return match
		}
		if _, err := os.Stat(dst); err == nil {
			// File already exists at the destination. Leave the source in
			// place (the human may have intentionally pasted a duplicate)
			// but rewrite the reference so the canonical version is used.
			ingested = append(ingested, dstRel)
			return "![[" + dstRel + alt + "]]"
		}
		if err := os.Rename(src, dst); err != nil {
			firstErr = fmt.Errorf("wiki obsidian embed: move %s: %w", target, err)
			return match
		}
		ingested = append(ingested, dstRel)
		return "![[" + dstRel + alt + "]]"
	})

	if firstErr != nil {
		return body, nil, firstErr
	}
	return rewritten, ingested, nil
}
