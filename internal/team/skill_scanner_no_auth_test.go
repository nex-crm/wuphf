package team

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// noAuthProvider is an llmProvider that simulates an unauthed/uninstalled
// LLM CLI by returning ErrLLMNotConfigured from AskIsSkill. It tracks the
// number of times it was invoked so the test can assert the scan loop bails
// after the very first call instead of hammering the dead provider for every
// article in the wiki.
type noAuthProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *noAuthProvider) AskIsSkill(_ context.Context, path, _, _ string) (bool, SkillFrontmatter, string, string, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return false, SkillFrontmatter{}, "", "", fmt.Errorf("%w: %w", ErrLLMNotConfigured, errors.New("exit status 1"))
}

func (p *noAuthProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestSkillScanner_NoLLMConfigured_BailsAfterFirstCall pins the #980 fix:
// when the LLM provider is unconfigured/unauthed, the scanner emits one
// summary WARN line and stops the per-article loop instead of logging
// "LLM call failed, skipping article" once per file in the wiki.
func TestSkillScanner_NoLLMConfigured_BailsAfterFirstCall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Skipf("git unavailable: %v", err)
	}

	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	worker.Start(context.Background())
	defer worker.Stop()
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	// Seed three articles — the same shape as the QA-reported boot:
	// team/about/{README,company,owner}.md, all with no auth available.
	for _, name := range []string{"README", "company", "owner"} {
		rel := filepath.Join("team", "about", name+".md")
		if _, _, err := repo.Commit(context.Background(), "ceo", rel,
			"# "+name+"\n\nseed body\n", "create", "seed "+name); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	prov := &noAuthProvider{}
	scanner := NewSkillScanner(b, prov, 100)
	res, err := scanner.Scan(context.Background(), "", true, "manual")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if got := prov.callCount(); got != 1 {
		t.Fatalf("unauthed provider should be invoked exactly once, was called %d times", got)
	}

	if res.Scanned != 1 {
		t.Fatalf("scanned = %d, want 1 (loop should break after first auth failure)", res.Scanned)
	}

	logText := logBuf.String()
	const summaryNeedle = "skipping scan"
	if !strings.Contains(logText, summaryNeedle) {
		t.Fatalf("expected single summary line containing %q, got:\n%s",
			summaryNeedle, logText)
	}
	if n := strings.Count(logText, summaryNeedle); n != 1 {
		t.Fatalf("expected exactly one summary line, got %d:\n%s", n, logText)
	}
	if strings.Contains(logText, "LLM call failed, skipping article") {
		// The legacy per-article WARN must be gone — it would put us back
		// to the original behaviour of 3 WARNs per pass.
		t.Fatalf("did not expect per-article WARN line, log was:\n%s", logText)
	}

	// The summary error should be attached so callers can see why the scan
	// short-circuited.
	if len(res.Errors) == 0 || res.Errors[0].Reason != "llm_not_configured" {
		t.Fatalf("expected llm_not_configured in res.Errors, got %+v", res.Errors)
	}
}

// TestErrLLMNotConfiguredIsWrappable confirms the sentinel composes cleanly
// with fmt.Errorf("%w: %w", …) so callers can attach the underlying provider
// detail without losing errors.Is matching.
func TestErrLLMNotConfiguredIsWrappable(t *testing.T) {
	wrapped := fmt.Errorf("%w: %w", ErrLLMNotConfigured, errors.New("exit status 1"))
	if !errors.Is(wrapped, ErrLLMNotConfigured) {
		t.Fatalf("wrapped error should match ErrLLMNotConfigured via errors.Is")
	}
}

// TestSkillScannerOnlyWrapsAuthErrors locks in the CodeRabbit fix on PR #987:
// defaultLLMProvider.AskIsSkill must only wrap with ErrLLMNotConfigured when
// the underlying error is a concrete auth signal. A transient/network/exec
// failure (e.g. bare "exit status 1") must NOT trigger the llmCalls==1
// bailout in Scan, otherwise a single hiccup on the first article would skip
// the rest of the pass even when auth is fine.
func TestSkillScannerOnlyWrapsAuthErrors(t *testing.T) {
	authSamples := []string{
		"Not logged in - Please run /login",
		"Please run `claude login`",
		"Codex CLI requires login. Run `codex login`",
		"authentication required",
		"Unauthorized",
	}
	for _, sample := range authSamples {
		probe := classifyProviderAuthError(sample)
		if !probe.IsAuthError {
			t.Errorf("expected IsAuthError=true for %q", sample)
		}
	}
	nonAuthSamples := []string{
		"exit status 1",
		"context deadline exceeded",
		"connection refused",
		"unexpected EOF",
		"signal: killed",
		"",
	}
	for _, sample := range nonAuthSamples {
		probe := classifyProviderAuthError(sample)
		if probe.IsAuthError {
			t.Errorf("expected IsAuthError=false for %q (got Provider=%q SignInCommand=%q)",
				sample, probe.Provider, probe.SignInCommand)
		}
	}
}

// syncLogBuffer is a mutex-guarded buffer safe to install as the global
// slog sink while unrelated goroutines may still be logging.
type syncLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
