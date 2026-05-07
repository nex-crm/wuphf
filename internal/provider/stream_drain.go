package provider

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DefaultMaxLineBytes caps the in-memory size of a single line. The cap is
// generous enough that legitimate provider output is delivered intact (8x
// the largest prior Scanner cap of 4 MiB, well above realistic JSONL events
// from any of the headless agents we run today) but bounded enough that a
// pathological or buggy upstream emitting an unbounded line cannot OOM the
// parent process.
const DefaultMaxLineBytes = 32 * 1024 * 1024

// DrainStreamLines reads complete `\n`-terminated lines from r and invokes
// onLine for each line. Each delivered line normally includes its trailing
// `\n` and a trailing partial line (no `\n` before EOF) is delivered as a
// final onLine call so callers see every byte the producer wrote.
//
// Implementation uses bufio.Reader.ReadSlice in a chunked loop, capped at
// DefaultMaxLineBytes per line. When a line exceeds the cap, only the
// prefix up to the cap is delivered (without a trailing newline), and the
// remaining bytes of that line are drained from the upstream reader and
// discarded. The upstream is never left blocked on a full pipe, and memory
// growth per line is bounded.
//
// Use DrainStreamLinesWithLimit to override the cap for tests or callers
// with a different memory profile.
//
// The historical wedge: every provider parser used bufio.Scanner with a
// fixed buffer (1 MiB to 4 MiB). When a single JSONL line exceeded the
// buffer the scanner returned ErrTooLong, the parse loop exited, and
// nothing else drained the upstream Reader. With io.TeeReader feeding the
// Scanner that meant the cmd's stdout pipe filled, the child blocked on
// write, and cmd.Wait never returned. DrainStreamLines lifts the runner-
// side ReadString pattern to a shared helper and adds the byte cap so the
// same reliability guarantee covers Claude, Codex, Opencode, and any
// future provider parser.
//
// onLine receives strings (not bytes) because every existing call site
// already builds a string. onLine must not retain the slice across calls
// — the helper makes no allocation guarantees beyond what bufio.Reader
// provides.
//
// Errors other than io.EOF are returned wrapped. io.EOF is treated as
// normal termination and is not surfaced.
func DrainStreamLines(r io.Reader, onLine func(string)) error {
	return DrainStreamLinesWithLimit(r, DefaultMaxLineBytes, onLine)
}

// DrainStreamLinesWithLimit is DrainStreamLines with an explicit per-line
// byte cap. maxLineBytes must be positive.
func DrainStreamLinesWithLimit(r io.Reader, maxLineBytes int, onLine func(string)) error {
	if r == nil {
		return errors.New("provider: DrainStreamLines: nil reader")
	}
	if maxLineBytes <= 0 {
		return errors.New("provider: DrainStreamLines: maxLineBytes must be positive")
	}
	br := bufio.NewReader(r)

	var (
		buf       strings.Builder
		truncated bool
	)
	deliver := func() {
		if buf.Len() > 0 && onLine != nil {
			onLine(buf.String())
		}
		buf.Reset()
		truncated = false
	}

	for {
		slice, err := br.ReadSlice('\n')

		// Append what we got, capped at maxLineBytes. Once a line exceeds
		// the cap, switch to discard mode for the remainder of that line:
		// keep calling ReadSlice so the upstream pipe drains, but stop
		// growing buf.
		if !truncated && len(slice) > 0 {
			remaining := maxLineBytes - buf.Len()
			if len(slice) <= remaining {
				buf.Write(slice)
			} else {
				if remaining > 0 {
					buf.Write(slice[:remaining])
				}
				truncated = true
			}
		}

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			// No newline found before bufio's internal buffer filled.
			// Continue draining; the slice we already consumed was either
			// appended or counted against the cap.
			continue
		case err == nil:
			// Got a complete line ending with '\n'.
			deliver()
		case errors.Is(err, io.EOF):
			// Trailing partial line, if any.
			deliver()
			return nil
		default:
			return fmt.Errorf("provider: DrainStreamLines: %w", err)
		}
	}
}
