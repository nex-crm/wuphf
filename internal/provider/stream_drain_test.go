package provider

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDrainStreamLinesNormal(t *testing.T) {
	t.Parallel()

	r := strings.NewReader("alpha\nbeta\ngamma\n")
	var got []string
	if err := DrainStreamLines(r, func(line string) {
		got = append(got, line)
	}); err != nil {
		t.Fatalf("DrainStreamLines: %v", err)
	}
	want := []string{"alpha\n", "beta\n", "gamma\n"}
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i, line := range want {
		if got[i] != line {
			t.Fatalf("line %d: got %q, want %q", i, got[i], line)
		}
	}
}

func TestDrainStreamLinesTrailingPartial(t *testing.T) {
	t.Parallel()

	r := strings.NewReader("first\nlast-no-newline")
	var got []string
	if err := DrainStreamLines(r, func(line string) {
		got = append(got, line)
	}); err != nil {
		t.Fatalf("DrainStreamLines: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 lines (first + trailing partial), got %d: %v", len(got), got)
	}
	if got[0] != "first\n" {
		t.Fatalf("first line: got %q, want %q", got[0], "first\n")
	}
	if got[1] != "last-no-newline" {
		t.Fatalf("trailing partial: got %q, want %q", got[1], "last-no-newline")
	}
}

func TestDrainStreamLinesEmpty(t *testing.T) {
	t.Parallel()

	r := strings.NewReader("")
	called := 0
	if err := DrainStreamLines(r, func(string) {
		called++
	}); err != nil {
		t.Fatalf("DrainStreamLines: %v", err)
	}
	if called != 0 {
		t.Fatalf("expected zero callbacks on empty reader, got %d", called)
	}
}

func TestDrainStreamLinesNilReader(t *testing.T) {
	t.Parallel()

	if err := DrainStreamLines(nil, func(string) {}); err == nil {
		t.Fatal("expected error for nil reader, got nil")
	}
}

// TestDrainStreamLinesOversizedLine is the regression test for the
// Scanner wedge. A single line larger than every previous Scanner buffer
// (1 MiB, 4 MiB) must drain cleanly. We pick 8 MiB to leave headroom and
// guarantee any future buffer constant change cannot quietly re-introduce
// the wedge.
func TestDrainStreamLinesOversizedLine(t *testing.T) {
	t.Parallel()

	const huge = 8 * 1024 * 1024 // 8 MiB
	body := strings.Repeat("x", huge)
	src := body + "\n" + "after\n"

	r := strings.NewReader(src)
	done := make(chan struct{})
	var got []string
	go func() {
		defer close(done)
		_ = DrainStreamLines(r, func(line string) {
			got = append(got, line)
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DrainStreamLines wedged on >8 MiB line — Scanner regression")
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 lines (huge + after), got %d", len(got))
	}
	if len(got[0]) != huge+1 { // body + \n
		t.Fatalf("oversized line len: got %d, want %d", len(got[0]), huge+1)
	}
	if got[1] != "after\n" {
		t.Fatalf("post-huge line: got %q, want %q", got[1], "after\n")
	}
}

// errReader returns a non-EOF error after the first read.
type errReader struct{ err error }

func (e *errReader) Read(p []byte) (int, error) { return 0, e.err }

func TestDrainStreamLinesPropagatesNonEOFError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	err := DrainStreamLines(&errReader{err: sentinel}, func(string) {})
	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrap of sentinel, got %v", err)
	}
}

func TestDrainStreamLinesReturnsEOFAsNil(t *testing.T) {
	t.Parallel()

	if err := DrainStreamLines(strings.NewReader("only-line"), func(string) {}); err != nil {
		t.Fatalf("EOF should be treated as nil; got %v", err)
	}
}

// TestDrainStreamLinesTruncatesLineBeyondLimit covers the byte-budget side
// of the contract: a line larger than maxLineBytes must (a) deliver a
// prefix capped at maxLineBytes, (b) silently drain the rest of that line
// from the upstream reader (no wedge, no second delivery), and (c) leave
// subsequent lines intact and uncorrupted.
func TestDrainStreamLinesTruncatesLineBeyondLimit(t *testing.T) {
	t.Parallel()

	const limit = 1024
	body := strings.Repeat("x", limit*4) // 4x the cap
	src := body + "\n" + "after\n"

	r := strings.NewReader(src)
	done := make(chan struct{})
	var got []string
	go func() {
		defer close(done)
		_ = DrainStreamLinesWithLimit(r, limit, func(line string) {
			got = append(got, line)
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DrainStreamLinesWithLimit wedged on oversized line — truncate-and-drain regression")
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 lines (truncated prefix + after), got %d: %v", len(got), got)
	}
	if len(got[0]) != limit {
		t.Fatalf("truncated prefix len: got %d, want %d", len(got[0]), limit)
	}
	if got[1] != "after\n" {
		t.Fatalf("post-truncation line: got %q, want %q", got[1], "after\n")
	}
}

func TestDrainStreamLinesWithLimitRejectsNonPositive(t *testing.T) {
	t.Parallel()

	if err := DrainStreamLinesWithLimit(strings.NewReader("a\n"), 0, func(string) {}); err == nil {
		t.Fatal("expected error for zero limit, got nil")
	}
	if err := DrainStreamLinesWithLimit(strings.NewReader("a\n"), -1, func(string) {}); err == nil {
		t.Fatal("expected error for negative limit, got nil")
	}
}

// io.MultiReader to a closed reader: ensure the helper doesn't loop on a
// reader that drips bytes and then returns io.EOF without a final \n.
func TestDrainStreamLinesDripFeed(t *testing.T) {
	t.Parallel()

	r := io.MultiReader(strings.NewReader("a\n"), strings.NewReader("b\n"), strings.NewReader("partial"))
	var got []string
	if err := DrainStreamLines(r, func(line string) {
		got = append(got, line)
	}); err != nil {
		t.Fatalf("DrainStreamLines: %v", err)
	}
	want := []string{"a\n", "b\n", "partial"}
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
