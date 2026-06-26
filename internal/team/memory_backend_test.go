package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/gbrain"
)

// fakeGBrainClient is an in-memory gbrainMemoryClient for exercising the
// gbrain memory backend without a live gbrain subprocess.
type fakeGBrainClient struct {
	queryResults []gbrain.SearchResult
	queryErr     error
	lastQuery    string
	lastLimit    int

	putResult  gbrain.PutResult
	putErr     error
	putCalls   int
	putContent string
	putOptions gbrain.PutOptions
}

func (f *fakeGBrainClient) Query(_ context.Context, query string, limit int) ([]gbrain.SearchResult, error) {
	f.lastQuery = query
	f.lastLimit = limit
	return f.queryResults, f.queryErr
}

func (f *fakeGBrainClient) PutPage(_ context.Context, content string, opts gbrain.PutOptions) (gbrain.PutResult, error) {
	f.putCalls++
	f.putContent = content
	f.putOptions = opts
	return f.putResult, f.putErr
}

func TestGBrainBackendQuerySharedMapsInjectedHits(t *testing.T) {
	fake := &fakeGBrainClient{queryResults: []gbrain.SearchResult{{
		Slug:        "people/nazz",
		PageID:      42,
		Title:       "Nazz",
		ChunkText:   "Relevant compiled truth",
		ChunkSource: "compiled_truth",
		ChunkID:     7,
		ChunkIndex:  3,
		Score:       0.91,
		Stale:       true,
	}}}
	backend := gbrainMemoryBackend{client: fake}

	hits, err := backend.QueryShared(context.Background(), "who is nazz", 3)
	if err != nil {
		t.Fatalf("QueryShared: %v", err)
	}
	if fake.lastQuery != "who is nazz" || fake.lastLimit != 3 {
		t.Fatalf("client not called with query/limit, got %q/%d", fake.lastQuery, fake.lastLimit)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	hit := hits[0]
	if hit.Backend != config.MemoryBackendGBrain || hit.Slug != "people/nazz" || hit.PageID != 42 ||
		hit.ChunkID != 7 || hit.ChunkIndex != 3 || hit.Source != "compiled_truth" {
		t.Fatalf("citation fields not mapped: %+v", hit)
	}
	if hit.Score == nil || *hit.Score != 0.91 || hit.Stale == nil || !*hit.Stale {
		t.Fatalf("score/stale not mapped: %+v", hit)
	}
}

func TestGBrainBackendFetchBriefUsesInjectedClient(t *testing.T) {
	fake := &fakeGBrainClient{queryResults: []gbrain.SearchResult{{
		Slug:      "launch",
		Title:     "Launch brief",
		Type:      "note",
		ChunkText: "We ship the pilot on Friday.",
	}}}
	backend := gbrainMemoryBackend{client: fake}

	brief := backend.FetchBrief(context.Background(), "what is the launch plan")
	if !strings.Contains(brief, "GBRAIN CONTEXT") || !strings.Contains(brief, "Launch brief") {
		t.Fatalf("brief did not render injected context: %q", brief)
	}
	if fake.lastQuery != "what is the launch plan" {
		t.Fatalf("FetchBrief did not query the client, got %q", fake.lastQuery)
	}
}

func TestGBrainBackendWriteSharedSendsSlugAndContent(t *testing.T) {
	fake := &fakeGBrainClient{putResult: gbrain.PutResult{Slug: "ignored", Status: "ok"}}
	backend := gbrainMemoryBackend{client: fake}

	slug, err := backend.WriteShared(context.Background(), SharedMemoryWrite{
		Actor:   "pm",
		Title:   "Launch decision",
		Content: "We shipped the pilot.",
	})
	if err != nil {
		t.Fatalf("WriteShared: %v", err)
	}
	if !strings.HasPrefix(slug, "wuphf-shared--pm--") {
		t.Fatalf("unexpected slug %q", slug)
	}
	if fake.putCalls != 1 {
		t.Fatalf("expected 1 PutPage call, got %d", fake.putCalls)
	}
	if strings.TrimSpace(fake.putOptions.Slug) == "" || fake.putOptions.Slug != slug {
		t.Fatalf("PutPage slug mismatch: opt=%q returned=%q", fake.putOptions.Slug, slug)
	}
	if fake.putOptions.SourceKind != gbrainSharedMemorySourceKind {
		t.Fatalf("expected source_kind %q, got %q", gbrainSharedMemorySourceKind, fake.putOptions.SourceKind)
	}
	if !strings.Contains(fake.putContent, "We shipped the pilot.") || !strings.Contains(fake.putContent, "Launch decision") {
		t.Fatalf("assembled content missing note body/title: %q", fake.putContent)
	}
}

func TestGBrainBackendWriteSharedWrapsClientError(t *testing.T) {
	fake := &fakeGBrainClient{putErr: errors.New("boom")}
	backend := gbrainMemoryBackend{client: fake}

	slug, err := backend.WriteShared(context.Background(), SharedMemoryWrite{Content: "note"})
	if err == nil {
		t.Fatal("expected error from WriteShared")
	}
	if !errors.Is(err, fake.putErr) {
		t.Fatalf("error not wrapped: %v", err)
	}
	if slug != "" {
		t.Fatalf("expected empty slug on error, got %q", slug)
	}
}

func TestResolveMemoryBackendStatusNoNexFallsBackToMarkdown(t *testing.T) {
	// --no-nex with no explicit backend pick lands the user on the markdown
	// wiki — the default external memory that doesn't require Nex. Silent
	// 'none' was a worse default: the user asked to skip Nex, not to lose
	// all shared memory.
	t.Setenv("WUPHF_NO_NEX", "1")
	t.Setenv("WUPHF_MEMORY_BACKEND", "")
	// Force gbrain NOT ready so the default deterministically falls back to
	// markdown regardless of whether gbrain happens to be installed on the
	// host running the suite: empty PATH (no gbrain binary) + no provider key.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WUPHF_GBRAIN_COMMAND", "")
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("WUPHF_ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	status := ResolveMemoryBackendStatus()
	if status.SelectedKind != config.MemoryBackendMarkdown {
		t.Fatalf("expected selected backend markdown, got %+v", status)
	}
	if status.ActiveKind != config.MemoryBackendMarkdown {
		t.Fatalf("expected active backend markdown, got %+v", status)
	}
}

func TestResolveMemoryBackendStatusGBrainReadyUnderNoNex(t *testing.T) {
	t.Setenv("WUPHF_NO_NEX", "1")
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_OPENAI_API_KEY", "sk-test-openai")

	binDir := t.TempDir()
	gbrainBin := filepath.Join(binDir, "gbrain")
	if err := os.WriteFile(gbrainBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake gbrain: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	status := ResolveMemoryBackendStatus()
	if status.SelectedKind != config.MemoryBackendGBrain || status.ActiveKind != config.MemoryBackendGBrain {
		t.Fatalf("expected gbrain to stay active under no-nex, got %+v", status)
	}
}

func TestResolveMemoryBackendStatusGBrainNeedsEmbedder(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	// Force no embedding provider deterministically: empty PATH (no ollama
	// binary) and no OpenAI key, regardless of host environment.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WUPHF_GBRAIN_COMMAND", "")
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	status := ResolveMemoryBackendStatus()
	if status.SelectedKind != config.MemoryBackendGBrain {
		t.Fatalf("expected gbrain to stay selected, got %+v", status)
	}
	if status.ActiveKind != config.MemoryBackendNone {
		t.Fatalf("expected gbrain to remain inactive without an embedder, got %+v", status)
	}
	if !strings.Contains(status.Detail, "OpenAI") || !strings.Contains(status.Detail, "vector search") {
		t.Fatalf("expected embedder guidance, got %+v", status)
	}
}

func TestResolveMemoryBackendStatusGBrainAnthropicOnlyIsNotSemantic(t *testing.T) {
	// An Anthropic key has no embeddings API. With no OpenAI key and no local
	// ollama embedder, gbrain cannot do semantic retrieval, so it is reported
	// inactive rather than "active in reduced mode" (the old, wrong behavior).
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_ANTHROPIC_API_KEY", "sk-ant-test-anthropic")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WUPHF_GBRAIN_COMMAND", "")
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	status := ResolveMemoryBackendStatus()
	if status.ActiveKind != config.MemoryBackendNone {
		t.Fatalf("expected gbrain inactive with anthropic-only, got %+v", status)
	}
	if !strings.Contains(status.Detail, "Anthropic") || !strings.Contains(status.Detail, "embeddings") {
		t.Fatalf("expected anthropic-no-embeddings detail, got %+v", status)
	}
}

func TestShouldPollNexNotificationsOnlyWhenNexIsActive(t *testing.T) {
	t.Setenv("WUPHF_NO_NEX", "")
	t.Setenv("WUPHF_API_KEY", "nex-test-key")
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendNex)

	binDir := t.TempDir()
	nexMCP := filepath.Join(binDir, "nex-mcp")
	if err := os.WriteFile(nexMCP, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake nex-mcp: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if !shouldPollNexNotifications() {
		t.Fatal("expected nex notification polling when nex backend is active")
	}

	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_OPENAI_API_KEY", "sk-test-openai")
	gbrainBin := filepath.Join(binDir, "gbrain")
	if err := os.WriteFile(gbrainBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake gbrain: %v", err)
	}
	if shouldPollNexNotifications() {
		t.Fatal("did not expect nex notification polling when gbrain is active")
	}
}

func TestInferSharedMemoryOwnerFromGBrainSlug(t *testing.T) {
	owner := inferSharedMemoryOwner("wuphf-shared--pm--launch-brief--20260416-120000", "")
	if owner != "pm" {
		t.Fatalf("expected owner pm from gbrain slug, got %q", owner)
	}
}

func TestScopedMemoryHitFromGBrainResultPreservesCitationFields(t *testing.T) {
	hit := scopedMemoryHitFromGBrainResult(gbrain.SearchResult{
		Slug:        "people/nazz",
		PageID:      42,
		Title:       "Nazz",
		ChunkText:   "Relevant compiled truth",
		ChunkSource: "compiled_truth",
		ChunkID:     7,
		ChunkIndex:  3,
		Score:       0.91,
		Stale:       true,
	})

	if hit.Identifier != "people/nazz" || hit.Slug != "people/nazz" || hit.PageID != 42 || hit.ChunkID != 7 || hit.ChunkIndex != 3 || hit.Source != "compiled_truth" {
		t.Fatalf("gbrain citation fields were not preserved: %+v", hit)
	}
	if hit.Score == nil || *hit.Score != 0.91 {
		t.Fatalf("expected score 0.91, got %+v", hit.Score)
	}
	if hit.Stale == nil || !*hit.Stale {
		t.Fatalf("expected stale=true, got %+v", hit.Stale)
	}
}
