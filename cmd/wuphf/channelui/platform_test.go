package channelui

import "testing"

// Empty URL must short-circuit before any exec.Command, so it can never
// fail and can never spawn a browser helper. Locks the contract that
// callers can pass an unconditional `OpenBrowserURL(maybeBlankURL)`
// without a pre-check. Whitespace-only inputs are intentionally not
// covered here — exercising them would spawn the platform browser
// launcher with junk input and produce nondeterministic test runs.
func TestOpenBrowserURLEmptyIsNoop(t *testing.T) {
	if err := OpenBrowserURL(""); err != nil {
		t.Fatalf("expected nil error for empty url, got %v", err)
	}
}

// Platform-detection helpers are mutually exclusive and at most one is
// true at a time. This protects against an accidental switch
// re-ordering inside OpenBrowserURL where two cases could match.
func TestPlatformDetectorsAreExclusive(t *testing.T) {
	count := 0
	if IsDarwin() {
		count++
	}
	if IsLinux() {
		count++
	}
	if IsWindows() {
		count++
	}
	if count > 1 {
		t.Fatalf("expected at most one platform helper to return true, got %d", count)
	}
}
