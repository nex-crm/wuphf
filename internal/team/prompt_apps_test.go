package team

// Tests for the Apps prompt blocks. The App Builder block carries the
// pre-publish verify gate (Phase 3): before register_app it must run
// `bun run verify` (tsc --noEmit + vite build), retry a bounded number of
// rounds on failure, and refuse to publish a broken app. The non-builder
// awareness block must NOT carry that gate language — the gate is the
// builder's responsibility, not every office agent's.

import (
	"strings"
	"testing"
)

// containsFold reports whether s contains substr, case-insensitively.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func TestAppBuilderPromptBlock_HasVerifyGateGuidance(t *testing.T) {
	block := appBuilderPromptBlock()

	// 1. It names the verify gate and the underlying type-check command.
	for _, phrase := range []string{"bun run verify", "verify", "tsc --noEmit"} {
		if !containsFold(block, phrase) {
			t.Errorf("appBuilderPromptBlock missing gate phrase %q", phrase)
		}
	}

	// 2. It ties the gate to register_app — must pass before publishing.
	if !containsFold(block, "register_app") {
		t.Fatalf("appBuilderPromptBlock no longer mentions register_app")
	}
	gatesPublish := containsFold(block, "before you call register_app") ||
		containsFold(block, "until the gate passes") ||
		containsFold(block, "do not call register_app")
	if !gatesPublish {
		t.Errorf("appBuilderPromptBlock missing 'gate before register_app' guidance")
	}

	// 3. It mandates a bounded retry / auto-fix loop.
	boundedRetry := containsFold(block, "up to about 2 rounds") ||
		(containsFold(block, "round") && containsFold(block, "again"))
	if !boundedRetry {
		t.Errorf("appBuilderPromptBlock missing bounded-retry guidance")
	}

	// 4. It refuses to publish a broken app on persistent failure.
	noPublishOnFail := containsFold(block, "blocking") &&
		(containsFold(block, "do not publish") ||
			containsFold(block, "instead of calling register_app") ||
			containsFold(block, "does not type-check or build"))
	if !noPublishOnFail {
		t.Errorf("appBuilderPromptBlock missing 'do not publish a broken app' guidance")
	}
}

func TestAppsAwarenessPromptBlock_OmitsVerifyGate(t *testing.T) {
	block := appsAwarenessPromptBlock()

	// The gate is App-Builder-only. The awareness block (every other agent)
	// must not carry build/type-check gate language.
	for _, phrase := range []string{"bun run verify", "tsc --noEmit", "register_app"} {
		if containsFold(block, phrase) {
			t.Errorf("appsAwarenessPromptBlock should not contain builder-only phrase %q", phrase)
		}
	}
}
