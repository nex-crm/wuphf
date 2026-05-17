package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type richArtifactTutorialManifest struct {
	Version   int                            `json:"version"`
	Scenarios []richArtifactTutorialScenario `json:"scenarios"`
}

type richArtifactTutorialScenario struct {
	Slug                   string   `json:"slug"`
	Title                  string   `json:"title"`
	Summary                string   `json:"summary"`
	ActorSlug              string   `json:"actorSlug"`
	SourceMarkdownPath     string   `json:"sourceMarkdownPath"`
	TargetWikiPath         string   `json:"targetWikiPath"`
	HTMLPath               string   `json:"htmlPath"`
	SourcePath             string   `json:"sourcePath"`
	WikiPath               string   `json:"wikiPath"`
	ChatPath               string   `json:"chatPath"`
	ExpectedChatArtifactID string   `json:"expectedChatArtifactId"`
	ExpectedTerms          []string `json:"expectedTerms"`
}

func TestRichArtifactTutorialFixturesExerciseWikiFlow(t *testing.T) {
	repoRoot := findTutorialRepoRoot(t)
	manifest := readRichArtifactTutorialManifest(t, repoRoot)
	if manifest.Version != 1 {
		t.Fatalf("manifest version = %d, want 1", manifest.Version)
	}
	if len(manifest.Scenarios) < 3 {
		t.Fatalf("expected at least 3 tutorial scenarios, got %d", len(manifest.Scenarios))
	}

	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init repo: %v", err)
	}

	baseTime := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	for i, scenario := range manifest.Scenarios {
		scenario := scenario
		now := baseTime.Add(time.Duration(i) * time.Minute)
		t.Run(scenario.Slug, func(t *testing.T) {
			source := readRichArtifactTutorialFile(t, repoRoot, scenario.SourcePath)
			wiki := readRichArtifactTutorialFile(t, repoRoot, scenario.WikiPath)
			html := readRichArtifactTutorialFile(t, repoRoot, scenario.HTMLPath)

			if !strings.Contains(html, "<script>") {
				t.Fatalf("%s: expected interactive HTML with a script tag", scenario.Slug)
			}
			for _, term := range scenario.ExpectedTerms {
				if !strings.Contains(html, term) {
					t.Fatalf("%s: html missing expected term %q", scenario.Slug, term)
				}
			}

			if _, _, err := repo.CommitNotebook(ctx, scenario.ActorSlug, scenario.SourceMarkdownPath, source, "create", "tutorial: capture source "+scenario.Slug); err != nil {
				t.Fatalf("commit notebook source: %v", err)
			}

			artifact, sanitizedHTML, err := newRichArtifact(RichArtifactCreateRequest{
				Slug:               scenario.ActorSlug,
				Title:              scenario.Title,
				Summary:            scenario.Summary,
				HTML:               html,
				SourceMarkdownPath: scenario.SourceMarkdownPath,
				RelatedTaskID:      "tutorial-" + scenario.Slug,
				RelatedReceiptIDs:  []string{"receipt-" + scenario.Slug},
			}, now)
			if err != nil {
				t.Fatalf("new rich artifact: %v", err)
			}
			if sanitizedHTML != html {
				t.Fatal("newRichArtifact changed fixture HTML")
			}

			if _, bytesWritten, err := repo.CommitRichArtifact(ctx, scenario.ActorSlug, artifact, sanitizedHTML, "artifact: create tutorial "+scenario.Slug); err != nil {
				t.Fatalf("commit rich artifact: %v", err)
			} else if bytesWritten == 0 {
				t.Fatal("expected artifact bytes to be written")
			}

			promoted, _, _, err := repo.PromoteRichArtifact(ctx, scenario.ActorSlug, artifact.ID, scenario.TargetWikiPath, wiki, "create", "wiki: promote tutorial "+scenario.Slug, now.Add(30*time.Second))
			if err != nil {
				t.Fatalf("promote rich artifact: %v", err)
			}
			if promoted.Kind != richArtifactKindWikiVisual {
				t.Fatalf("promoted kind = %q, want %q", promoted.Kind, richArtifactKindWikiVisual)
			}
			if promoted.TrustLevel != richArtifactTrustPromoted {
				t.Fatalf("promoted trust = %q, want %q", promoted.TrustLevel, richArtifactTrustPromoted)
			}
			if promoted.PromotedWikiPath != scenario.TargetWikiPath {
				t.Fatalf("promoted path = %q, want %q", promoted.PromotedWikiPath, scenario.TargetWikiPath)
			}

			got, gotHTML, err := repo.RichArtifact(artifact.ID)
			if err != nil {
				t.Fatalf("read rich artifact: %v", err)
			}
			if got.ID != promoted.ID || got.SourceMarkdownPath != scenario.SourceMarkdownPath {
				t.Fatalf("read artifact provenance = (%q, %q), want (%q, %q)", got.ID, got.SourceMarkdownPath, promoted.ID, scenario.SourceMarkdownPath)
			}
			if gotHTML != html {
				t.Fatal("persisted HTML did not round trip")
			}

			wikiOnDisk, err := os.ReadFile(filepath.Join(repo.Root(), filepath.FromSlash(scenario.TargetWikiPath)))
			if err != nil {
				t.Fatalf("read promoted wiki article: %v", err)
			}
			wikiText := string(wikiOnDisk)
			for _, want := range []string{"Visual Artifact Provenance", artifact.ID, artifact.HTMLPath, scenario.SourceMarkdownPath} {
				if !strings.Contains(wikiText, want) {
					t.Fatalf("promoted wiki article missing %q", want)
				}
			}

			listed, err := repo.ListRichArtifacts(RichArtifactFilter{SourceMarkdownPath: scenario.SourceMarkdownPath})
			if err != nil {
				t.Fatalf("list rich artifacts: %v", err)
			}
			if len(listed) != 1 || listed[0].ID != artifact.ID {
				t.Fatalf("listed artifacts = %+v, want only %s", listed, artifact.ID)
			}
		})
	}
}

func readRichArtifactTutorialManifest(t *testing.T, repoRoot string) richArtifactTutorialManifest {
	t.Helper()
	raw := readRichArtifactTutorialFile(t, repoRoot, "scenarios.json")
	var manifest richArtifactTutorialManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("decode tutorial manifest: %v", err)
	}
	return manifest
}

func readRichArtifactTutorialFile(t *testing.T, repoRoot, relPath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "tutorials", "rich-html-artifacts", filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read tutorial fixture %s: %v", relPath, err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		t.Fatalf("tutorial fixture %s is empty", relPath)
	}
	return string(raw)
}

func findTutorialRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root with go.mod")
		}
		dir = parent
	}
}
