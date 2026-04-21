package migration

// nex.go iterates the legacy Nex HTTP records API and emits one
// MigrationRecord per stored record. Reuses the existing api.Client
// (same bearer-token plumbing the /record slash commands use) so there's
// exactly one place that knows how to talk to app.nex.ai.
//
// Discovery strategy
// ==================
//
// Nex does not expose a single "give me everything" endpoint. Instead
// it's object-type scoped: GET /v1/records?object_type={t}. We iterate
// the well-known legacy types (person, company, customer, topic, note)
// plus any extra types the caller passes via WithExtraTypes. Each type
// is paged with limit/offset until the server returns fewer items than
// the page size.
//
// The API client already handles auth + transport. This adapter only
// handles shape translation onto MigrationRecord.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/api"
)

// DefaultNexTypes is the seed list of Nex object types the adapter walks
// when no override is provided. Ordered to put high-signal record kinds
// first so --limit stops on the most useful content.
var DefaultNexTypes = []string{"person", "company", "customer", "topic", "note"}

// nexPageSize bounds each /v1/records request. Matching the CLI's default
// of 100 keeps the server's pagination code warm on the same fast path.
const nexPageSize = 100

// NexAdapter adapts the legacy Nex HTTP records API onto the
// migration.Adapter interface.
type NexAdapter struct {
	client    *api.Client
	types     []string
	fetchPage func(ctx context.Context, objectType string, limit, offset int) ([]map[string]any, error)
	pageSize  int
	stopOnErr bool
}

// NexOption configures a NexAdapter at construction.
type NexOption func(*NexAdapter)

// WithNexTypes overrides the default set of object types walked by the
// adapter. Useful for Nex installs with custom object schemas.
func WithNexTypes(types ...string) NexOption {
	return func(a *NexAdapter) {
		if len(types) == 0 {
			return
		}
		a.types = append(a.types[:0], types...)
	}
}

// WithNexFetcher injects a custom page fetcher. Primarily for tests;
// production code gets the default HTTP-backed implementation.
func WithNexFetcher(f func(ctx context.Context, objectType string, limit, offset int) ([]map[string]any, error)) NexOption {
	return func(a *NexAdapter) {
		a.fetchPage = f
	}
}

// WithNexPageSize overrides the default page size. Primarily for tests
// that want to exercise the pagination loop without 100-row fixtures.
func WithNexPageSize(n int) NexOption {
	return func(a *NexAdapter) {
		if n > 0 {
			a.pageSize = n
		}
	}
}

// NewNexAdapter returns a ready-to-use Nex adapter. When client is nil a
// default one is constructed from WUPHF_API_KEY / ResolveAPIKey; callers
// that want non-default config can build their own api.Client first.
func NewNexAdapter(client *api.Client, opts ...NexOption) *NexAdapter {
	a := &NexAdapter{
		client:   client,
		types:    append([]string{}, DefaultNexTypes...),
		pageSize: nexPageSize,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.fetchPage == nil {
		a.fetchPage = a.defaultFetchPage
	}
	return a
}

// defaultFetchPage is the production fetcher: GET /v1/records?object_type=...&limit=&offset=.
// Returns []map[string]any so the translator can be lenient about
// schema drift — legacy installs grew fields organically.
func (a *NexAdapter) defaultFetchPage(ctx context.Context, objectType string, limit, offset int) ([]map[string]any, error) {
	if a.client == nil {
		return nil, fmt.Errorf("nex adapter: api client is required")
	}
	if !a.client.IsAuthenticated() {
		return nil, fmt.Errorf("nex adapter: api client is not authenticated (set WUPHF_API_KEY or pass --api-key)")
	}
	q := url.Values{}
	q.Set("object_type", objectType)
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("offset", fmt.Sprintf("%d", offset))
	path := "/v1/records?" + q.Encode()
	// The API returns heterogeneous shapes across installs — newer backends
	// nest records under {"data": [...]}, older ones return the slice at
	// the top level. Decode as any and normalise here.
	raw, err := api.Get[any](a.client, path, 0)
	if err != nil {
		return nil, fmt.Errorf("nex adapter: fetch %s offset=%d: %w", objectType, offset, err)
	}
	return coerceNexRecordsList(raw)
}

// coerceNexRecordsList normalises the Nex /v1/records response onto
// []map[string]any regardless of whether the server wraps it in a
// {"data":[...]} envelope, a {"records":[...]} envelope, or returns a
// bare list. Unknown shapes yield an empty slice — the caller treats
// "zero records" as end-of-pagination.
func coerceNexRecordsList(raw any) ([]map[string]any, error) {
	switch v := raw.(type) {
	case []any:
		return mapEachToMap(v), nil
	case map[string]any:
		for _, key := range []string{"data", "records", "items", "results"} {
			if inner, ok := v[key]; ok {
				if list, ok := inner.([]any); ok {
					return mapEachToMap(list), nil
				}
			}
		}
	case nil:
		return nil, nil
	}
	return nil, fmt.Errorf("nex adapter: unexpected records shape %T", raw)
}

func mapEachToMap(list []any) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// Iter satisfies migration.Adapter. Streams records from each configured
// object type in turn, paginating until the server runs out. The channel
// is buffered to keep the fetcher moving while the caller writes.
func (a *NexAdapter) Iter(ctx context.Context) (<-chan MigrationRecord, error) {
	if len(a.types) == 0 {
		return nil, fmt.Errorf("nex adapter: no object types configured")
	}
	out := make(chan MigrationRecord, 16)
	go func() {
		defer close(out)
		for _, objectType := range a.types {
			if ctx.Err() != nil {
				return
			}
			if err := a.iterType(ctx, objectType, out); err != nil {
				// Per-type failures don't abort the whole walk — a legacy
				// install might have one broken table without the rest
				// being unreachable. The error is already annotated with
				// the type for triage; downgrade to a stderr-style line.
				if a.stopOnErr {
					return
				}
				_, _ = fmt.Printf("warning: %v\n", err)
			}
		}
	}()
	return out, nil
}

func (a *NexAdapter) iterType(ctx context.Context, objectType string, out chan<- MigrationRecord) error {
	offset := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		page, err := a.fetchPage(ctx, objectType, a.pageSize, offset)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		for _, row := range page {
			rec, ok := translateNexRecord(objectType, row)
			if !ok {
				continue
			}
			select {
			case out <- rec:
			case <-ctx.Done():
				return nil
			}
		}
		if len(page) < a.pageSize {
			return nil
		}
		offset += len(page)
	}
}

// translateNexRecord maps one Nex record (loose JSON) onto a
// MigrationRecord. Missing fields fall back to sensible defaults; when
// we can't salvage a Content field we skip the row (second return false)
// rather than emitting an empty article.
func translateNexRecord(objectType string, row map[string]any) (MigrationRecord, bool) {
	slug := slugify(firstString(row, "slug", "handle", "identifier", "id"))
	title := firstString(row, "title", "name", "display_name", "label")
	if slug == "" {
		slug = slugify(title)
	}
	if slug == "" {
		return MigrationRecord{}, false
	}
	if title == "" {
		title = slug
	}
	content := firstString(row, "content", "body", "description", "summary", "notes")
	if strings.TrimSpace(content) == "" {
		// Fall back to a full-JSON dump so no data is lost; the writer
		// will render it as a code block inside the article.
		if b, err := json.MarshalIndent(row, "", "  "); err == nil {
			content = string(b)
		}
	}
	if strings.TrimSpace(content) == "" {
		return MigrationRecord{}, false
	}
	ts := parseNexTimestamp(firstString(row, "updated_at", "modified_at", "created_at", "timestamp"))
	return MigrationRecord{
		Kind:      NormalizeKind(objectType),
		Slug:      slug,
		Title:     title,
		Content:   content,
		Source:    "nex",
		Timestamp: ts,
	}, true
}

// firstString returns the first non-empty string-valued field found at
// any of the provided keys. Numeric IDs coerce to string so they can
// seed a slug without forcing adapters to pre-string-ify.
func firstString(row map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := row[k]
		if !ok {
			continue
		}
		switch s := v.(type) {
		case string:
			if strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		case float64:
			return fmt.Sprintf("%v", s)
		case int:
			return fmt.Sprintf("%d", s)
		case int64:
			return fmt.Sprintf("%d", s)
		}
	}
	return ""
}

// parseNexTimestamp accepts the handful of shapes legacy Nex installs
// have produced over the years. Unparseable values yield the zero time.
func parseNexTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
