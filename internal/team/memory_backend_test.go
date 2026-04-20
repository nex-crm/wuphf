package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

func TestResolveMemoryBackendStatusNoNexFallsBackToMarkdown(t *testing.T) {
	// --no-nex with no explicit backend pick lands the user on the markdown
	// wiki — the default external memory that doesn't require Nex. Silent
	// 'none' was a worse default: the user asked to skip Nex, not to lose
	// all shared memory.
	t.Setenv("WUPHF_NO_NEX", "1")
	t.Setenv("WUPHF_MEMORY_BACKEND", "")

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

func TestResolveMemoryBackendStatusGBrainNeedsProviderKey(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)

	status := ResolveMemoryBackendStatus()
	if status.SelectedKind != config.MemoryBackendGBrain {
		t.Fatalf("expected gbrain to stay selected, got %+v", status)
	}
	if status.ActiveKind != config.MemoryBackendNone {
		t.Fatalf("expected gbrain to remain inactive without provider key, got %+v", status)
	}
	if !strings.Contains(status.Detail, "OpenAI") || !strings.Contains(status.Detail, "vector search") {
		t.Fatalf("expected provider-key guidance, got %+v", status)
	}
}

func TestResolveMemoryBackendStatusGBrainAnthropicOnlyShowsReducedMode(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_ANTHROPIC_API_KEY", "sk-ant-test-anthropic")

	binDir := t.TempDir()
	gbrainBin := filepath.Join(binDir, "gbrain")
	if err := os.WriteFile(gbrainBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake gbrain: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	status := ResolveMemoryBackendStatus()
	if status.ActiveKind != config.MemoryBackendGBrain {
		t.Fatalf("expected gbrain to stay active with anthropic-only mode, got %+v", status)
	}
	if !strings.Contains(status.Detail, "Anthropic-only mode") || !strings.Contains(status.Detail, "OpenAI") {
		t.Fatalf("expected reduced-mode detail, got %+v", status)
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
