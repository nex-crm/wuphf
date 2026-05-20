package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func obsidianPaths(repo *Repo) (appJSON, gitignore string) {
	dir := filepath.Join(repo.Root(), "team", ".obsidian")
	return filepath.Join(dir, "app.json"), filepath.Join(dir, ".gitignore")
}

func readAppJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read app.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse app.json: %v\n%s", err, data)
	}
	return m
}

func TestObsidianBootstrap_EmptyTeam(t *testing.T) {
	repo := newTestRepo(t)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	appPath, ignorePath := obsidianPaths(repo)
	app := readAppJSON(t, appPath)

	if got, want := app["useMarkdownLinks"], false; got != want {
		t.Errorf("useMarkdownLinks = %v, want %v", got, want)
	}
	if got, want := app["newLinkFormat"], "absolute"; got != want {
		t.Errorf("newLinkFormat = %v, want %v", got, want)
	}
	if got, want := app["alwaysUpdateLinks"], false; got != want {
		t.Errorf("alwaysUpdateLinks = %v, want %v", got, want)
	}
	if got, want := app["attachmentFolderPath"], "inbox/raw"; got != want {
		t.Errorf("attachmentFolderPath = %v, want %v", got, want)
	}
	filters, ok := app["userIgnoreFilters"].([]any)
	if !ok {
		t.Fatalf("userIgnoreFilters not array: %#v", app["userIgnoreFilters"])
	}
	wantFilters := []string{"playbooks/.compiled/", "entities/.graph.jsonl"}
	if len(filters) != len(wantFilters) {
		t.Fatalf("userIgnoreFilters len = %d, want %d", len(filters), len(wantFilters))
	}
	for i, want := range wantFilters {
		if filters[i] != want {
			t.Errorf("userIgnoreFilters[%d] = %v, want %v", i, filters[i], want)
		}
	}

	ignore, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, want := range []string{"workspace.json", "workspace-mobile.json", "graph.json"} {
		if !strings.Contains(string(ignore), want) {
			t.Errorf(".gitignore missing %q\ncontent:\n%s", want, ignore)
		}
	}
}

func TestObsidianBootstrap_Idempotent(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	appPath, ignorePath := obsidianPaths(repo)
	app1, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read app.json: %v", err)
	}
	ignore1, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	// Re-run via the same path the broker takes.
	repo.mu.Lock()
	if err := repo.ensureLayoutLocked(); err != nil {
		repo.mu.Unlock()
		t.Fatalf("ensureLayoutLocked: %v", err)
	}
	repo.mu.Unlock()

	app2, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read app.json after re-run: %v", err)
	}
	ignore2, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .gitignore after re-run: %v", err)
	}
	if string(app1) != string(app2) {
		t.Errorf("app.json drifted on re-run\nbefore:\n%s\nafter:\n%s", app1, app2)
	}
	if string(ignore1) != string(ignore2) {
		t.Errorf(".gitignore drifted on re-run\nbefore:\n%s\nafter:\n%s", ignore1, ignore2)
	}
}

func TestObsidianBootstrap_MergePreservesUserKeys(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	appPath, _ := obsidianPaths(repo)
	seed := []byte(`{"theme":"obsidian","useMarkdownLinks":true,"customField":42}`)
	if err := os.WriteFile(appPath, seed, 0o600); err != nil {
		t.Fatalf("seed app.json: %v", err)
	}

	repo.mu.Lock()
	if err := repo.ensureLayoutLocked(); err != nil {
		repo.mu.Unlock()
		t.Fatalf("ensureLayoutLocked: %v", err)
	}
	repo.mu.Unlock()

	app := readAppJSON(t, appPath)
	if got := app["theme"]; got != "obsidian" {
		t.Errorf("theme = %v, want %q (user value lost)", got, "obsidian")
	}
	// JSON numbers decode as float64 in map[string]any.
	if got, ok := app["customField"].(float64); !ok || got != 42 {
		t.Errorf("customField = %v (%T), want 42", app["customField"], app["customField"])
	}
	if got := app["useMarkdownLinks"]; got != false {
		t.Errorf("useMarkdownLinks = %v, want false (WUPHF must override)", got)
	}
}

func TestObsidianBootstrap_RequiredKeyOverride(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	appPath, _ := obsidianPaths(repo)
	seed := []byte(`{"newLinkFormat":"shortest"}`)
	if err := os.WriteFile(appPath, seed, 0o600); err != nil {
		t.Fatalf("seed app.json: %v", err)
	}

	repo.mu.Lock()
	if err := repo.ensureLayoutLocked(); err != nil {
		repo.mu.Unlock()
		t.Fatalf("ensureLayoutLocked: %v", err)
	}
	repo.mu.Unlock()

	app := readAppJSON(t, appPath)
	if got := app["newLinkFormat"]; got != "absolute" {
		t.Errorf("newLinkFormat = %v, want %q (WUPHF must override)", got, "absolute")
	}
}

func TestObsidianBootstrap_GitignoreLeftAloneWhenComplete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, ignorePath := obsidianPaths(repo)
	// Pre-seed with all required entries plus a user comment, ordered
	// non-canonically, to prove byte-for-byte preservation.
	seed := "# my notes\ngraph.json\nworkspace.json\nworkspace-mobile.json\n"
	if err := os.WriteFile(ignorePath, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	repo.mu.Lock()
	if err := repo.ensureLayoutLocked(); err != nil {
		repo.mu.Unlock()
		t.Fatalf("ensureLayoutLocked: %v", err)
	}
	repo.mu.Unlock()

	got, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(got) != seed {
		t.Errorf(".gitignore was rewritten\nwant:\n%q\ngot:\n%q", seed, got)
	}
}

func TestObsidianBootstrap_GitignoreAppendsMissing(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, ignorePath := obsidianPaths(repo)
	// User has only one of the three required entries plus a comment.
	seed := "# user file\nworkspace.json\n"
	if err := os.WriteFile(ignorePath, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	repo.mu.Lock()
	if err := repo.ensureLayoutLocked(); err != nil {
		repo.mu.Unlock()
		t.Fatalf("ensureLayoutLocked: %v", err)
	}
	repo.mu.Unlock()

	got, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	gotStr := string(got)
	// Pre-existing content must remain.
	if !strings.HasPrefix(gotStr, seed) {
		t.Errorf("expected output to begin with seed\nseed:\n%q\ngot:\n%q", seed, gotStr)
	}
	for _, want := range []string{"workspace.json", "workspace-mobile.json", "graph.json"} {
		if !strings.Contains(gotStr, want) {
			t.Errorf(".gitignore missing %q after merge\ncontent:\n%s", want, gotStr)
		}
	}
	// Set semantics: pre-existing entry must not be duplicated.
	if n := strings.Count(gotStr, "workspace.json\n"); n != 1 {
		// Note: "workspace.json" is a substring of "workspace-mobile.json"
		// when the latter has no path component — but our token is the full
		// line, so count the exact line form.
		t.Errorf("workspace.json appears %d times; want 1\ncontent:\n%s", n, gotStr)
	}
}

func TestObsidianBootstrap_CorruptAppJSONErrors(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	appPath, _ := obsidianPaths(repo)
	if err := os.WriteFile(appPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed corrupt app.json: %v", err)
	}

	repo.mu.Lock()
	err := repo.ensureLayoutLocked()
	repo.mu.Unlock()
	if err == nil {
		t.Fatalf("expected error on corrupt app.json; got nil")
	}
}
