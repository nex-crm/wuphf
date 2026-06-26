package team

// wiki_compile_state.go — the S4 idempotency layer. The compile engine caches
// its Phase-1 extraction per source (keyed by the source content hash) and the
// Phase-2 input hash per compiled page, so a recompile whose inputs are
// unchanged makes ZERO PamRunner calls.
//
// The state is a plain JSON document at the repo-root ".compile/state.json"
// (sibling to team/ and sources/, NOT a wiki article — it never reaches the
// catalog or the single-writer commit queue). It is a rebuildable cache: a
// missing or corrupt file simply means "recompile everything", never an error
// that aborts the run.
//
// Two maps drive the two skips:
//
//	Sources[id]  = {content_hash, []ExtractedConcept}
//	    a source whose on-disk content_hash matches the cached one reuses the
//	    cached concepts — no Phase-1 extract call.
//	Pages[slug]  = {input_hash}
//	    a merged concept whose input hash matches the cached one AND whose page
//	    file still exists on disk skips Phase-2 — no author call, no rewrite.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// compileStateVersion is bumped when the on-disk schema changes so a stale
// cache from an older binary is discarded (treated as empty) rather than
// misread.
const compileStateVersion = 1

// compileStateDir / compileStateFile locate the cache under the repo root.
const (
	compileStateDir  = ".compile"
	compileStateFile = "state.json"
)

// CachedExtraction is the Phase-1 result for one source, keyed in CompileState
// by source ID. ContentHash is the source's ContentHash at extraction time;
// when it still matches the source on disk the cached Concepts are reused
// verbatim and no extract call is made.
type CachedExtraction struct {
	ContentHash string             `json:"content_hash"`
	Concepts    []ExtractedConcept `json:"concepts"`
}

// CompiledPageState records the Phase-2 input hash for one compiled page,
// keyed in CompileState by slug. When the recomputed input hash matches and
// the page file still exists, the page write is skipped entirely.
type CompiledPageState struct {
	InputHash string `json:"input_hash"`
	Kind      string `json:"kind"`
}

// CompileState is the persisted idempotency cache. The zero value (and a
// missing file) is a valid empty state meaning "nothing cached yet".
type CompileState struct {
	Version int                          `json:"version"`
	Sources map[string]CachedExtraction  `json:"sources"`
	Pages   map[string]CompiledPageState `json:"pages"`
}

// newCompileState returns an empty, ready-to-use state with initialized maps.
func newCompileState() CompileState {
	return CompileState{
		Version: compileStateVersion,
		Sources: make(map[string]CachedExtraction),
		Pages:   make(map[string]CompiledPageState),
	}
}

// compileStatePath returns the absolute path to the cache file under the repo
// root.
func compileStatePath(repo *Repo) string {
	return filepath.Join(repo.Root(), compileStateDir, compileStateFile)
}

// LoadCompileState reads the cache from disk. A missing file yields an empty
// state (NOT an error). A corrupt or version-mismatched file is also treated
// as empty — the cache is rebuildable, so a parse failure must never abort a
// compile. A genuine read error (e.g. permission) is returned wrapped.
func LoadCompileState(repo *Repo) (CompileState, error) {
	if repo == nil {
		return CompileState{}, fmt.Errorf("compile state: repo is nil")
	}
	data, err := os.ReadFile(compileStatePath(repo))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return newCompileState(), nil
		}
		return CompileState{}, fmt.Errorf("compile state: read: %w", err)
	}
	var st CompileState
	if err := json.Unmarshal(data, &st); err != nil || st.Version != compileStateVersion {
		// Corrupt or stale cache: discard and recompile from scratch.
		return newCompileState(), nil
	}
	if st.Sources == nil {
		st.Sources = make(map[string]CachedExtraction)
	}
	if st.Pages == nil {
		st.Pages = make(map[string]CompiledPageState)
	}
	return st, nil
}

// SaveCompileState writes the cache to disk atomically (write temp + rename) so
// a crash mid-write cannot leave a truncated JSON file that the next run would
// discard. The .compile dir is created if missing.
func SaveCompileState(repo *Repo, st CompileState) error {
	if repo == nil {
		return fmt.Errorf("compile state: repo is nil")
	}
	st.Version = compileStateVersion
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("compile state: marshal: %w", err)
	}
	dir := filepath.Join(repo.Root(), compileStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("compile state: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, compileStateFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("compile state: temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("compile state: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("compile state: close temp: %w", err)
	}
	if err := os.Rename(tmpName, compileStatePath(repo)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("compile state: rename: %w", err)
	}
	return nil
}

// pageInputHash is the deterministic fingerprint of a merged concept's Phase-2
// input. It folds the sorted source ids + their content hashes plus the
// title/kind/summary, so a page is recompiled iff one of those changed. Body
// content is deliberately NOT part of the hash: a deterministic interlink
// rewrite must not invalidate the cache.
func pageInputHash(mc MergedConcept) string {
	parts := make([]string, 0, len(mc.Sources))
	for _, s := range mc.Sources {
		parts = append(parts, s.ID+"@"+s.ContentHash)
	}
	sort.Strings(parts)
	h := sha256.New()
	writeHashField(h, mc.Title)
	writeHashField(h, mc.Kind)
	writeHashField(h, mc.Summary)
	for _, p := range parts {
		writeHashField(h, p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeHashField writes one length-delimited field so distinct field
// boundaries cannot collide (e.g. {"ab","c"} vs {"a","bc"}).
func writeHashField(h interface{ Write([]byte) (int, error) }, s string) {
	var lenBuf [8]byte
	n := uint64(len(s))
	for i := 0; i < 8; i++ {
		lenBuf[i] = byte(n >> (8 * i))
	}
	_, _ = h.Write(lenBuf[:])
	_, _ = h.Write([]byte(s))
}
