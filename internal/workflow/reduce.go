package workflow

import "encoding/json"

// reduce.go is the platform's size-budget reducer (RFC docs/specs/large-io-
// framework.md, S2). A large payload — a fat integration response or a big
// inter-step value — must be BOUNDED before it enters an LLM prompt or a
// persisted run record, and the reduction must be RECORDED (honest, not silent).
//
// It operates on json.RawMessage in and out: that is the right Go type for
// "valid JSON of a shape I don't statically know," and it avoids the
// any -> map[string]any -> re-assert round-trip that erodes type safety at every
// callsite (the reduced bytes can be stored, re-parsed into any target type, or
// rendered without a detour through `any`).
//
// Reduce runs only in the LIVE exec path (prompt assembly, run-record persist).
// shipcheck/replay uses recordingExec and never reduces, so determinism holds.

// Budget bounds a reduced payload. Zero fields fall back to defaults.
type Budget struct {
	MaxBytes  int // hard ceiling on the marshalled result
	MaxItems  int // max elements kept in any array
	StringCap int // max runes kept in any string value
}

// Reduction records what Reduce cut, for the run record and operator visibility.
type Reduction struct {
	ItemsOmitted int  `json:"items_omitted,omitempty"`
	BytesBefore  int  `json:"bytes_before"`
	BytesAfter   int  `json:"bytes_after"`
	Truncated    bool `json:"truncated"`
}

// DefaultRunRecordBudget bounds what a single action's output contributes to the
// persisted run record (runs.jsonl), so one fat fetch can't bloat the file or
// trip the reader's line-size limit.
var DefaultRunRecordBudget = Budget{MaxBytes: 256 << 10, MaxItems: 50, StringCap: 500}

// DefaultPromptBudget bounds what a value contributes to an LLM prompt.
var DefaultPromptBudget = Budget{MaxBytes: 128 << 10, MaxItems: 50, StringCap: 1000}

func (b Budget) withDefaults() Budget {
	if b.MaxItems <= 0 {
		b.MaxItems = 50
	}
	if b.StringCap <= 0 {
		b.StringCap = 1000
	}
	if b.MaxBytes <= 0 {
		b.MaxBytes = 256 << 10
	}
	return b
}

// Reduce returns a bounded view of raw plus a Reduction describing what was cut.
// On invalid JSON it returns the input unchanged with Truncated=false (the
// caller decides what to do with non-JSON). Reduction is structural only — array
// tails dropped, strings truncated — never an LLM summarization (that would add
// nondeterminism to a deterministic boundary).
func Reduce(raw json.RawMessage, b Budget) (json.RawMessage, Reduction) {
	b = b.withDefaults()
	red := Reduction{BytesBefore: len(raw), BytesAfter: len(raw)}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw, red // not JSON we can structure-reduce; leave it to the caller
	}
	reduced, omitted, trunc := reduceValue(v, b)
	out, err := json.Marshal(reduced)
	if err != nil {
		return raw, red
	}
	// If still over the byte ceiling, shrink arrays harder until it fits or we
	// hit one item each. Bounded loop: MaxItems halves each pass.
	for len(out) > b.MaxBytes && b.MaxItems > 1 {
		b.MaxItems /= 2
		reduced, more, t2 := reduceValue(v, b)
		omitted += more
		trunc = trunc || t2
		if m, err := json.Marshal(reduced); err == nil {
			out = m
		} else {
			break
		}
	}
	red.ItemsOmitted = omitted
	red.BytesAfter = len(out)
	red.Truncated = trunc || len(out) < len(raw)
	return out, red
}

// reduceValue recursively caps arrays (MaxItems) and strings (StringCap),
// returning the reduced value, the count of omitted array elements, and whether
// anything was truncated.
func reduceValue(v any, b Budget) (any, int, bool) {
	switch t := v.(type) {
	case []any:
		omitted := 0
		trunc := false
		keep := t
		if len(t) > b.MaxItems {
			omitted += len(t) - b.MaxItems
			keep = t[:b.MaxItems]
			trunc = true
		}
		out := make([]any, len(keep))
		for i, e := range keep {
			r, o, tr := reduceValue(e, b)
			out[i] = r
			omitted += o
			trunc = trunc || tr
		}
		return out, omitted, trunc
	case map[string]any:
		omitted := 0
		trunc := false
		out := make(map[string]any, len(t))
		for k, e := range t {
			r, o, tr := reduceValue(e, b)
			out[k] = r
			omitted += o
			trunc = trunc || tr
		}
		return out, omitted, trunc
	case string:
		if runes := []rune(t); len(runes) > b.StringCap {
			return string(runes[:b.StringCap]) + "…", 0, true
		}
		return t, 0, false
	default:
		return v, 0, false
	}
}
