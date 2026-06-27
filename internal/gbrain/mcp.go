package gbrain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names exposed by `gbrain serve`. Centralised so typed methods and tests
// agree on the wire identifiers in one place.
const (
	toolQuery            = "query"
	toolSearch           = "search"
	toolGetPage          = "get_page"
	toolListPages        = "list_pages"
	toolPutPage          = "put_page"
	toolFindExperts      = "find_experts"
	toolGetBrainIdentity = "get_brain_identity"
	toolAddLink          = "add_link"
	toolGetLinks         = "get_links"
)

// Client default timeouts. Per-call work is short; spawning `gbrain serve` and
// completing the MCP handshake can be slower, so the connect budget is larger.
const (
	defaultCallTimeout    = 15 * time.Second
	defaultConnectTimeout = 30 * time.Second
)

// MCPURLEnv is the environment variable that, when set, points the Client at a
// remote gbrain MCP endpoint (streamable HTTP) instead of spawning a local
// `gbrain serve` subprocess.
const MCPURLEnv = "WUPHF_GBRAIN_MCP_URL"

// Client is a lazy, thread-safe MCP client to a gbrain knowledge backend.
//
// It connects on first use and keeps a persistent session. If a call fails on a
// dead session the Client transparently reconnects once and retries. The zero
// value is not usable; construct one with NewClient. The Client is owned by its
// caller (the broker wires it in a later slice); it holds no global state.
type Client struct {
	// remoteURL, when non-empty, selects the streamable HTTP transport.
	// Otherwise the Client spawns `gbrain serve` over stdio.
	remoteURL string

	callTimeout    time.Duration
	connectTimeout time.Duration

	// transportFn overrides transport construction. Nil in production (the
	// Client picks remote HTTP vs local stdio itself); tests inject a fake
	// transport to exercise reconnect without a subprocess.
	transportFn func(context.Context) (mcp.Transport, error)

	mu      sync.Mutex
	session *mcp.ClientSession
}

// Option customises a Client.
type Option func(*Client)

// WithRemoteURL forces the streamable HTTP transport at the given endpoint,
// overriding the WUPHF_GBRAIN_MCP_URL env var.
func WithRemoteURL(url string) Option {
	return func(c *Client) { c.remoteURL = strings.TrimSpace(url) }
}

// WithCallTimeout overrides the per-call context budget.
func WithCallTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.callTimeout = d
		}
	}
}

// WithConnectTimeout overrides the connect/handshake budget.
func WithConnectTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.connectTimeout = d
		}
	}
}

// NewClient builds a gbrain MCP client. When WUPHF_GBRAIN_MCP_URL is set (and
// no explicit WithRemoteURL is given) the Client dials that remote endpoint;
// otherwise it spawns the local `gbrain serve` subprocess on first use. No
// connection is opened until the first call.
func NewClient(opts ...Option) *Client {
	c := &Client{
		callTimeout:    defaultCallTimeout,
		connectTimeout: defaultConnectTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.remoteURL == "" {
		c.remoteURL = strings.TrimSpace(resolveMCPURL())
	}
	return c
}

// resolveMCPURL reads the remote endpoint env var. Pulled out so tests can
// reason about it without touching the constructor.
func resolveMCPURL() string {
	return strings.TrimSpace(os.Getenv(MCPURLEnv))
}

// Connect opens the MCP session if it is not already open. Safe to call
// repeatedly and concurrently; subsequent calls are no-ops while the session
// is alive.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.ensureSessionLocked(ctx)
	return err
}

// Close tears down the session, if any. Always safe to call.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return nil
	}
	err := c.session.Close()
	c.session = nil
	if err != nil {
		return fmt.Errorf("close gbrain mcp session: %w", err)
	}
	return nil
}

// ensureSessionLocked returns a live session, dialing one if needed. Caller
// must hold c.mu.
func (c *Client) ensureSessionLocked(ctx context.Context) (*mcp.ClientSession, error) {
	if c.session != nil {
		return c.session, nil
	}
	session, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	c.session = session
	return session, nil
}

// dial constructs the transport (remote HTTP or local stdio) and completes the
// MCP handshake under the connect timeout.
func (c *Client) dial(ctx context.Context) (*mcp.ClientSession, error) {
	build := c.transport
	if c.transportFn != nil {
		build = c.transportFn
	}
	transport, err := build(ctx)
	if err != nil {
		return nil, err
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "wuphf-gbrain-client",
		Version: "0.1.0",
	}, nil)

	connectCtx, cancel := context.WithTimeout(ctx, c.connectTimeout)
	defer cancel()
	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect gbrain mcp: %w", err)
	}
	return session, nil
}

// transport selects the streamable HTTP transport when a remote URL is set,
// otherwise spawns `gbrain serve` over stdio using the shared BinaryPath /
// gbrainEnv helpers. Returns ErrNotInstalled when neither a URL nor a local
// binary is available.
func (c *Client) transport(ctx context.Context) (mcp.Transport, error) {
	if c.remoteURL != "" {
		return &mcp.StreamableClientTransport{Endpoint: c.remoteURL}, nil
	}

	bin := BinaryPath()
	if bin == "" {
		return nil, fmt.Errorf("connect gbrain mcp: %w (set %s or install gbrain on PATH)", ErrNotInstalled, MCPURLEnv)
	}

	// CommandContext ties the subprocess lifetime to ctx; the transport owns
	// the pipes. gbrainEnv injects HOME + OPENAI/ANTHROPIC keys gbrain needs.
	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = gbrainEnv()
	return &mcp.CommandTransport{Command: cmd}, nil
}

// CallTool invokes an MCP tool by name and returns the flattened text payload
// (the concatenated TextContent, with an "ERROR: " prefix when the result is
// flagged as an error). It connects lazily and, if the call fails on what looks
// like a dead session, reconnects once and retries.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("gbrain call: tool name is required")
	}

	callCtx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()

	result, err := c.callOnce(callCtx, name, args)
	if err != nil {
		// One transparent retry on a fresh session: covers a serve subprocess
		// that died or a remote endpoint that dropped the session. Don't retry
		// when the caller's context is already done.
		if ctx.Err() != nil {
			return "", fmt.Errorf("gbrain call %s: %w", name, err)
		}
		c.resetSession()
		retryCtx, retryCancel := context.WithTimeout(ctx, c.callTimeout)
		defer retryCancel()
		result, err = c.callOnce(retryCtx, name, args)
		if err != nil {
			return "", fmt.Errorf("gbrain call %s (after reconnect): %w", name, err)
		}
	}
	return flattenResult(result), nil
}

// callOnce performs a single tool call against a (possibly freshly dialed)
// session.
func (c *Client) callOnce(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	c.mu.Lock()
	session, err := c.ensureSessionLocked(ctx)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp call: %w", err)
	}
	return result, nil
}

// resetSession drops the current session so the next call dials a fresh one.
func (c *Client) resetSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
}

// flattenResult concatenates the TextContent of an MCP result, prefixing
// "ERROR: " when IsError is set, and falling back to JSON-encoded
// StructuredContent when there is no text content. Mirrors the extraction
// idiom in internal/team/headless_openai_compat_mcp.go.
func flattenResult(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	if result.IsError {
		b.WriteString("ERROR: ")
	}
	for _, content := range result.Content {
		tc, ok := content.(*mcp.TextContent)
		if !ok || tc == nil {
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(tc.Text)
	}
	if b.Len() == 0 && result.StructuredContent != nil {
		if data, err := json.Marshal(result.StructuredContent); err == nil {
			b.Write(data)
		}
	}
	return b.String()
}

// ---- Typed methods ----------------------------------------------------------

// Hit is a single retrieval result from query/search. It reuses the field
// shape of SearchResult (defined in cli.go); the methods below return
// SearchResult directly so this is an alias for readability at call sites.
type Hit = SearchResult

// Page is a single gbrain page with its raw markdown body and metadata. Fields
// are tolerant: gbrain returns a superset and missing fields decode to zero
// values.
type Page struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// Content is the page body. gbrain's get_page returns the body under
	// "compiled_truth"; "content" is accepted as a fallback for forward
	// compatibility. GetPage folds compiled_truth into Content after decode.
	Content       string         `json:"content"`
	CompiledTruth string         `json:"compiled_truth"`
	Type          string         `json:"type"`
	Tags          []string       `json:"tags"`
	Frontmatter   map[string]any `json:"frontmatter"`
}

// PageMeta is page metadata as returned by list_pages (no body).
type PageMeta struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Tags    []string `json:"tags"`
	Updated string   `json:"updated"`
	Stale   bool     `json:"stale"`
}

// ListOptions filters list_pages. Zero values are omitted from the call.
type ListOptions struct {
	Limit          int
	Type           string
	Tag            string
	IncludeDeleted bool
}

// PutOptions carries the slug plus optional provenance metadata for put_page.
// Content is the full markdown (with YAML frontmatter) and is passed separately
// to PutPage.
//
// NOTE: the live gbrain put_page schema marks BOTH slug and content as
// required (verified via ListTools on gbrain serve), so Slug must be set even
// though the markdown frontmatter also carries a title. source_kind /
// ingested_via are server-stamped for remote callers and only honoured for
// local stdio callers.
type PutOptions struct {
	Slug        string
	SourceKind  string
	IngestedVia string
}

// PutResult is the tolerant result of a put_page write.
type PutResult struct {
	Slug   string `json:"slug"`
	Status string `json:"status"`
	PageID int    `json:"page_id"`
}

// Query runs gbrain's hybrid retrieval (vector + keyword + expansion) and
// returns the parsed hits.
func (c *Client) Query(ctx context.Context, query string, limit int) ([]Hit, error) {
	return c.searchLike(ctx, toolQuery, query, limit, map[string]any{"detail": "low"})
}

// Search runs gbrain's full-text search and returns the parsed hits.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	return c.searchLike(ctx, toolSearch, query, limit, nil)
}

// searchLike powers Query and Search, which share an argument and result shape.
func (c *Client) searchLike(ctx context.Context, tool, query string, limit int, extra map[string]any) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	args := map[string]any{"query": query, "limit": limit}
	for k, v := range extra {
		args[k] = v
	}
	raw, err := c.CallTool(ctx, tool, args)
	if err != nil {
		return nil, err
	}
	var hits []Hit
	if err := decodeJSON(raw, &hits); err != nil {
		return nil, fmt.Errorf("decode gbrain %s: %w", tool, err)
	}
	return hits, nil
}

// GetPage fetches a single page (markdown + metadata) by slug.
func (c *Client) GetPage(ctx context.Context, slug string) (Page, error) {
	var page Page
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return page, fmt.Errorf("gbrain get_page: slug is required")
	}
	raw, err := c.CallTool(ctx, toolGetPage, map[string]any{"slug": slug})
	if err != nil {
		return page, err
	}
	if err := decodeJSON(raw, &page); err != nil {
		return page, fmt.Errorf("decode gbrain get_page: %w", err)
	}
	if page.Content == "" {
		page.Content = page.CompiledTruth
	}
	return page, nil
}

// ListPages returns page metadata, filtered by opts.
func (c *Client) ListPages(ctx context.Context, opts ListOptions) ([]PageMeta, error) {
	args := map[string]any{}
	if opts.Limit > 0 {
		args["limit"] = opts.Limit
	}
	if t := strings.TrimSpace(opts.Type); t != "" {
		args["type"] = t
	}
	if tag := strings.TrimSpace(opts.Tag); tag != "" {
		args["tag"] = tag
	}
	if opts.IncludeDeleted {
		args["include_deleted"] = true
	}
	raw, err := c.CallTool(ctx, toolListPages, args)
	if err != nil {
		return nil, err
	}
	var pages []PageMeta
	if err := decodeJSON(raw, &pages); err != nil {
		return nil, fmt.Errorf("decode gbrain list_pages: %w", err)
	}
	return pages, nil
}

// PutPage writes a page from full markdown (including YAML frontmatter, from
// which gbrain derives slug/title). gbrain chunks and embeds the content.
func (c *Client) PutPage(ctx context.Context, content string, opts PutOptions) (PutResult, error) {
	var result PutResult
	if strings.TrimSpace(content) == "" {
		return result, fmt.Errorf("gbrain put_page: content is required")
	}
	args := map[string]any{"content": content}
	if slug := strings.TrimSpace(opts.Slug); slug != "" {
		args["slug"] = slug
	}
	if sk := strings.TrimSpace(opts.SourceKind); sk != "" {
		args["source_kind"] = sk
	}
	if iv := strings.TrimSpace(opts.IngestedVia); iv != "" {
		args["ingested_via"] = iv
	}
	raw, err := c.CallTool(ctx, toolPutPage, args)
	if err != nil {
		return result, err
	}
	if err := decodeJSON(raw, &result); err != nil {
		return result, fmt.Errorf("decode gbrain put_page: %w", err)
	}
	return result, nil
}

// Expert is a tolerant find_experts hit.
type Expert struct {
	Name   string  `json:"name"`
	Slug   string  `json:"slug"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// FindExperts is a thin pass-through to gbrain's expert finder. The result
// shape is best-effort; gbrain returns an array of expert records.
func (c *Client) FindExperts(ctx context.Context, topic string, limit int) ([]Expert, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, nil
	}
	// The live find_experts schema takes `topic` (free-form NL), not `query`.
	args := map[string]any{"topic": topic}
	if limit > 0 {
		args["limit"] = limit
	}
	raw, err := c.CallTool(ctx, toolFindExperts, args)
	if err != nil {
		return nil, err
	}
	var experts []Expert
	if err := decodeJSON(raw, &experts); err != nil {
		return nil, fmt.Errorf("decode gbrain find_experts: %w", err)
	}
	return experts, nil
}

// Identity calls get_brain_identity. It is a cheap connectivity probe: any
// non-error response means the session is live. Returns the raw identity text.
func (c *Client) Identity(ctx context.Context) (string, error) {
	raw, err := c.CallTool(ctx, toolGetBrainIdentity, map[string]any{})
	if err != nil {
		return "", err
	}
	return raw, nil
}

// Link is a tolerant view of one graph edge as returned by get_links. Note the
// asymmetry with add_link: the write tool takes from/to, but get_links returns
// from_slug/to_slug.
type Link struct {
	From   string `json:"from_slug"`
	To     string `json:"to_slug"`
	Type   string `json:"link_type"`
	Source string `json:"link_source"`
}

// AddLink creates a directed edge from -> to in gbrain's knowledge graph.
// linkType is the edge kind (e.g. "related"); linkSource is a kebab-case
// provenance tag (e.g. "wuphf-capture") — gbrain rejects its reconciliation-
// managed built-ins (markdown/frontmatter/mentions/wikilink-resolved). note is
// optional free-text context. Empty optional fields are omitted.
func (c *Client) AddLink(ctx context.Context, from, to, linkType, linkSource, note string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return fmt.Errorf("gbrain add_link: from and to are required")
	}
	args := map[string]any{"from": from, "to": to}
	if s := strings.TrimSpace(linkType); s != "" {
		args["link_type"] = s
	}
	if s := strings.TrimSpace(linkSource); s != "" {
		args["link_source"] = s
	}
	if s := strings.TrimSpace(note); s != "" {
		args["context"] = s
	}
	if _, err := c.CallTool(ctx, toolAddLink, args); err != nil {
		return fmt.Errorf("gbrain add_link %s->%s: %w", from, to, err)
	}
	return nil
}

// GetLinks lists the outgoing edges from a page. Tolerant of gbrain returning
// either a bare array or a {"links":[...]} envelope.
func (c *Client) GetLinks(ctx context.Context, slug string) ([]Link, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("gbrain get_links: slug is required")
	}
	raw, err := c.CallTool(ctx, toolGetLinks, map[string]any{"slug": slug})
	if err != nil {
		return nil, err
	}
	var links []Link
	if err := decodeJSON(raw, &links); err == nil {
		return links, nil
	}
	var env struct {
		Links []Link `json:"links"`
	}
	if err := decodeJSON(raw, &env); err != nil {
		return nil, fmt.Errorf("decode gbrain get_links: %w", err)
	}
	return env.Links, nil
}

// decodeJSON unmarshals a flattened tool payload into v. It surfaces a clear
// error when the payload was an MCP error string rather than JSON.
func decodeJSON(raw string, v any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "ERROR: ") {
		return errors.New(strings.TrimPrefix(raw, "ERROR: "))
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	return nil
}
