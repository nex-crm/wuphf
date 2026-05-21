package teammcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// payloadFieldOrder is the priority list of payload keys we surface in
// the approval card, top to bottom. Each entry maps a payload key (any
// of the synonyms ship together) to the label the human sees. The first
// match per label wins so synonyms like to/recipient/recipients do not
// double up.
var payloadFieldOrder = []struct {
	Keys  []string
	Label string
}{
	{[]string{"to", "recipient", "recipients", "recipient_email", "user_id"}, "To"},
	{[]string{"cc"}, "CC"},
	{[]string{"bcc"}, "BCC"},
	{[]string{"from", "sender"}, "From"},
	{[]string{"email", "email_address"}, "Email"},
	{[]string{"subject"}, "Subject"},
	{[]string{"channel", "channel_id"}, "Channel"},
	{[]string{"thread_ts", "thread_id"}, "Thread"},
	{[]string{"text", "message", "body", "content", "html_body"}, "Body"},
	{[]string{"title", "name"}, "Title"},
	{[]string{"first_name", "firstname", "given_name"}, "First name"},
	{[]string{"last_name", "lastname", "family_name"}, "Last name"},
	{[]string{"company", "company_name", "organization"}, "Company"},
	{[]string{"phone", "phone_number"}, "Phone"},
	{[]string{"status", "stage", "lifecycle_stage", "deal_stage"}, "Status"},
	{[]string{"summary", "description"}, "Description"},
	{[]string{"url", "link"}, "URL"},
	{[]string{"event_id", "calendar_event_id"}, "Event"},
	{[]string{"start_time", "start"}, "Starts"},
	{[]string{"end_time", "end"}, "Ends"},
	{[]string{"amount", "price"}, "Amount"},
	{[]string{"currency"}, "Currency"},
	{[]string{"query", "q"}, "Query"},
}

// payloadRedactedKeys are field names we never surface in the approval
// card. The human does not need to see credentials to decide whether to
// approve, and a leaky log is one OS clipboard away from a support ticket.
var payloadRedactedKeys = map[string]struct{}{
	"password":      {},
	"passwd":        {},
	"secret":        {},
	"api_key":       {},
	"access_token":  {},
	"refresh_token": {},
	"token":         {},
	"client_secret": {},
	"private_key":   {},
}

var payloadTechnicalKeys = map[string]struct{}{
	"connection_key":       {},
	"connected_account_id": {},
	"account_id":           {},
	"headers":              {},
}

// summarizeActionPayload renders a bulleted list of decision-relevant
// payload fields from args.Data, args.PathVariables, and
// args.QueryParameters. Long values are clipped, multi-line bodies are
// flattened, and redacted keys are skipped entirely. Returns an empty
// string when none of the recognized fields are present.
func summarizeActionPayload(args TeamActionExecuteArgs) string {
	type field struct {
		Label string
		Value string
	}
	var fields []field
	seen := make(map[string]bool, len(payloadFieldOrder))
	seenPaths := make(map[string]bool)

	sources := []map[string]any{args.Data, args.PathVariables, args.QueryParameters}
	for _, entry := range payloadFieldOrder {
		if seen[entry.Label] {
			continue
		}
		for _, key := range entry.Keys {
			if shouldSkipPayloadKey(key) {
				continue
			}
			value, path, ok := lookupPayloadValue(sources, key)
			if !ok {
				continue
			}
			rendered := formatPayloadValue(value)
			if rendered == "" {
				continue
			}
			fields = append(fields, field{Label: entry.Label, Value: rendered})
			seen[entry.Label] = true
			if path != "" {
				seenPaths[path] = true
			}
			break
		}
	}
	for _, extra := range additionalPayloadFields(sources, seenPaths, 8-len(fields)) {
		if seen[extra.Label] {
			continue
		}
		fields = append(fields, extra)
		seen[extra.Label] = true
	}

	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range fields {
		b.WriteString("• ")
		b.WriteString(f.Label)
		b.WriteString(": ")
		b.WriteString(f.Value)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// lookupPayloadValue checks each source map for the given key and returns the
// first hit plus its dotted path. Comparison is case-insensitive and recursive
// so provider envelopes like {"request":{"body":{"to":"..."}}} still produce a
// useful approval card instead of only showing the action id.
func lookupPayloadValue(sources []map[string]any, key string) (any, string, bool) {
	lowered := strings.ToLower(key)
	for sourceIndex, src := range sources {
		if src == nil {
			continue
		}
		if v, path, ok := lookupPayloadValueInMap(src, lowered, fmt.Sprintf("source%d", sourceIndex)); ok {
			return v, path, true
		}
	}
	return nil, "", false
}

func lookupPayloadValueInMap(src map[string]any, lowered, pathPrefix string) (any, string, bool) {
	keys := make([]string, 0, len(src))
	for k := range src {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := src[k]
		if shouldSkipPayloadKey(k) {
			continue
		}
		path := pathPrefix + "." + k
		if strings.ToLower(k) == lowered {
			return v, path, true
		}
	}
	for _, k := range keys {
		v := src[k]
		if shouldSkipPayloadKey(k) {
			continue
		}
		path := pathPrefix + "." + k
		if nested, ok := payloadMap(v); ok {
			if found, foundPath, ok := lookupPayloadValueInMap(nested, lowered, path); ok {
				return found, foundPath, true
			}
		}
	}
	return nil, "", false
}

func additionalPayloadFields(sources []map[string]any, seenPaths map[string]bool, limit int) []struct {
	Label string
	Value string
} {
	if limit <= 0 {
		return nil
	}
	var candidates []payloadFieldCandidate
	for sourceIndex, src := range sources {
		collectPayloadFields(&candidates, src, fmt.Sprintf("source%d", sourceIndex), nil)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Path < candidates[j].Path
	})
	out := make([]struct {
		Label string
		Value string
	}, 0, limit)
	for _, c := range candidates {
		if seenPaths[c.Path] || c.Value == "" {
			continue
		}
		out = append(out, struct {
			Label string
			Value string
		}{Label: c.Label, Value: c.Value})
		if len(out) == limit {
			break
		}
	}
	return out
}

type payloadFieldCandidate struct {
	Path  string
	Label string
	Value string
}

func collectPayloadFields(out *[]payloadFieldCandidate, v any, path string, labels []string) {
	switch t := v.(type) {
	case nil:
		return
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if shouldSkipPayloadKey(k) {
				continue
			}
			collectPayloadFields(out, t[k], path+"."+k, append(labels, k))
		}
	default:
		rendered := formatPayloadValue(t)
		if rendered == "" {
			return
		}
		*out = append(*out, payloadFieldCandidate{
			Path:  path,
			Label: payloadFieldLabel(labels),
			Value: rendered,
		})
	}
}

func payloadMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[string]string:
		out := make(map[string]any, len(t))
		for k, v := range t {
			out[k] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func shouldSkipPayloadKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return true
	}
	lowered := strings.ToLower(key)
	if _, ok := payloadRedactedKeys[lowered]; ok {
		return true
	}
	if _, ok := payloadTechnicalKeys[lowered]; ok {
		return true
	}
	compact := strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(lowered)
	for _, marker := range []string{"token", "secret", "password", "passwd", "apikey", "privatekey", "authorization", "cookie", "credential"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func payloadFieldLabel(parts []string) string {
	if len(parts) == 0 {
		return "Value"
	}
	keep := parts
	if len(keep) > 2 {
		keep = keep[len(keep)-2:]
	}
	words := make([]string, 0, len(keep)*2)
	for _, part := range keep {
		for _, word := range strings.FieldsFunc(part, func(r rune) bool {
			return r == '_' || r == '-' || r == '.' || r == ' '
		}) {
			word = strings.TrimSpace(word)
			if word != "" {
				words = append(words, titleCaser.String(strings.ToLower(word)))
			}
		}
	}
	if len(words) == 0 {
		return "Value"
	}
	return strings.Join(words, " ")
}

func actionApprovalAccountLabel(connectionKey string) string {
	key := sanitizeContextValue(strings.TrimSpace(connectionKey))
	if key == "" {
		return ""
	}
	lowered := strings.ToLower(key)
	switch {
	case strings.Contains(key, "::"):
		return ""
	case strings.HasPrefix(lowered, "ca_"), strings.HasPrefix(lowered, "conn_"), strings.HasPrefix(lowered, "acct_"):
		return ""
	}
	return key
}

// payloadValueClipLen is the soft cap on a single payload value we render in
// the approval card, measured in RUNES (not bytes) so multi-byte characters
// like CJK or emoji do not get sliced mid-codepoint into invalid UTF-8.
const payloadValueClipLen = 240

// formatPayloadValue renders any payload value as a single, clipped string.
// Arrays become comma-separated lists; structured values fall back to JSON.
func formatPayloadValue(v any) string {
	raw := payloadValueString(v)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(raw) > payloadValueClipLen {
		runes := []rune(raw)
		raw = string(runes[:payloadValueClipLen]) + "…"
	}
	return raw
}

func payloadValueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []string:
		return strings.Join(t, ", ")
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s := payloadValueString(redactPayloadValue(item)); strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		clean := redactPayloadValue(t)
		if clean == nil {
			return ""
		}
		raw, err := json.Marshal(clean)
		if err != nil {
			return fmt.Sprintf("%v", clean)
		}
		return string(raw)
	case map[string]string:
		clean := redactPayloadValue(t)
		if clean == nil {
			return ""
		}
		raw, err := json.Marshal(clean)
		if err != nil {
			return fmt.Sprintf("%v", clean)
		}
		return string(raw)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", t), "0"), ".")
	case int, int64:
		return fmt.Sprintf("%d", t)
	}
	clean := redactPayloadValue(v)
	if clean == nil {
		return ""
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return fmt.Sprintf("%v", clean)
	}
	return string(raw)
}

func redactPayloadValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, value := range t {
			if shouldSkipPayloadKey(k) {
				continue
			}
			clean := redactPayloadValue(value)
			if clean == nil {
				continue
			}
			out[k] = clean
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, value := range t {
			if shouldSkipPayloadKey(k) {
				continue
			}
			out[k] = value
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			clean := redactPayloadValue(item)
			if clean == nil {
				continue
			}
			out = append(out, clean)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return v
	}
}
