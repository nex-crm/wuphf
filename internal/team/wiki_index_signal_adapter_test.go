package team

import (
	"errors"
	"fmt"
	"testing"
)

// TestErrStopIterationWrappedMatchesWithErrorsIs guards the HIGH fix: any
// future caller that wraps the sentinel via fmt.Errorf("iterate: %w", ...)
// must still match errors.Is so the adapter does not surface it as a real
// error. Previously the adapter compared with == which would break on any
// wrap.
func TestErrStopIterationWrappedMatchesWithErrorsIs(t *testing.T) {
	// Direct equality still works.
	if !errors.Is(errStopIteration, errStopIteration) {
		t.Fatal("errors.Is(errStopIteration, errStopIteration) = false, want true")
	}

	// Wrapped once.
	wrapped := fmt.Errorf("iterate: %w", errStopIteration)
	if !errors.Is(wrapped, errStopIteration) {
		t.Errorf("errors.Is(wrapped, errStopIteration) = false; single-wrap regressed")
	}

	// Wrapped twice — the stop-iteration sentinel must still match so
	// deeply-wrapped short-circuits don't fall through as real errors.
	doubleWrapped := fmt.Errorf("outer: %w", wrapped)
	if !errors.Is(doubleWrapped, errStopIteration) {
		t.Errorf("errors.Is(doubleWrapped, errStopIteration) = false; double-wrap regressed")
	}

	// An unrelated error MUST NOT match.
	other := errors.New("something else")
	if errors.Is(other, errStopIteration) {
		t.Errorf("errors.Is(unrelated-error, errStopIteration) = true; false positive")
	}
	if errors.Is(fmt.Errorf("wrapped other: %w", other), errStopIteration) {
		t.Errorf("errors.Is(wrapped unrelated-error, errStopIteration) = true; false positive")
	}
}
