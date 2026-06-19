package action

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// countingReader reports how many bytes were actually read — to prove readCapped
// does not buffer far past the cap just to reject an oversized body.
type countingReader struct {
	data []byte
	pos  int
	read int
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, nil // emulate a body with more available later; never EOF here
	}
	n := copy(p, c.data[c.pos:])
	c.pos += n
	c.read += n
	return n, nil
}

func TestReadCappedUnderLimit(t *testing.T) {
	body := strings.Repeat("x", 1000)
	out, err := readCapped(strings.NewReader(body), 4096, "test")
	if err != nil {
		t.Fatalf("under-limit read should succeed: %v", err)
	}
	if string(out) != body {
		t.Fatalf("read mismatch: got %d bytes", len(out))
	}
}

func TestReadCappedOverflowFailsLoud(t *testing.T) {
	body := strings.Repeat("x", 5000)
	_, err := readCapped(strings.NewReader(body), 1024, "GMAIL_FETCH_EMAILS")
	if err == nil {
		t.Fatal("over-limit read must fail loud, not truncate")
	}
	var tooLarge *ResultTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("want *ResultTooLargeError, got %T: %v", err, err)
	}
	if tooLarge.Limit != 1024 || tooLarge.Action != "GMAIL_FETCH_EMAILS" {
		t.Fatalf("error should carry limit+action: %+v", tooLarge)
	}
	if !strings.Contains(tooLarge.Error(), "lighter response") {
		t.Fatalf("message should guide recovery: %q", tooLarge.Error())
	}
}

// readCapped must not buffer the entire oversized body — it reads at most the
// cap, then probes one byte. A 50 MiB body rejected at a 1 KiB cap must read
// ~1 KiB, not 50 MiB.
func TestReadCappedDoesNotBufferWholeBody(t *testing.T) {
	cr := &countingReader{data: make([]byte, 50<<20)} // 50 MiB available
	_, err := readCapped(cr, 1024, "big")
	if err == nil {
		t.Fatal("should overflow")
	}
	if cr.read > 1024+64 { // cap + a little slack for the probe
		t.Fatalf("readCapped buffered %d bytes; must cap reads near the limit (1024)", cr.read)
	}
}

func TestMaxResponseBytesCeiling(t *testing.T) {
	ctx := withMaxResponseBytes(context.Background(), 1<<30) // 1 GiB requested
	if got := maxResponseBytesFromCtx(ctx); got != maxResponseBytesAbsoluteCeiling {
		t.Fatalf("override must be clamped to the ceiling, got %d", got)
	}
	if got := maxResponseBytesFromCtx(context.Background()); got != defaultMaxResponseBytes {
		t.Fatalf("unset ctx must use the default, got %d", got)
	}
}
