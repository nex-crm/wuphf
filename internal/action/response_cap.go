package action

import (
	"context"
	"fmt"
	"io"
)

// response_cap.go is the platform's fail-loud large-response framework (RFC
// docs/specs/large-io-framework.md, S1). The old read path capped responses at
// a fixed size with io.LimitReader and DISCARDED the overflow silently, which
// truncated large provider responses (e.g. a multi-MB Gmail fetch) into invalid
// JSON that then parsed as an empty result — a silent, data-dependent wrong
// answer. Here, exceeding the cap is an explicit, typed, recoverable signal; the
// platform never hands back truncated bytes.

// defaultMaxResponseBytes bounds a single provider response read. 4 MiB is
// generous for the metadata-mode responses callers are encouraged to request;
// a per-call override (ExecuteRequest.MaxResponseBytes) can raise it where a
// large body is genuinely needed. A modest default keeps total memory bounded
// under concurrent runs (N runs × this cap), unlike a large global default.
const defaultMaxResponseBytes int64 = 4 << 20

// maxResponseBytesAbsoluteCeiling bounds even an explicit per-call override, so
// a crafted request can't ask the platform to buffer arbitrary memory.
const maxResponseBytesAbsoluteCeiling int64 = 32 << 20

// ResultTooLargeError is returned when a provider response exceeds the read cap.
// It carries enough to drive a recovery (lower max_results, request metadata
// mode, paginate) and to log the event for operability. Detect it with
// errors.As.
type ResultTooLargeError struct {
	Action  string // action id, when known
	Limit   int64  // the cap that was exceeded (bytes)
	AtLeast int64  // bytes observed before bailing (== Limit+1 when detected)
}

func (e *ResultTooLargeError) Error() string {
	action := e.Action
	if action == "" {
		action = "provider response"
	}
	return fmt.Sprintf("%s response exceeded the %d-byte read cap (the provider returned a larger payload); request a lighter response (e.g. metadata-only / fewer items) or raise MaxResponseBytes", action, e.Limit)
}

// ctxKeyMaxBytes threads a per-call response cap through the shared do() helper
// without changing every caller's signature. Unset → defaultMaxResponseBytes.
type ctxKeyMaxBytes struct{}

func withMaxResponseBytes(ctx context.Context, n int64) context.Context {
	if n <= 0 {
		return ctx
	}
	if n > maxResponseBytesAbsoluteCeiling {
		n = maxResponseBytesAbsoluteCeiling
	}
	return context.WithValue(ctx, ctxKeyMaxBytes{}, n)
}

func maxResponseBytesFromCtx(ctx context.Context) int64 {
	if v, ok := ctx.Value(ctxKeyMaxBytes{}).(int64); ok && v > 0 {
		return v
	}
	return defaultMaxResponseBytes
}

// readCapped reads the body up to limit bytes and detects overflow by STREAMING:
// it reads at most `limit` bytes into the buffer, then probes for one more byte.
// If the probe yields data, the body exceeded the cap and we bail immediately —
// we never buffer more than `limit` bytes just to reject an oversized response.
// Returns a *ResultTooLargeError on overflow rather than truncated bytes.
func readCapped(r io.Reader, limit int64, action string) ([]byte, error) {
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	buf, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	// Probe one byte past the cap. Any data here means the body was larger than
	// the cap and `buf` is a truncated prefix — fail loud instead of returning it.
	var probe [1]byte
	n, _ := r.Read(probe[:])
	if n > 0 {
		return nil, &ResultTooLargeError{Action: action, Limit: limit, AtLeast: limit + 1}
	}
	return buf, nil
}
