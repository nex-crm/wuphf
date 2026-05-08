package provider

import (
	"context"

	"github.com/nex-crm/wuphf/internal/config"
)

// RunConfiguredOneShot runs a single-shot generation using the active LLM
// provider's OneShot implementation. Providers without a one-shot path
// (Capabilities.SupportsOneShot == false) and unregistered kinds fall back
// to Claude.
func RunConfiguredOneShot(systemPrompt, prompt, cwd string) (string, error) {
	return RunConfiguredOneShotCtx(context.Background(), systemPrompt, prompt, cwd)
}

// RunConfiguredOneShotCtx is like RunConfiguredOneShot, but cancellation is
// propagated into providers that expose a context-aware one-shot hook.
func RunConfiguredOneShotCtx(ctx context.Context, systemPrompt, prompt, cwd string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	kind := config.ResolveLLMProvider("")
	if e := Lookup(kind); e != nil && e.Capabilities.SupportsOneShot {
		if e.OneShotCtx != nil {
			return e.OneShotCtx(ctx, systemPrompt, prompt, cwd)
		}
		if e.OneShot != nil {
			return runLegacyOneShotCtx(ctx, e.OneShot, systemPrompt, prompt, cwd)
		}
	}
	return RunClaudeOneShotCtx(ctx, systemPrompt, prompt, cwd)
}

func runLegacyOneShotCtx(ctx context.Context, fn func(systemPrompt, prompt, cwd string) (string, error), systemPrompt, prompt, cwd string) (string, error) {
	if ctx.Done() == nil {
		return fn(systemPrompt, prompt, cwd)
	}

	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		t, e := fn(systemPrompt, prompt, cwd)
		ch <- result{t, e}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.text, r.err
	}
}
