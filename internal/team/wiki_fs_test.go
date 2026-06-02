package team

// Tests for the cabinet-style wiki file surface: GET /wiki/tree and
// GET /wiki/file. Covers tree classification (dir / page / file / app /
// website, hidden-skipped, sorted), title extraction precedence, and file
// serving (MIME, body bytes, Range → 206, traversal/absolute → 400,
// missing → 404). Reuses the package-level httptest helpers (get / readBody)
// from wiki_e2e_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newWikiFSTestServer spins up an httptest server wired to the two new fs
// handlers, backed by a fresh temp-dir Repo. It returns the base URL, the
// repo (so tests can seed files under team/), and a cleanup func.
func newWikiFSTestServer(t *testing.T) (baseURL string, repo *Repo, cleanup func()) {
	t.Helper()

	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo = NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	worker := NewWikiWorker(repo, &capturePublisher{events: make(chan wikiWriteEvent, 4)})
	broker := &Broker{wikiWorker: worker}

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/tree", broker.handleWikiTree)
	mux.HandleFunc("/wiki/file", broker.handleWikiFile)

	srv := httptest.NewServer(mux)
	return srv.URL, repo, func() { srv.Close() }
}

// seedFile writes content to <teamDir>/<rel>, creating parent dirs.
func seedFile(t *testing.T, repo *Repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo.TeamDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// fetchTree GETs /wiki/tree (optionally scoped to subPath) and decodes nodes.
func fetchTree(t *testing.T, baseURL, subPath string) []TreeNode {
	t.Helper()
	url := baseURL + "/wiki/tree"
	if subPath != "" {
		url += "?path=" + subPath
	}
	resp := get(t, url)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	var decoded struct {
		Nodes []TreeNode `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode tree: %v", err)
	}
	return decoded.Nodes
}

// findNode returns the node with the given name from a slice, or nil.
func findNode(nodes []TreeNode, name string) *TreeNode {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

func TestWikiTreeClassification(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	// A markdown page with an H1 title.
	seedFile(t, repo, "people/nazz.md", "# Nazz Mohammad\n\nFounder.\n")
	// A markdown page whose title comes from frontmatter.
	seedFile(t, repo, "people/sarah.md", "---\ntitle: Sarah Chen\n---\n\nBody.\n")
	// A markdown page with neither H1 nor frontmatter title → humanized base.
	seedFile(t, repo, "people/customer-success.md", "Just a body, no heading.\n")
	// A raw file (png) → "file" with ext.
	seedFile(t, repo, "assets/logo.png", "\x89PNG\r\n\x1a\n")
	// A website dir: index.html, no index.md, no .app marker.
	seedFile(t, repo, "site/index.html", "<!doctype html><title>Site</title>")
	seedFile(t, repo, "site/style.css", "body{}")
	// An app dir: index.html + .app marker, no index.md.
	seedFile(t, repo, "dash/index.html", "<!doctype html><title>Dash</title>")
	seedFile(t, repo, "dash/.app", "")
	// A dir that has BOTH index.html and index.md → plain dir, recurses.
	seedFile(t, repo, "guide/index.html", "<!doctype html>")
	seedFile(t, repo, "guide/index.md", "# Guide\n")
	seedFile(t, repo, "guide/intro.md", "# Intro\n")
	// Hidden entries that must be skipped.
	seedFile(t, repo, "people/.secret.md", "# Secret\n")
	if err := os.MkdirAll(filepath.Join(repo.TeamDir(), ".git-shadow"), 0o755); err != nil {
		t.Fatalf("mkdir hidden dir: %v", err)
	}

	nodes := fetchTree(t, baseURL, "")

	// Top level should contain dirs (assets, dash, guide, people, site) but
	// NOT the .gitkeep stubs or hidden entries. Init also seeds the default
	// layout dirs (companies, projects, ...), so just assert on what we added.
	people := findNode(nodes, "people")
	if people == nil {
		t.Fatal("missing 'people' dir node")
	}
	if people.Type != treeTypeDir {
		t.Errorf("people.Type = %q, want dir", people.Type)
	}

	// people children: nazz (H1), sarah (frontmatter), customer-success
	// (humanized base). Hidden .secret.md must be absent.
	nazz := findNode(people.Children, "nazz.md")
	if nazz == nil {
		t.Fatal("missing nazz.md page")
	}
	if nazz.Type != treeTypePage {
		t.Errorf("nazz.Type = %q, want page", nazz.Type)
	}
	if nazz.Path != "team/people/nazz.md" {
		t.Errorf("nazz.Path = %q, want team/people/nazz.md", nazz.Path)
	}
	if nazz.Title != "Nazz Mohammad" {
		t.Errorf("nazz.Title = %q, want 'Nazz Mohammad' (first H1)", nazz.Title)
	}
	if sarah := findNode(people.Children, "sarah.md"); sarah == nil || sarah.Title != "Sarah Chen" {
		t.Errorf("sarah title = %v, want frontmatter 'Sarah Chen'", sarah)
	}
	if cs := findNode(people.Children, "customer-success.md"); cs == nil || cs.Title != "Customer Success" {
		t.Errorf("customer-success title = %v, want humanized 'Customer Success'", cs)
	}
	if hidden := findNode(people.Children, ".secret.md"); hidden != nil {
		t.Error(".secret.md should be skipped (hidden)")
	}

	// website classification.
	site := findNode(nodes, "site")
	if site == nil || site.Type != treeTypeWebsite {
		t.Errorf("site node = %v, want type website", site)
	}
	if site != nil && len(site.Children) != 0 {
		t.Errorf("website node must be a leaf; got %d children", len(site.Children))
	}

	// app classification (.app marker present).
	dash := findNode(nodes, "dash")
	if dash == nil || dash.Type != treeTypeApp {
		t.Errorf("dash node = %v, want type app", dash)
	}
	if dash != nil && len(dash.Children) != 0 {
		t.Errorf("app node must be a leaf; got %d children", len(dash.Children))
	}

	// guide has both index.html and index.md → plain dir with children.
	guide := findNode(nodes, "guide")
	if guide == nil || guide.Type != treeTypeDir {
		t.Errorf("guide node = %v, want type dir (has index.md)", guide)
	}
	if guide != nil {
		if findNode(guide.Children, "intro.md") == nil {
			t.Error("guide should recurse and include intro.md")
		}
	}

	// raw file classification + ext.
	assets := findNode(nodes, "assets")
	if assets == nil {
		t.Fatal("missing assets dir")
	}
	logo := findNode(assets.Children, "logo.png")
	if logo == nil {
		t.Fatal("missing logo.png file node")
	}
	if logo.Type != treeTypeFile {
		t.Errorf("logo.Type = %q, want file", logo.Type)
	}
	if logo.Ext != ".png" {
		t.Errorf("logo.Ext = %q, want .png", logo.Ext)
	}
	if logo.Path != "team/assets/logo.png" {
		t.Errorf("logo.Path = %q, want team/assets/logo.png", logo.Path)
	}
}

func TestWikiTreeSortOrder(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	// Scope to a single subtree so Init's default layout dirs don't pollute
	// the ordering assertion.
	seedFile(t, repo, "sorted/zeta.md", "# Zeta\n")      // page
	seedFile(t, repo, "sorted/alpha.md", "# Alpha\n")    // page
	seedFile(t, repo, "sorted/notes.txt", "hi\n")        // file
	seedFile(t, repo, "sorted/data.csv", "a,b\n")        // file
	seedFile(t, repo, "sorted/zsub/keep.md", "# Keep\n") // dir (zsub)
	seedFile(t, repo, "sorted/asub/keep.md", "# Keep\n") // dir (asub)

	nodes := fetchTree(t, baseURL, "team/sorted")

	var order []string
	for _, n := range nodes {
		order = append(order, n.Name)
	}
	want := []string{"asub", "zsub", "alpha.md", "zeta.md", "data.csv", "notes.txt"}
	if len(order) != len(want) {
		t.Fatalf("got %d nodes %v, want %d %v", len(order), order, len(want), want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("sort order[%d] = %q, want %q (full: %v)", i, order[i], want[i], order)
		}
	}
}

func TestWikiTreeSubPathTraversalRejected(t *testing.T) {
	baseURL, _, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	resp := get(t, baseURL+"/wiki/tree?path="+"../../etc")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("tree with traversal path: status %d, want 400", resp.StatusCode)
	}
}

func TestWikiFileServesBytesAndMIME(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	const body = "# Hello\n\nworld\n"
	seedFile(t, repo, "people/nazz.md", body)

	resp := get(t, baseURL+"/wiki/file?path=team/people/nazz.md")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET file: status %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/markdown; charset=utf-8", ct)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=300" {
		t.Errorf("Cache-Control = %q, want private, max-age=300", got)
	}
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if got := readBody(t, resp); got != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestWikiFileHTMLCacheControl(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	seedFile(t, repo, "site/index.html", "<!doctype html><title>Site</title>")

	resp := get(t, baseURL+"/wiki/file?path=team/site/index.html")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET html: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache, must-revalidate" {
		t.Errorf("Cache-Control = %q, want no-cache, must-revalidate", cc)
	}
}

func TestWikiFileRangeRequest(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	const body = "0123456789ABCDEF"
	seedFile(t, repo, "assets/clip.mp4", body)

	req, err := http.NewRequest(http.MethodGet, baseURL+"/wiki/file?path=team/assets/clip.mp4", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Range", "bytes=4-7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Range request: status %d, want 206", resp.StatusCode)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "bytes 4-7/16" {
		t.Errorf("Content-Range = %q, want bytes 4-7/16", cr)
	}
	if got := readBody(t, resp); got != "4567" {
		t.Errorf("range body = %q, want 4567", got)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("Content-Type = %q, want video/mp4", ct)
	}
}

func TestWikiFileUnknownExtFallback(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	seedFile(t, repo, "blobs/thing.bin", "\x00\x01\x02\x03")

	resp := get(t, baseURL+"/wiki/file?path=team/blobs/thing.bin")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET bin: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
}

func TestWikiFileSecurityRejections(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	// Create a real secret file OUTSIDE team/ (under repo root) to prove the
	// traversal target exists yet is unreachable.
	if err := os.WriteFile(filepath.Join(repo.Root(), "secret.txt"), []byte("top secret"), 0o644); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	// And a sibling dir that shares the "team" prefix to prove the
	// separator-aware containment check rejects prefix-confusion.
	if err := os.MkdirAll(filepath.Join(repo.Root(), "team-secrets"), 0o755); err != nil {
		t.Fatalf("mkdir team-secrets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo.Root(), "team-secrets", "x.md"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("seed team-secrets file: %v", err)
	}

	cases := []struct {
		name string
		path string
		want int
	}{
		{"empty", "", http.StatusBadRequest},
		{"traversal", "team/../secret.txt", http.StatusBadRequest},
		{"deep traversal", "team/../../etc/passwd", http.StatusBadRequest},
		{"absolute", "/etc/passwd", http.StatusBadRequest},
		{"outside team root", "secret.txt", http.StatusBadRequest},
		{"prefix confusion", "team-secrets/x.md", http.StatusBadRequest},
		{"missing", "team/people/ghost.md", http.StatusNotFound},
		{"directory", "team/people", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// people/ needs to exist for the "directory" case; Init seeds it.
			url := baseURL + "/wiki/file?path=" + tc.path
			resp := get(t, url)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				t.Errorf("path %q: status %d, want %d (body: %s)", tc.path, resp.StatusCode, tc.want, readBody(t, resp))
			}
		})
	}
}

func TestWikiFileRejectsSymlink(t *testing.T) {
	baseURL, repo, cleanup := newWikiFSTestServer(t)
	defer cleanup()

	// Seed a real secret file OUTSIDE team/ (under repo root). resolveTeamRelPath
	// confines the *path* to team/, but os.Open would follow a symlink whose
	// target escapes the tree — this proves the Lstat reject closes that hole.
	const secret = "TOP SECRET PAYLOAD that must never be served"
	target := filepath.Join(repo.Root(), "secret-target.txt")
	if err := os.WriteFile(target, []byte(secret), 0o644); err != nil {
		t.Fatalf("seed secret target: %v", err)
	}

	// Create a symlink at team/escape.txt pointing to the out-of-tree target.
	link := filepath.Join(repo.TeamDir(), "escape.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unsupported on this platform: %v", err)
	}

	resp := get(t, baseURL+"/wiki/file?path=team/escape.txt")
	defer func() { _ = resp.Body.Close() }()

	// Must be rejected (404 hides that it is a symlink; 400 also acceptable).
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("symlink GET: status %d, want 404 or 400", resp.StatusCode)
	}
	if body := readBody(t, resp); body == secret {
		t.Fatalf("symlink GET leaked target contents: %q", body)
	}
}

func TestResolveTeamRelPathContainment(t *testing.T) {
	repoRoot := filepath.Join(string(os.PathSeparator)+"root", ".wuphf", "wiki")
	teamDir := filepath.Join(repoRoot, "team")

	bad := []string{
		"",
		"   ",
		"/etc/passwd",
		"team/../secret.txt",
		"team/../../etc/passwd",
		"..",
		"../team/x.md",
		"secret.txt",     // no team/ prefix
		"team-secrets/x", // prefix confusion
		"team/x\x00.md",  // NUL byte
		"team/x\nfoo.md", // control byte
	}
	for _, p := range bad {
		if _, _, err := resolveTeamRelPath(repoRoot, p); err == nil {
			t.Errorf("resolveTeamRelPath(%q) = nil error, want rejection", p)
		}
	}

	good := map[string]string{
		"team":               "team",
		"team/people":        "team/people",
		"team/people/x.md":   "team/people/x.md",
		"team/./people/x.md": "team/people/x.md",
	}
	for in, wantClean := range good {
		gotClean, gotAbs, err := resolveTeamRelPath(repoRoot, in)
		if err != nil {
			t.Errorf("resolveTeamRelPath(%q) unexpected error: %v", in, err)
			continue
		}
		if gotClean != wantClean {
			t.Errorf("resolveTeamRelPath(%q) clean = %q, want %q", in, gotClean, wantClean)
		}
		if !isPathWithin(teamDir, gotAbs) {
			t.Errorf("resolveTeamRelPath(%q) abs %q not within %q", in, gotAbs, teamDir)
		}
	}
}
