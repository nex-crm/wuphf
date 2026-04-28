package main

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// Empty stdin pipe (no data, immediate EOF) MUST report handled=false so the
// caller falls through to the normal interactive launch. This is the
// regression vector for the Windows-launch bug — wuphf was exiting silently
// because isPiped() was true and the dispatch loop drained zero lines.
//
// On Windows, every spawn path that doesn't allocate a real console
// (SSH without -tt, scheduled tasks, PowerShell Start-Process) gives the
// child a closed pipe as stdin. If consumePipedStdin returns handled=true
// for that case, the bug is back.
func TestConsumePipedStdin_EmptyPipe_NotHandled(t *testing.T) {
	var dispatched []string
	handled, err := consumePipedStdin(strings.NewReader(""), func(line string) {
		dispatched = append(dispatched, line)
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if handled {
		t.Fatal("empty pipe must not be reported as handled — caller would exit silently")
	}
	if len(dispatched) != 0 {
		t.Fatalf("dispatch called for empty pipe: %v", dispatched)
	}
}

// Real piped command stream still short-circuits the interactive launch.
// This is the original purpose of the piped-stdin path; the fix must not
// regress the `echo "/foo" | wuphf` use case.
func TestConsumePipedStdin_LinesPresent_Handled(t *testing.T) {
	var dispatched []string
	handled, err := consumePipedStdin(strings.NewReader("hello\nworld\n"), func(line string) {
		dispatched = append(dispatched, line)
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !handled {
		t.Fatal("non-empty pipe must be reported as handled — caller would double-launch")
	}
	want := []string{"hello", "world"}
	if !reflect.DeepEqual(dispatched, want) {
		t.Fatalf("dispatched=%v; want %v", dispatched, want)
	}
}

// A single line without a trailing newline (common for one-shot `wuphf
// --cmd`-like uses) still counts as "handled" — bufio.Scanner returns
// it on EOF.
func TestConsumePipedStdin_SingleLineNoTrailingNewline_Handled(t *testing.T) {
	var dispatched []string
	handled, err := consumePipedStdin(strings.NewReader("/init"), func(line string) {
		dispatched = append(dispatched, line)
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !handled {
		t.Fatal("single line without trailing newline must be reported as handled")
	}
	if !reflect.DeepEqual(dispatched, []string{"/init"}) {
		t.Fatalf("dispatched=%v; want [/init]", dispatched)
	}
}

// Real I/O failure on the pipe must propagate. We rely on this so we can
// log the cause and exit with a non-zero status — silently dropping the
// error would mask broken pipes and other I/O issues.
type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestConsumePipedStdin_ReadError_Propagates(t *testing.T) {
	want := errors.New("pipe broke")
	handled, err := consumePipedStdin(&errReader{err: want}, func(string) {
		t.Fatal("dispatch must not be called when the read fails")
	})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v; want %v", err, want)
	}
	if handled {
		t.Fatal("read error before any line is delivered must report handled=false")
	}
	_ = io.EOF // keep io import in case the test grows
}
