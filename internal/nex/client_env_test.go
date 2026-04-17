package nex

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/buildinfo"
)

func TestAppendClientEnvSetsDefault(t *testing.T) {
	env := appendClientEnv([]string{"PATH=/usr/bin", "HOME=/home/user"})
	want := NexClientEnvVar + "=wuphf/" + buildinfo.Current().Version
	if !contains(env, want) {
		t.Fatalf("expected %q in env, got %v", want, env)
	}
	// Original env entries preserved.
	if !contains(env, "PATH=/usr/bin") || !contains(env, "HOME=/home/user") {
		t.Fatalf("existing env entries were dropped: %v", env)
	}
}

func TestAppendClientEnvRespectsExistingValue(t *testing.T) {
	// A wrapper may have already declared itself as the client — don't
	// stomp on that. This lets integrators build chains like
	// NEX_CLIENT=myapp/wuphf/<version> without us clobbering.
	env := appendClientEnv([]string{NexClientEnvVar + "=myapp/1.0", "PATH=/usr/bin"})
	if count := countPrefix(env, NexClientEnvVar+"="); count != 1 {
		t.Fatalf("expected exactly one %s entry, got %d: %v", NexClientEnvVar, count, env)
	}
	if !contains(env, NexClientEnvVar+"=myapp/1.0") {
		t.Fatalf("existing NEX_CLIENT value was overwritten: %v", env)
	}
}

func contains(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func countPrefix(env []string, prefix string) int {
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// End-to-end: asserts Run() actually propagates NEX_CLIENT to the
// subprocess. Without this, a refactor that drops the cmd.Env line could
// silently ship a nex-cli invocation with no client tag.
func TestRun_PropagatesNexClientToSubprocess(t *testing.T) {
	dir := withIsolatedPATH(t)
	t.Setenv("WUPHF_NO_NEX", "")
	// The fake nex-cli writes its $NEX_CLIENT to stdout; Run() trims and
	// returns it.
	writeFakeNexCLI(t, dir, "nex-cli", `printf '%s' "$NEX_CLIENT"`)

	got, err := Run(nil, "whatever")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasPrefix(got, "wuphf/") {
		t.Fatalf("expected NEX_CLIENT=wuphf/<version>, got %q", got)
	}
}
