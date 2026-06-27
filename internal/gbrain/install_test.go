package gbrain

import (
	"context"
	"strings"
	"testing"
)

// withSeams saves and restores the install seams around a test.
func withSeams(t *testing.T) {
	t.Helper()
	oi, ob, or := isInstalledFn, resolveBunFn, runStreamFn
	t.Cleanup(func() { isInstalledFn, resolveBunFn, runStreamFn = oi, ob, or })
}

func TestEnsureInstalled_NoopWhenAlreadyInstalled(t *testing.T) {
	withSeams(t)
	isInstalledFn = func() bool { return true }
	calls := 0
	runStreamFn = func(context.Context, []string, func(string), string, ...string) error {
		calls++
		return nil
	}
	if err := EnsureInstalled(context.Background(), nil); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no commands when gbrain already installed, ran %d", calls)
	}
}

func TestEnsureInstalled_BunPresent_InstallsGBrain(t *testing.T) {
	withSeams(t)
	installed := false
	isInstalledFn = func() bool { return installed }
	resolveBunFn = func() string { return "/usr/local/bin/bun" }
	var got [][]string
	runStreamFn = func(_ context.Context, _ []string, _ func(string), name string, args ...string) error {
		got = append(got, append([]string{name}, args...))
		if name == "/usr/local/bin/bun" && len(args) > 0 && args[0] == "install" {
			installed = true // the gbrain install succeeded
		}
		return nil
	}
	if err := EnsureInstalled(context.Background(), nil); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected commands to run")
	}
	// Bun is present, so the first command is the gbrain install, NOT a bun bootstrap.
	first := strings.Join(got[0], " ")
	if !strings.HasPrefix(first, "/usr/local/bin/bun install -g ") || !strings.Contains(first, gbrainRepoSpec) {
		t.Fatalf("expected `bun install -g %s` first, got %q", gbrainRepoSpec, first)
	}
	for _, c := range got {
		if c[0] == "bash" {
			t.Fatalf("must not bootstrap bun when bun is present: %v", got)
		}
	}
}

func TestEnsureInstalled_BunMissing_BootstrapsBun(t *testing.T) {
	withSeams(t)
	installed := false
	bun := "" // missing until the bootstrap runs
	isInstalledFn = func() bool { return installed }
	resolveBunFn = func() string { return bun }
	var got [][]string
	runStreamFn = func(_ context.Context, _ []string, _ func(string), name string, args ...string) error {
		got = append(got, append([]string{name}, args...))
		if name == "bash" {
			bun = "/root/.bun/bin/bun" // bun installer dropped bun
		}
		if name == bun && len(args) > 0 && args[0] == "install" {
			installed = true
		}
		return nil
	}
	if err := EnsureInstalled(context.Background(), nil); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected bun bootstrap then gbrain install, got %v", got)
	}
	boot := strings.Join(got[0], " ")
	if got[0][0] != "bash" || !strings.Contains(boot, bunInstallURL) {
		t.Fatalf("expected bun bootstrap first, got %q", boot)
	}
}
