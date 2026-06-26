package team

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestPamActions_RegistryHasCompileWiki proves the global recompile action is
// registered (so GET /pam/actions surfaces it) and is flagged global.
func TestPamActions_RegistryHasCompileWiki(t *testing.T) {
	a, err := LookupPamAction(PamActionCompileWiki)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if a.Label == "" {
		t.Fatalf("expected a label for compile_wiki")
	}
	if !pamActionIsGlobal(PamActionCompileWiki) {
		t.Fatalf("compile_wiki must be a global action")
	}
	if pamActionIsGlobal(PamActionEnrichArticle) {
		t.Fatalf("enrich_article must not be global")
	}
}

// TestPamDispatcher_CompileWikiInvokesHook proves a compile_wiki job routes
// through the injected compile hook (not the LLM runner) and emits the done
// event. The article path defaults to the synthetic global target.
func TestPamDispatcher_CompileWikiInvokesHook(t *testing.T) {
	runner := &fakePamRunner{}
	disp, _, pub, teardown := newPamFixtureWithFake(t, runner)
	defer teardown()

	var compiled atomic.Int64
	disp.SetCompileHook(func(context.Context) (CompileResult, error) {
		compiled.Add(1)
		return CompileResult{PagesWritten: 3, Concepts: 2}, nil
	})

	// Empty path is allowed for the global action.
	id, err := disp.Enqueue(PamActionCompileWiki, "", "human")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero job id")
	}

	waitPamCounts(t, pub, 1, 1, 0, 3*time.Second)

	if compiled.Load() != 1 {
		t.Fatalf("compile hook calls: want 1 got %d", compiled.Load())
	}
	// The LLM runner must NOT be called for the engine action.
	if runner.callCount() != 0 {
		t.Fatalf("LLM runner must not be called for compile_wiki; got %d calls", runner.callCount())
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.done) != 1 {
		t.Fatalf("want 1 done event, got %d", len(pub.done))
	}
	if pub.done[0].Action != string(PamActionCompileWiki) {
		t.Fatalf("done event action = %q, want %q", pub.done[0].Action, PamActionCompileWiki)
	}
	if pub.done[0].ArticlePath != pamGlobalTarget {
		t.Fatalf("done event path = %q, want %q", pub.done[0].ArticlePath, pamGlobalTarget)
	}
}

// TestPamDispatcher_CompileWikiHookErrorPublishesFailed proves a compile error
// surfaces as a failed event and does not crash the dispatcher.
func TestPamDispatcher_CompileWikiHookErrorPublishesFailed(t *testing.T) {
	disp, _, pub, teardown := newPamFixtureWithFake(t, &fakePamRunner{})
	defer teardown()

	disp.SetCompileHook(func(context.Context) (CompileResult, error) {
		return CompileResult{}, errors.New("compile blew up")
	})

	if _, err := disp.Enqueue(PamActionCompileWiki, "", "human"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitPamCounts(t, pub, 1, 0, 1, 3*time.Second)
}

// TestPamDispatcher_CompileWikiNoHookFails proves a compile_wiki job with no
// hook wired publishes a failed event (ErrPamCompileUnavailable) rather than
// hanging or panicking.
func TestPamDispatcher_CompileWikiNoHookFails(t *testing.T) {
	disp, _, pub, teardown := newPamFixtureWithFake(t, &fakePamRunner{})
	defer teardown()

	if _, err := disp.Enqueue(PamActionCompileWiki, "", "human"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// No hook fails fast (before the started event), so only a failed event
	// fires.
	waitPamCounts(t, pub, 0, 0, 1, 3*time.Second)
	if got := pub.lastFailedError(); got == "" {
		t.Fatalf("expected a failed-event error message")
	}
}

// TestPamDispatcher_EnrichStillRequiresPath proves the global-action carve-out
// did not weaken the per-article path requirement for enrich_article.
func TestPamDispatcher_EnrichStillRequiresPath(t *testing.T) {
	disp, _, _, teardown := newPamFixtureWithFake(t, &fakePamRunner{})
	defer teardown()

	if _, err := disp.Enqueue(PamActionEnrichArticle, "", "human"); err == nil {
		t.Fatalf("expected an error enqueuing enrich_article with an empty path")
	}
}
