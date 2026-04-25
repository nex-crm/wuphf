package team

// Regression coverage for the test-isolation race that surfaced as a
// flaky `test` job on PR #281's CI run, then was fixed in PR #282.
// saveLocked used a fixed `<path>.tmp` filename, so two
// brokers concurrently saving to the same state path could interleave
// like this:
//
//   A.WriteFile(path.tmp, dataA)
//   B.WriteFile(path.tmp, dataB)   // clobbers A's bytes (harmless)
//   A.Rename(path.tmp, path)       // wins; path = dataB content
//   B.Rename(path.tmp, path)       // FAILS: path.tmp no longer exists
//
// In production only one broker writes a given path, so the race is
// invisible — but the test suite used to share a leaked tempdir from
// worktree_guard_test.go init() across every unisolated test, plus many
// test files that overrode the state path and leaked goroutines reading
// the old global. The TestHeadlessTurnCompletedDurably… flake in CI was
// this race.
//
// Fix: each save uses a unique tmp filename via os.CreateTemp, so
// concurrent saves can never race on the source of a Rename.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSaveLocked_ConcurrentBrokersSamePathDoNotRace(t *testing.T) {
	// Pin every goroutine's Broker to the same per-test tempdir path so
	// all N goroutines target the same file (the production failure mode).
	statePath := filepath.Join(t.TempDir(), "broker-state.json")

	const goroutines = 32

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			b := NewBrokerAt(statePath)
			// EnsurePlannedTask calls saveLocked synchronously under b.mu —
			// each broker serializes its own saves, but two brokers do not
			// share a mutex, so they race on the on-disk tmp filename. Vary
			// Title per goroutine so each save serializes a distinct JSON
			// payload — otherwise WriteFile clobbers see byte-identical
			// content and the rename race is artificially harder to surface.
			_, _, err := b.EnsurePlannedTask(plannedTaskInput{
				Channel:       "general",
				Title:         fmt.Sprintf("race repro %d", i),
				Owner:         "ceo",
				CreatedBy:     "ceo",
				TaskType:      "feature",
				ExecutionMode: "local_worktree",
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	var got []error
	for err := range errs {
		got = append(got, err)
	}
	if len(got) > 0 {
		t.Errorf("expected zero errors across %d concurrent saves; got %d, first: %v",
			goroutines, len(got), got[0])
		// Surface up to 5 distinct messages so the failure report shows
		// the rename signature even if the race produced multiple.
		seen := map[string]struct{}{}
		for _, err := range got {
			msg := err.Error()
			if _, ok := seen[msg]; ok {
				continue
			}
			seen[msg] = struct{}{}
			t.Logf("error: %s", msg)
			if len(seen) >= 5 {
				break
			}
		}
	}

	// Sanity: at least the final state.json must exist after all saves.
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state file missing after concurrent saves: %v", err)
	}
}
