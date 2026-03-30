package commands

import (
	"os"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

func withTempConfigHome(t *testing.T, f func()) {
	t.Helper()
	orig := os.Getenv("HOME")
	t.Setenv("HOME", t.TempDir())
	defer os.Setenv("HOME", orig)
	f()
}

func TestConfigSetActionProviderPersists(t *testing.T) {
	withTempConfigHome(t, func() {
		result := Dispatch("/config set action_provider composio", "", "text", 0)
		if result.ExitCode != 0 {
			t.Fatalf("expected success, got exit=%d err=%q output=%q", result.ExitCode, result.Error, result.Output)
		}
		if result.Output != "Set action_provider = composio" {
			t.Fatalf("unexpected output %q", result.Output)
		}

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.ActionProvider != "composio" {
			t.Fatalf("expected action_provider composio, got %q", cfg.ActionProvider)
		}
		if got := config.ResolveActionProvider(); got != "composio" {
			t.Fatalf("expected resolved action provider composio, got %q", got)
		}
	})
}

func TestConfigShowIncludesResolvedActionProvider(t *testing.T) {
	withTempConfigHome(t, func() {
		if err := config.Save(config.Config{ActionProvider: "composio"}); err != nil {
			t.Fatalf("save config: %v", err)
		}

		result := Dispatch("/config show", "", "text", 0)
		if result.ExitCode != 0 {
			t.Fatalf("expected success, got exit=%d err=%q output=%q", result.ExitCode, result.Error, result.Output)
		}
		if want := "Action provider: composio"; !contains(result.Output, want) {
			t.Fatalf("expected %q in output %q", want, result.Output)
		}
	})
}
