package team

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- Test fixtures ---

// mockDetector is a minimal ChangeDetector for driving table-driven tests
// without touching the real on-disk manifest. It records IsChanged calls
// and MarkIngested calls so tests can assert on them.
type mockDetector struct {
	changed   map[string]bool
	ingested  map[string]string
	hasRoot   bool
	saveCalls int
	saveErr   error
}

func newMockDetector() *mockDetector {
	return &mockDetector{
		changed:  map[string]bool{},
		ingested: map[string]string{},
		hasRoot:  true, // default: bypass the confirmation gate
	}
}

func (m *mockDetector) IsChanged(path string, _ fs.FileInfo) bool {
	if v, ok := m.changed[path]; ok {
		return v
	}
	return true
}

func (m *mockDetector) MarkIngested(path string, _ fs.FileInfo, ctx string) {
	m.ingested[path] = ctx
}

func (m *mockDetector) Save() error {
	m.saveCalls++
	return m.saveErr
}

// setupRoot builds a temp scan root containing the given files. content
// may be empty-string for zero-byte files.
func setupRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

// --- WalkDir tests ---

func TestScannerWalkDirHonorsSkipDirs(t *testing.T) {
	root := setupRoot(t, map[string]string{
		"keep.md":                "A",
		"node_modules/skip.md":   "B",
		".git/skip.md":           "C",
		"sub/keep2.md":           "D",
		"sub/.venv/skip.md":      "E",
		"sub/__pycache__/bad.md": "F",
	})
	got := WalkDir(root, root, WalkOptions{
		Extensions: map[string]struct{}{".md": {}},
		MaxDepth:   20,
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, f := range got {
		names[filepath.Base(f.AbsolutePath)] = true
	}
	if !names["keep.md"] || !names["keep2.md"] {
		t.Fatalf("expected keep.md + keep2.md, got %+v", names)
	}
}

func TestScannerWalkDirRespectsExtensionAllowlist(t *testing.T) {
	root := setupRoot(t, map[string]string{
		"a.md":  "1",
		"b.txt": "2",
		"c.go":  "3",
		"d.py":  "4",
	})
	got := WalkDir(root, root, WalkOptions{
		Extensions: map[string]struct{}{".md": {}, ".txt": {}},
		MaxDepth:   20,
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got))
	}
}

func TestScannerWalkDirEnforcesDepthCap(t *testing.T) {
	root := setupRoot(t, map[string]string{
		"depth0.md":                     "a",
		"one/depth1.md":                 "b",
		"one/two/depth2.md":             "c",
		"one/two/three/depth3.md":       "d",
		"one/two/three/four/depth4.md": "e",
	})
	got := WalkDir(root, root, WalkOptions{
		Extensions: map[string]struct{}{".md": {}},
		MaxDepth:   2,
	})
	names := map[string]bool{}
	for _, f := range got {
		names[filepath.Base(f.AbsolutePath)] = true
	}
	if !names["depth0.md"] || !names["depth1.md"] || !names["depth2.md"] {
		t.Fatalf("missing expected files under depth 2: %+v", names)
	}
	if names["depth3.md"] || names["depth4.md"] {
		t.Fatalf("files beyond depth cap leaked in: %+v", names)
	}
}

func TestScannerWalkDirSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics diverge on Windows")
	}
	root := setupRoot(t, map[string]string{
		"real.md":     "real",
		"sub/file.md": "ok",
	})
	target := filepath.Join(root, "real.md")
	if err := os.Symlink(target, filepath.Join(root, "link.md")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	// Symlinked directory outside the scan root. If we followed symlinks
	// we'd pick up files under outside/.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("s"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	got := WalkDir(root, root, WalkOptions{
		Extensions: map[string]struct{}{".md": {}},
		MaxDepth:   20,
	})
	for _, f := range got {
		base := filepath.Base(f.AbsolutePath)
		if base == "link.md" || base == "secret.md" {
			t.Fatalf("scanner followed a symlink to %s", f.AbsolutePath)
		}
	}
	// Regression anchor for the eng-review decision to diverge from nex-cli.
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 (real.md + sub/file.md), got %d: %+v", len(got), got)
	}
}

func TestScannerWalkDirSilentlyIgnoresUnreadableSubdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics diverge on Windows")
	}
	root := setupRoot(t, map[string]string{
		"ok.md":       "a",
		"locked/x.md": "b",
	})
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(locked, 0o700) // so t.TempDir cleanup works

	got := WalkDir(root, root, WalkOptions{
		Extensions: map[string]struct{}{".md": {}},
		MaxDepth:   20,
	})
	names := map[string]bool{}
	for _, f := range got {
		names[filepath.Base(f.AbsolutePath)] = true
	}
	if !names["ok.md"] {
		t.Fatalf("expected ok.md in results, got %+v", names)
	}
	// Locked subdir contents must not leak.
	if names["x.md"] {
		t.Fatalf("permission-denied subdir leaked a file into results")
	}
}

// --- Secret redaction tests ---

func TestScannerRedactsSecrets(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantHits int
	}{
		{"openai-ish", "token=sk-ABCDEFGHIJKLMNOPQRSTUVWX more stuff", 1},
		{"aws", "id=AKIAIOSFODNN7EXAMPLE here", 1},
		{"bearer", "Authorization: Bearer abcd.ef-012=345", 1},
		{"env-style", "OPENAI_API_KEY=sk-xyz", 1},
		{"github-pat", "token=ghp_" + strings.Repeat("a", 36), 1},
		{"nothing", "just plain prose", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, n := redactSecrets(tc.in)
			if n != tc.wantHits {
				t.Fatalf("want %d hits, got %d. out=%q", tc.wantHits, n, out)
			}
			if tc.wantHits > 0 && !strings.Contains(out, "[REDACTED]") {
				t.Fatalf("expected [REDACTED] marker in %q", out)
			}
		})
	}
}

// --- Slugify tests ---

func TestScannerSlugifyRoot(t *testing.T) {
	cases := map[string]string{
		"/Users/nazz/Documents/notes":       "users-nazz-documents-notes",
		"/tmp/Some Dir With Spaces":         "tmp-some-dir-with-spaces",
		"/":                                 "root",
		"/ABC/def":                          "abc-def",
	}
	for in, want := range cases {
		got := slugifyRoot(in)
		if got != want {
			t.Errorf("slugifyRoot(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- LoadScanExtensions tests ---

func TestScannerLoadScanExtensions(t *testing.T) {
	t.Setenv("WUPHF_SCAN_EXTENSIONS", "")
	def := LoadScanExtensions(nil)
	if _, ok := def[".md"]; !ok {
		t.Fatalf("default should include .md")
	}
	if _, ok := def[".go"]; ok {
		t.Fatalf("default should NOT include .go")
	}
	t.Setenv("WUPHF_SCAN_EXTENSIONS", "go, md , txt")
	env := LoadScanExtensions(nil)
	for _, want := range []string{".go", ".md", ".txt"} {
		if _, ok := env[want]; !ok {
			t.Fatalf("env set missing %s: %+v", want, env)
		}
	}
	override := LoadScanExtensions([]string{".rst"})
	if _, ok := override[".rst"]; !ok || len(override) != 1 {
		t.Fatalf("override should be exactly .rst: %+v", override)
	}
}

// --- Scan flow tests ---

func TestScannerScanHumanConfirmationGate(t *testing.T) {
	root := setupRoot(t, map[string]string{"note.md": "hello"})
	wiki := t.TempDir()
	det := newMockDetector()
	det.hasRoot = false
	// mockDetector always returns false from HasRoot via the type switch
	// default — but we use the real detector path here. Use the real
	// MtimeChangeDetector with an isolated manifest path.
	_ = det
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	real, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	_, preview, err := Scan(context.Background(), ScanOptions{Root: root}, real, wiki, nil)
	if !errors.Is(err, ErrScanConfirmationRequired) {
		t.Fatalf("want ErrScanConfirmationRequired, got %v", err)
	}
	if preview == nil || !preview.NeedsConfig || len(preview.Files) != 1 {
		t.Fatalf("preview malformed: %+v", preview)
	}
}

func TestScannerScanConfirmsAndIngests(t *testing.T) {
	root := setupRoot(t, map[string]string{
		"note.md":        "content one",
		"sub/other.md":   "content two",
	})
	wiki := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	commitCalls := 0
	commit := func(_ context.Context, author, msg string) (string, error) {
		commitCalls++
		if author != scannerSlug {
			t.Errorf("wrong author %q", author)
		}
		if !strings.Contains(msg, "2 files") {
			t.Errorf("unexpected msg %q", msg)
		}
		return "abc123", nil
	}
	res, _, err := Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, wiki, commit)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Ingested != 2 {
		t.Fatalf("want 2 ingested, got %+v", res)
	}
	if res.CommitSHA != "abc123" || commitCalls != 1 {
		t.Fatalf("commit not wired: %+v, calls=%d", res, commitCalls)
	}
	// Files landed under team/inbox/raw/<slug>/
	entries, err := os.ReadDir(filepath.Join(wiki, "team", "inbox", "raw"))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one source-slug dir, got %d", len(entries))
	}
}

func TestScannerScanIsIdempotent(t *testing.T) {
	root := setupRoot(t, map[string]string{"note.md": "hi"})
	wiki := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	commitCalls := 0
	commit := func(_ context.Context, _, _ string) (string, error) {
		commitCalls++
		return "sha", nil
	}
	// First scan ingests one file.
	res, _, err := Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, wiki, commit)
	if err != nil || res.Ingested != 1 {
		t.Fatalf("first scan: %v %+v", err, res)
	}
	// Reload detector from disk and re-scan — nothing should change.
	det2, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector reload: %v", err)
	}
	res2, _, err := Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det2, wiki, commit)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if res2.Ingested != 0 {
		t.Fatalf("expected 0 ingested on re-scan, got %+v", res2)
	}
	if commitCalls != 1 {
		t.Fatalf("expected commit only once across both scans, got %d", commitCalls)
	}
}

func TestScannerScanSkipsSecretsFile(t *testing.T) {
	// A file with > 3 secret matches is skipped wholesale.
	body := strings.Join([]string{
		"sk-" + strings.Repeat("A", 30),
		"AKIAIOSFODNN7EXAMPLE",
		"Bearer abc.def-012345",
		"OPENAI_API_KEY=sk-xyz",
		"ghp_" + strings.Repeat("b", 36),
	}, "\n")
	root := setupRoot(t, map[string]string{
		"clean.md":   "no secrets here",
		"secrets.md": body,
	})
	wiki := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	commit := func(_ context.Context, _, _ string) (string, error) { return "sha", nil }
	res, _, err := Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, wiki, commit)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	// clean.md ingested, secrets.md skipped.
	if res.Ingested != 1 {
		t.Fatalf("expected 1 ingested, got %+v", res)
	}
	if res.Skipped < 1 {
		t.Fatalf("expected at least 1 skipped, got %+v", res)
	}
}

func TestScannerScanRedactsSecretsInIngestedFile(t *testing.T) {
	root := setupRoot(t, map[string]string{
		"mixed.md": "ok\nAKIAIOSFODNN7EXAMPLE\nrest",
	})
	wiki := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	commit := func(_ context.Context, _, _ string) (string, error) { return "sha", nil }
	res, _, err := Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, wiki, commit)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Ingested != 1 || res.Redacted != 1 {
		t.Fatalf("expected ingested+redacted=1, got %+v", res)
	}
	ingested := res.IngestedPath[0]
	body, err := os.ReadFile(ingested)
	if err != nil {
		t.Fatalf("read ingested: %v", err)
	}
	if !strings.Contains(string(body), "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in ingested body, got %q", body)
	}
	if strings.Contains(string(body), "AKIA") {
		t.Fatalf("original AWS key leaked through: %q", body)
	}
}

func TestScannerScanEnforcesSizeCaps(t *testing.T) {
	// Per-file cap: create a file slightly over 10MB.
	root := t.TempDir()
	big := strings.Repeat("a", maxScanFileBytes+1)
	if err := os.WriteFile(filepath.Join(root, "big.md"), []byte(big), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	_, _, err = Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, t.TempDir(), nil)
	if !errors.Is(err, ErrScanFileTooLarge) {
		t.Fatalf("want ErrScanFileTooLarge, got %v", err)
	}
}

func TestScannerScanEnforcesFileCountCap(t *testing.T) {
	// Create > 1000 tiny files — quickly.
	root := t.TempDir()
	for i := 0; i <= maxScanFiles; i++ {
		p := filepath.Join(root, fmt.Sprintf("n%04d.md", i))
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	_, _, err = Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, t.TempDir(), nil)
	if !errors.Is(err, ErrScanFileCountExceeded) {
		t.Fatalf("want ErrScanFileCountExceeded, got %v", err)
	}
}

func TestScannerScanEnforcesTotalSizeCap(t *testing.T) {
	// Each file is 9MB, > 12 files pushes total over 100MB. Keep it just
	// above the cap — too far above is slow.
	root := t.TempDir()
	per := 9 * 1024 * 1024
	body := strings.Repeat("a", per)
	needed := (maxScanTotalBytes / per) + 2
	for i := 0; i < needed; i++ {
		p := filepath.Join(root, fmt.Sprintf("f%02d.md", i))
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	_, _, err = Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, t.TempDir(), nil)
	if !errors.Is(err, ErrScanTotalTooLarge) {
		t.Fatalf("want ErrScanTotalTooLarge, got %v", err)
	}
}

func TestScannerScanSkipsEmptyFiles(t *testing.T) {
	root := setupRoot(t, map[string]string{
		"empty.md":   "",
		"content.md": "real",
	})
	wiki := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	commit := func(_ context.Context, _, _ string) (string, error) { return "sha", nil }
	res, _, err := Scan(context.Background(), ScanOptions{Root: root, Confirm: true}, det, wiki, commit)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Ingested != 1 {
		t.Fatalf("expected 1 ingested (empty skipped), got %+v", res)
	}
}

func TestScannerScanRootMustBeDirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	_, _, err = Scan(context.Background(), ScanOptions{Root: f, Confirm: true}, det, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("want not-a-dir error, got %v", err)
	}
}

func TestScannerSafeFilename(t *testing.T) {
	cases := map[string]string{
		"a.md":           "a.md",
		"sub/b.md":       "sub__b.md",
		"a/b/c.txt":      "a__b__c.txt.md",
		"notes.org":      "notes.org.md",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScannerHasRootMatchesSubpaths(t *testing.T) {
	m := emptyManifest()
	m.Files["/users/x/notes/a.md"] = ScanManifestEntry{Mtime: 1, Size: 1}
	if !m.HasRoot("/users/x/notes") {
		t.Fatalf("HasRoot should match parent of an indexed file")
	}
	if m.HasRoot("/users/y") {
		t.Fatalf("HasRoot should not match unrelated roots")
	}
}

// Verify the mtime-based detector treats a touched file as changed.
func TestScannerMtimeDetectsModification(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	det, err := NewMtimeChangeDetector()
	if err != nil {
		t.Fatalf("detector: %v", err)
	}
	root := t.TempDir()
	p := filepath.Join(root, "a.md")
	if err := os.WriteFile(p, []byte("v1"), 0o600); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	info, _ := os.Stat(p)
	if !det.IsChanged(p, info) {
		t.Fatalf("new file should register as changed")
	}
	det.MarkIngested(p, info, "test")
	info2, _ := os.Stat(p)
	if det.IsChanged(p, info2) {
		t.Fatalf("same file should not be changed after ingest")
	}
	// Touch the file — future mtime.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	info3, _ := os.Stat(p)
	if !det.IsChanged(p, info3) {
		t.Fatalf("touched file should register as changed")
	}
}
