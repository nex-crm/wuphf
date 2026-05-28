package teammcp

import (
	"fmt"
	"strings"
)

// summarizeActionResult renders the action result into a short,
// chat-friendly block the human can read at a glance. The goal is
// "what came back?" — not the full payload, not raw JSON wrapping.
// For action results that are obviously structured (a list, a map
// with familiar keys), we surface the top-level shape (count, ids,
// subjects). For opaque shapes, we fall back to a pretty-printed
// excerpt clipped to a budget so chat stays readable.
//
// The agent is still prompted to post a richer human_message right
// after — that's where the interpretation lives. This preview just
// guarantees the raw signal is visible immediately, even before the
// agent's followup lands.
func summarizeActionResult(result any) string {
	if result == nil {
		return ""
	}
	if shaped := shapeKnownResult(result); shaped != "" {
		return shaped
	}
	raw := prettyObject(result)
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" || raw == "[]" {
		return ""
	}
	const maxPreviewBytes = 600
	if len(raw) > maxPreviewBytes {
		raw = raw[:maxPreviewBytes] + "\n… (truncated; agent will summarize next)"
	}
	return "```\n" + raw + "\n```"
}

// shapeKnownResult tries to lift the most common result shapes into a
// human line:
//   - a slice → "N item(s)" + a couple of representative summaries
//   - a map with a `threads` / `messages` / `items` / `results` /
//     `events` slice → same as above
//
// Returns "" when no familiar shape matches; the caller falls back to
// the raw JSON excerpt.
func shapeKnownResult(result any) string {
	if arr, ok := result.([]any); ok {
		return shapeArraySummary(arr)
	}
	if m, ok := result.(map[string]any); ok {
		for _, key := range []string{"threads", "messages", "items", "results", "records", "events", "data", "rows"} {
			if v, present := m[key]; present {
				if arr, ok := v.([]any); ok {
					summary := shapeArraySummary(arr)
					if summary != "" {
						return key + ": " + summary
					}
				}
			}
		}
		var ids []string
		for _, key := range []string{"id", "thread_id", "message_id", "event_id"} {
			if v, ok := m[key].(string); ok && v != "" {
				ids = append(ids, key+"="+v)
			}
		}
		if len(ids) > 0 {
			return strings.Join(ids, ", ")
		}
	}
	return ""
}

// shapeArraySummary builds a "N items" header + up to 3 representative
// one-line excerpts, picking common identifying fields (subject, name,
// title, id) when each item is a map.
func shapeArraySummary(arr []any) string {
	if len(arr) == 0 {
		return "0 results"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d result(s)", len(arr))
	max := len(arr)
	if max > 3 {
		max = 3
	}
	for i := 0; i < max; i++ {
		line := shapeArrayItem(arr[i])
		if line == "" {
			continue
		}
		fmt.Fprintf(&b, "\n• %s", line)
	}
	if len(arr) > max {
		fmt.Fprintf(&b, "\n… and %d more", len(arr)-max)
	}
	return b.String()
}

func shapeArrayItem(item any) string {
	m, ok := item.(map[string]any)
	if !ok {
		s := strings.TrimSpace(fmt.Sprint(item))
		if len(s) > 120 {
			s = s[:120] + "…"
		}
		return s
	}
	for _, key := range []string{"subject", "title", "name", "summary", "snippet", "preview"} {
		if v, ok := m[key].(string); ok {
			v = strings.TrimSpace(v)
			if v != "" {
				if len(v) > 100 {
					v = v[:100] + "…"
				}
				for _, fromKey := range []string{"from", "sender", "author"} {
					if f, ok := m[fromKey].(string); ok && f != "" {
						return v + " — " + f
					}
				}
				return v
			}
		}
	}
	for _, key := range []string{"id", "thread_id", "message_id"} {
		if v, ok := m[key].(string); ok && v != "" {
			return key + "=" + v
		}
	}
	return ""
}
