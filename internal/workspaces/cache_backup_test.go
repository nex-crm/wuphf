package workspaces

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureCacheBackupExclusionsDoesNotCreateMissingCaches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	cacheDir := filepath.Join(home, ".wuphf-spaces", "main", ".wuphf", "cache")
	reg := &Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{{
			Name:        "main",
			RuntimeHome: filepath.Join(home, ".wuphf-spaces", "main"),
			BrokerPort:  MainBrokerPort,
			WebPort:     MainWebPort,
			State:       StateNeverStarted,
			CreatedAt:   time.Now().UTC(),
			LastUsedAt:  time.Now().UTC(),
		}},
	}
	if err := Write(reg); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	if err := EnsureCacheBackupExclusions(); err != nil {
		t.Fatalf("EnsureCacheBackupExclusions: %v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("expected missing cache dir to stay missing, stat err = %v", err)
	}
}

func TestEnsureCacheBackupExclusionsAcceptsExistingCaches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	runtimeHome := filepath.Join(home, ".wuphf-spaces", "main")
	cacheDir := filepath.Join(runtimeHome, ".wuphf", "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	reg := &Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{{
			Name:        "main",
			RuntimeHome: runtimeHome,
			BrokerPort:  MainBrokerPort,
			WebPort:     MainWebPort,
			State:       StateNeverStarted,
			CreatedAt:   time.Now().UTC(),
			LastUsedAt:  time.Now().UTC(),
		}},
	}
	if err := Write(reg); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	if err := EnsureCacheBackupExclusions(); err != nil {
		t.Fatalf("EnsureCacheBackupExclusions: %v", err)
	}
}
