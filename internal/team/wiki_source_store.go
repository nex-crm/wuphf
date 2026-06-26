package team

// wiki_source_store.go is the read side of the immutable source layer. It
// scans the repo-root sources/ subtree and parses each record back into a
// SourceRecord. The compile engine and the /sources/* HTTP handlers read
// through here; the single-writer WikiWorker owns all writes (CommitSource).

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListSources walks sources/ and returns every parsed record, sorted by
// CapturedAt descending (most recent first), ties broken by id for a stable
// order. A missing sources/ dir is not an error — it returns an empty slice.
// Dot-files and non-.md files are skipped, matching walkCatalogArticles.
func ListSources(repo *Repo) ([]SourceRecord, error) {
	if repo == nil {
		return nil, fmt.Errorf("source store: repo is nil")
	}
	root := repo.Root()
	base := filepath.Join(root, sourcesDir)

	var records []SourceRecord
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A missing sources/ tree (nothing captured yet) or an unreadable
			// entry should not blow up the listing — skip and keep walking.
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return fmt.Errorf("source store: walk %s: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		// Skip dot-prefixed files (system markers) and non-markdown files.
		if strings.HasPrefix(d.Name(), ".") || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			if errors.Is(readErr, fs.ErrNotExist) || errors.Is(readErr, fs.ErrPermission) {
				return nil
			}
			return fmt.Errorf("source store: read %s: %w", path, readErr)
		}
		rec, parseErr := ParseSourceMarkdown(content)
		if parseErr != nil {
			return fmt.Errorf("source store: parse %s: %w", path, parseErr)
		}
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].CapturedAt.Equal(records[j].CapturedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].CapturedAt.After(records[j].CapturedAt)
	})
	return records, nil
}

// ReadSource returns the single source record at sources/{kind}/{id}.md.
// Returns a wrapped fs.ErrNotExist when the record is absent so callers can
// distinguish a 404 from a genuine read error.
func ReadSource(repo *Repo, kind SourceKind, id string) (SourceRecord, error) {
	if repo == nil {
		return SourceRecord{}, fmt.Errorf("source store: repo is nil")
	}
	if !kind.IsValid() {
		return SourceRecord{}, fmt.Errorf("source store: invalid kind %q", kind)
	}
	if strings.TrimSpace(id) == "" {
		return SourceRecord{}, fmt.Errorf("source store: id is required")
	}
	relPath := SourceRelPath(kind, id)
	if !IsSourcePath(relPath) {
		return SourceRecord{}, fmt.Errorf("source store: derived path %q is not a valid source path", relPath)
	}
	fullPath := filepath.Join(repo.Root(), filepath.FromSlash(relPath))
	content, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SourceRecord{}, fmt.Errorf("source store: read %s: %w", relPath, fs.ErrNotExist)
		}
		return SourceRecord{}, fmt.Errorf("source store: read %s: %w", relPath, err)
	}
	rec, err := ParseSourceMarkdown(content)
	if err != nil {
		return SourceRecord{}, fmt.Errorf("source store: parse %s: %w", relPath, err)
	}
	return rec, nil
}
