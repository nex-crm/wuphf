package provider

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// DrainStreamLines reads complete `\n`-terminated lines from r and invokes
// onLine for each line, including the trailing newline. A trailing partial
// line (no `\n` before EOF) is delivered as a final onLine call so callers
// see every byte the producer wrote.
//
// The historical wedge: every provider parser used bufio.Scanner with a
// fixed buffer (1 MiB to 4 MiB). When a single JSONL line exceeded the
// buffer the scanner returned ErrTooLong, the parse loop exited, and
// nothing else drained the upstream Reader. With io.TeeReader feeding
// the Scanner that meant the cmd's stdout pipe filled, the child blocked
// on write, and cmd.Wait never returned. Codex's runner-side tee already
// uses bufio.NewReader.ReadString — DrainStreamLines lifts that pattern
// to a shared helper so the same reliability guarantee covers Claude,
// Codex, Opencode, and any future provider parser.
//
// onLine receives strings (not bytes) because every existing call site
// already builds a string. onLine must not retain the slice across calls
// — the helper makes no allocation guarantees beyond what bufio.Reader
// provides through ReadString.
//
// Errors other than io.EOF are returned wrapped. io.EOF is treated as
// normal termination and is not surfaced.
func DrainStreamLines(r io.Reader, onLine func(string)) error {
	if r == nil {
		return errors.New("provider: DrainStreamLines: nil reader")
	}
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		// ReadString returns whatever it had buffered alongside the
		// error, so the trailing partial line is delivered before we
		// surface EOF. Empty trailing chunk is dropped.
		if line != "" && onLine != nil {
			onLine(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("provider: DrainStreamLines: %w", err)
		}
	}
}
