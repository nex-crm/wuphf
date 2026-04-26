package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLatestCLI(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "args.log")
	npmPath := filepath.Join(dir, "npm")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(logFile) + "\n"
	if err := os.WriteFile(npmPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WUPHF_CLI_INSTALL_BIN", "npm")
	t.Setenv("WUPHF_CLI_PACKAGE", "@example/wuphf")

	notice, err := InstallLatestCLI(context.Background())
	if err != nil {
		t.Fatalf("InstallLatestCLI returned error: %v", err)
	}
	if !strings.Contains(notice, "@example/wuphf") {
		t.Fatalf("expected package name in notice, got %q", notice)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	got := strings.Fields(string(data))
	want := []string{"install", "-g", "@example/wuphf@latest"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("expected args %v, got %v", want, got)
	}
}

func TestInstallLatestCLIReturnsHelpfulFailure(t *testing.T) {
	dir := t.TempDir()
	npmPath := filepath.Join(dir, "npm")
	script := "#!/bin/sh\necho boom >&2\nexit 1\n"
	if err := os.WriteFile(npmPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WUPHF_CLI_INSTALL_BIN", "npm")
	t.Setenv("WUPHF_CLI_PACKAGE", "@example/wuphf")

	_, err := InstallLatestCLI(context.Background())
	if err == nil {
		t.Fatal("expected install failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
}
