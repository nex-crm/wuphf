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
	kind := config.ResolveLLMProvider("")
	if e := Lookup(kind); e != nil && e.Capabilities.SupportsOneShot && e.OneShot != nil {
		return e.OneShot(systemPrompt, prompt, cwd)
	}
	return RunClaudeOneShot(systemPrompt, prompt, cwd)
}

// RunConfiguredOneShotCtx is like RunConfiguredOneShot but returns ctx.Err()
// immediately when the context is cancelled or expired. The underlying LLM
// subprocess is started in a goroutine; if the context fires first the call
// returns without waiting for it to finish.
func RunConfiguredOneShotCtx(ctx context.Context, systemPrompt, prompt, cwd string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		t, e := RunConfiguredOneShot(systemPrompt, prompt, cwd)
		ch <- result{t, e}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.text, r.err
	}
}
