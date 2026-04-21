package migration

// gbrain.go adapts the local `gbrain` CLI onto migration.Adapter.
//
// Discovery strategy
// ==================
//
// GBrain exposes an MCP-style command surface. The adapter uses two tools:
//
//   - list_pages  — returns page slugs + titles + types. Used to enumerate.
//   - get_page    — returns full page content for a single slug.
//
// Both are reached through the existing gbrain.Call helper (same transport
// used by gbrainMemoryBackend.WriteShared), so there's a single place that
// knows how to shell out to the binary. When list_pages is unavailable
// (older GBrain builds), we fall back to a broad gbrain.Query scan which
// surfaces page slugs via the ChunkSource field.
//
// Timeouts are generous: gbrain.Call defaults to 8s per call and a large
// brain can have thousands of pages, so the adapter must accept that a
// full migration takes minutes, not seconds.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

// gbrainListPageSize is the page size requested from list_pages. Keeping
// it large reduces round-trips against a batch-oriented CLI.
const gbrainListPageSize = 200

// GBrainPage is the shape we expect list_pages + get_page to return. All
// fields are optional so a sparse upstream response still yields useful
// records.
type GBrainPage struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"created_at"`
}

// GBrainCaller is the subset of gbrain.Call we need. Split out as an
// interface so tests inject a fake without the gbrain binary being
// present on PATH.
type GBrainCaller interface {
	Call(ctx context.Context, tool string, params any) (string, error)
}

type gbrainDefaultCaller struct{}

func (gbrainDefaultCaller) Call(ctx context.Context, tool string, params any) (string, error) {
	return gbrain.Call(ctx, tool, params)
}

// GBrainAdapter iterates a GBrain brain via list_pages + get_page. When
// list_pages is not supported, it falls back to a broad Query sweep.
type GBrainAdapter struct {
	caller   GBrainCaller
	pageSize int
	// fallbackQuery is used when list_pages returns an error; defaults to
	// "*" which most GBrain builds interpret as "match everything".
	fallbackQuery string
}

// GBrainOption tunes a GBrainAdapter at construction.
type GBrainOption func(*GBrainAdapter)

// WithGBrainCaller injects a custom caller (tests).
func WithGBrainCaller(c GBrainCaller) GBrainOption {
	return func(a *GBrainAdapter) {
		if c != nil {
			a.caller = c
		}
	}
}

// WithGBrainPageSize overrides the default list_pages batch size.
func WithGBrainPageSize(n int) GBrainOption {
	return func(a *GBrainAdapter) {
		if n > 0 {
			a.pageSize = n
		}
	}
}

// WithGBrainFallbackQuery overrides the default query used when
// list_pages is not supported.
func WithGBrainFallbackQuery(q string) GBrainOption {
	return func(a *GBrainAdapter) {
		q = strings.TrimSpace(q)
		if q != "" {
			a.fallbackQuery = q
		}
	}
}

// NewGBrainAdapter returns a ready-to-use GBrain adapter.
func NewGBrainAdapter(opts ...GBrainOption) *GBrainAdapter {
	a := &GBrainAdapter{
		caller:        gbrainDefaultCaller{},
		pageSize:      gbrainListPageSize,
		fallbackQuery: "*",
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Iter satisfies migration.Adapter. Emits one MigrationRecord per page.
func (a *GBrainAdapter) Iter(ctx context.Context) (<-chan MigrationRecord, error) {
	out := make(chan MigrationRecord, 16)
	go func() {
		defer close(out)
		if err := a.iterViaList(ctx, out); err == nil {
			return
		}
		// list_pages unavailable — fall back to a query scan. Best effort.
		_ = a.iterViaQuery(ctx, out)
	}()
	return out, nil
}

func (a *GBrainAdapter) iterViaList(ctx context.Context, out chan<- MigrationRecord) error {
	offset := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		raw, err := a.caller.Call(ctx, "list_pages", map[string]any{
			"limit":  a.pageSize,
			"offset": offset,
		})
		if err != nil {
			return err
		}
		pages, err := decodeGBrainPages(raw)
		if err != nil {
			return err
		}
		if len(pages) == 0 {
			return nil
		}
		for _, p := range pages {
			if ctx.Err() != nil {
				return nil
			}
			rec, ok := a.hydratePage(ctx, p)
			if !ok {
				continue
			}
			select {
			case out <- rec:
			case <-ctx.Done():
				return nil
			}
		}
		if len(pages) < a.pageSize {
			return nil
		}
		offset += len(pages)
	}
}

func (a *GBrainAdapter) iterViaQuery(ctx context.Context, out chan<- MigrationRecord) error {
	raw, err := a.caller.Call(ctx, "query", map[string]any{
		"query":  a.fallbackQuery,
		"limit":  a.pageSize,
		"detail": "low",
	})
	if err != nil {
		return err
	}
	var results []gbrain.SearchResult
	if derr := json.Unmarshal([]byte(raw), &results); derr != nil {
		return fmt.Errorf("gbrain adapter: decode fallback query: %w", derr)
	}
	seen := map[string]struct{}{}
	for _, r := range results {
		slug := strings.TrimSpace(r.Slug)
		if slug == "" {
			continue
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		rec, ok := a.hydratePage(ctx, GBrainPage{
			Slug:  slug,
			Title: strings.TrimSpace(r.Title),
			Type:  strings.TrimSpace(r.Type),
			// Fallback uses the chunk text as content; list_pages path
			// supersedes this with the full page body.
			Content: strings.TrimSpace(r.ChunkText),
		})
		if !ok {
			continue
		}
		select {
		case out <- rec:
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

// hydratePage fetches the full content for a page when only a summary
// was returned. Returns ok=false when the page has no usable content.
func (a *GBrainAdapter) hydratePage(ctx context.Context, p GBrainPage) (MigrationRecord, bool) {
	content := strings.TrimSpace(firstGBrainContent(p))
	if content == "" && strings.TrimSpace(p.Slug) != "" {
		raw, err := a.caller.Call(ctx, "get_page", map[string]any{"slug": p.Slug})
		if err == nil {
			if full, derr := decodeGBrainPage(raw); derr == nil {
				p.Content = firstGBrainContent(full)
				if strings.TrimSpace(p.Title) == "" {
					p.Title = full.Title
				}
				if strings.TrimSpace(p.Type) == "" {
					p.Type = full.Type
				}
				if strings.TrimSpace(p.UpdatedAt) == "" {
					p.UpdatedAt = full.UpdatedAt
				}
			}
		}
		content = strings.TrimSpace(firstGBrainContent(p))
	}
	if content == "" {
		return MigrationRecord{}, false
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = p.Slug
	}
	return MigrationRecord{
		Kind:      NormalizeKind(p.Type),
		Slug:      slugify(p.Slug),
		Title:     title,
		Content:   content,
		Source:    "gbrain",
		Timestamp: parseNexTimestamp(firstNonEmpty(p.UpdatedAt, p.CreatedAt)),
	}, true
}

func firstGBrainContent(p GBrainPage) string {
	if s := strings.TrimSpace(p.Content); s != "" {
		return s
	}
	return strings.TrimSpace(p.Body)
}

// decodeGBrainPages handles the half-dozen shapes list_pages has
// emitted across GBrain releases.
func decodeGBrainPages(raw string) ([]GBrainPage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// Plain array of pages.
	if strings.HasPrefix(raw, "[") {
		var pages []GBrainPage
		if err := json.Unmarshal([]byte(raw), &pages); err != nil {
			return nil, fmt.Errorf("gbrain adapter: decode list_pages array: %w", err)
		}
		return pages, nil
	}
	// Envelope — look for common keys.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil, fmt.Errorf("gbrain adapter: decode list_pages envelope: %w", err)
	}
	for _, key := range []string{"pages", "data", "items", "results"} {
		if r, ok := envelope[key]; ok {
			var pages []GBrainPage
			if err := json.Unmarshal(r, &pages); err != nil {
				return nil, fmt.Errorf("gbrain adapter: decode list_pages[%s]: %w", key, err)
			}
			return pages, nil
		}
	}
	return nil, fmt.Errorf("gbrain adapter: unrecognised list_pages shape")
}

// decodeGBrainPage handles get_page responses. Accepts the page object
// directly or wrapped in {"page":{...}}.
func decodeGBrainPage(raw string) (GBrainPage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return GBrainPage{}, nil
	}
	var p GBrainPage
	if err := json.Unmarshal([]byte(raw), &p); err == nil && (p.Slug != "" || p.Content != "" || p.Body != "") {
		return p, nil
	}
	var envelope struct {
		Page GBrainPage `json:"page"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && envelope.Page.Slug != "" {
		return envelope.Page, nil
	}
	return GBrainPage{}, fmt.Errorf("gbrain adapter: unrecognised get_page shape")
}

// GBrainReady reports whether the gbrain binary is reachable on PATH.
// Exposed so the CLI can short-circuit before launching the worker.
func GBrainReady() bool {
	return gbrain.IsInstalled()
}

// NexNow is the fallback timestamp when a record has none of its own.
// Exposed so callers and tests use the same clock source.
func NexNow() time.Time { return time.Now().UTC() }
