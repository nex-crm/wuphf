package team

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/gbrain"
	"github.com/nex-crm/wuphf/internal/nex"
)

type MemoryBackendStatus struct {
	SelectedKind  string
	SelectedLabel string
	ActiveKind    string
	ActiveLabel   string
	Detail        string
	NextStep      string
}

type memoryMCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	EnvVars []string
}

type memoryBackend interface {
	Kind() string
	Label() string
	Ready() bool
	MCPServer() (*memoryMCPServer, error)
	FetchBrief(ctx context.Context, notification string) string
	QueryShared(ctx context.Context, query string, limit int) ([]ScopedMemoryHit, error)
	WriteShared(ctx context.Context, note SharedMemoryWrite) (string, error)
}

type ScopedMemoryHit struct {
	Scope      string   `json:"scope,omitempty"`
	Backend    string   `json:"backend,omitempty"`
	Identifier string   `json:"identifier,omitempty"`
	Title      string   `json:"title,omitempty"`
	Snippet    string   `json:"snippet,omitempty"`
	OwnerSlug  string   `json:"owner_slug,omitempty"`
	Slug       string   `json:"slug,omitempty"`
	PageID     int      `json:"page_id,omitempty"`
	ChunkID    int      `json:"chunk_id,omitempty"`
	ChunkIndex int      `json:"chunk_index,omitempty"`
	Source     string   `json:"source,omitempty"`
	Score      *float64 `json:"score,omitempty"`
	Stale      *bool    `json:"stale,omitempty"`
}

type SharedMemoryWrite struct {
	Actor   string
	Key     string
	Title   string
	Content string
}

type noMemoryBackend struct{}

func (noMemoryBackend) Kind() string  { return config.MemoryBackendNone }
func (noMemoryBackend) Label() string { return config.MemoryBackendLabel(config.MemoryBackendNone) }
func (noMemoryBackend) Ready() bool   { return true }
func (noMemoryBackend) MCPServer() (*memoryMCPServer, error) {
	return nil, nil
}
func (noMemoryBackend) FetchBrief(context.Context, string) string { return "" }
func (noMemoryBackend) QueryShared(context.Context, string, int) ([]ScopedMemoryHit, error) {
	return nil, nil
}
func (noMemoryBackend) WriteShared(context.Context, SharedMemoryWrite) (string, error) {
	return "", fmt.Errorf("shared external memory is not active for this run")
}

type nexMemoryBackend struct{}

func (nexMemoryBackend) Kind() string  { return config.MemoryBackendNex }
func (nexMemoryBackend) Label() string { return config.MemoryBackendLabel(config.MemoryBackendNex) }
func (nexMemoryBackend) Ready() bool {
	return strings.TrimSpace(config.ResolveAPIKey("")) != "" && nexMCPBinaryPath() != ""
}
func (nexMemoryBackend) MCPServer() (*memoryMCPServer, error) {
	bin := nexMCPBinaryPath()
	if bin == "" {
		return nil, nil
	}
	apiKey := strings.TrimSpace(config.ResolveAPIKey(""))
	if apiKey == "" {
		return nil, nil
	}
	return &memoryMCPServer{
		Name:    "nex",
		Command: bin,
		Env: map[string]string{
			"WUPHF_API_KEY": apiKey,
			"NEX_API_KEY":   apiKey,
		},
		EnvVars: []string{"WUPHF_API_KEY", "NEX_API_KEY"},
	}, nil
}
func (nexMemoryBackend) FetchBrief(ctx context.Context, notification string) string {
	if !nex.Connected() {
		return ""
	}
	query := strings.TrimSpace(notification)
	if query == "" {
		return ""
	}
	if len(query) > 400 {
		query = query[:400]
	}
	answer, err := nex.Recall(ctx, query)
	if err != nil || strings.TrimSpace(answer) == "" {
		return ""
	}
	return "== NEX CONTEXT ==\n" + strings.TrimSpace(answer) + "\n== END NEX CONTEXT =="
}
func (nexMemoryBackend) QueryShared(ctx context.Context, query string, limit int) ([]ScopedMemoryHit, error) {
	client := api.NewClient(strings.TrimSpace(config.ResolveAPIKey("")))
	if !client.IsAuthenticated() {
		return nil, fmt.Errorf("nex is not configured")
	}
	type askResponse struct {
		Answer string `json:"answer"`
	}
	resp, err := api.Post[askResponse](client, "/v1/context/ask", map[string]any{
		"query": strings.TrimSpace(query),
	}, 0)
	if err != nil || strings.TrimSpace(resp.Answer) == "" {
		return nil, err
	}
	return []ScopedMemoryHit{{
		Scope:      "shared",
		Backend:    config.MemoryBackendNex,
		Identifier: "nex-context",
		Title:      "Nex context",
		Snippet:    strings.TrimSpace(resp.Answer),
		OwnerSlug:  inferSharedMemoryOwner("", strings.TrimSpace(resp.Answer)),
	}}, nil
}
func (nexMemoryBackend) WriteShared(ctx context.Context, note SharedMemoryWrite) (string, error) {
	client := api.NewClient(strings.TrimSpace(config.ResolveAPIKey("")))
	if !client.IsAuthenticated() {
		return "", fmt.Errorf("nex is not configured")
	}
	content := renderNexSharedMemoryContent(note)
	if _, err := api.Post[map[string]any](client, "/v1/context/text", map[string]any{
		"content": content,
	}, 0); err != nil {
		return "", err
	}
	key := strings.TrimSpace(note.Key)
	if key == "" {
		key = slugify(firstNonEmpty(note.Title, note.Content))
	}
	if key == "" {
		key = "shared-note"
	}
	return key, nil
}

// gbrainMemoryClient is the narrow slice of *gbrain.Client the shared-memory
// backend depends on. Reducing it to the two methods the backend actually
// calls lets tests inject a fake (no live gbrain subprocess) and keeps the
// backend decoupled from the full MCP surface.
type gbrainMemoryClient interface {
	Query(ctx context.Context, query string, limit int) ([]gbrain.SearchResult, error)
	PutPage(ctx context.Context, content string, opts gbrain.PutOptions) (gbrain.PutResult, error)
	// AddLink wires a graph edge; used by capture to associate a freshly
	// written page with related pages immediately (not only via gbrain's async
	// dream-cycle).
	AddLink(ctx context.Context, from, to, linkType, linkSource, note string) error
}

// gbrainSharedMemorySourceKind labels shared-memory writes for provenance on
// the gbrain side. The legacy CLI put_page call did not set source_kind; the
// MCP client carries it so gbrain can attribute these pages to WUPHF's
// agent-authored shared memory rather than human-curated wiki content.
const gbrainSharedMemorySourceKind = "wuphf_shared_memory"

// sharedGBrainClientMu guards the process-wide gbrain MCP client the broker
// owns. The broker constructs one client on Start and registers it here so the
// package-level memory entry points (QuerySharedMemory / WriteSharedMemory /
// FetchBrief) — which run without a *Broker handle — can reach it. Cleared on
// broker Stop. nil until a broker registers one.
var (
	sharedGBrainClientMu sync.RWMutex
	sharedGBrainClientV  gbrainMemoryClient

	// defaultGBrainClientOnce lazily builds a fallback client for paths that
	// run without a broker (standalone teammcp callers, focused tests). The
	// broker-owned client is always preferred; this is only a safety net so a
	// gbrain-backed call never nil-panics. Like the broker client it connects
	// lazily, so an absent gbrain costs nothing until first use.
	defaultGBrainClientOnce sync.Once
	defaultGBrainClientV    *gbrain.Client
)

// setSharedGBrainClient registers (or, with nil, clears) the broker-owned
// gbrain client for the package-level memory entry points.
func setSharedGBrainClient(client gbrainMemoryClient) {
	sharedGBrainClientMu.Lock()
	defer sharedGBrainClientMu.Unlock()
	sharedGBrainClientV = client
}

// sharedGBrainClient returns the broker-registered client, or nil if no broker
// has registered one yet.
func sharedGBrainClient() gbrainMemoryClient {
	sharedGBrainClientMu.RLock()
	defer sharedGBrainClientMu.RUnlock()
	return sharedGBrainClientV
}

// defaultGBrainClient lazily constructs the no-broker fallback client.
func defaultGBrainClient() *gbrain.Client {
	defaultGBrainClientOnce.Do(func() {
		defaultGBrainClientV = gbrain.NewClient()
	})
	return defaultGBrainClientV
}

type gbrainMemoryBackend struct {
	// client overrides the shared/broker client. Tests inject a fake here;
	// it is nil in production, where resolveClient falls back to the
	// broker-owned client (or a lazy default).
	client gbrainMemoryClient
}

// resolveClient returns the gbrain client this backend should talk to: an
// injected client (tests) first, then the broker-owned client, then a lazily
// built fallback so a gbrain-backed call never nil-panics.
func (b gbrainMemoryBackend) resolveClient() gbrainMemoryClient {
	if b.client != nil {
		return b.client
	}
	if shared := sharedGBrainClient(); shared != nil {
		return shared
	}
	return defaultGBrainClient()
}

func (b gbrainMemoryBackend) Kind() string { return config.MemoryBackendGBrain }
func (b gbrainMemoryBackend) Label() string {
	return config.MemoryBackendLabel(config.MemoryBackendGBrain)
}
func (b gbrainMemoryBackend) Ready() bool {
	return gbrain.IsInstalled() && gbrain.EmbeddingAvailable()
}
func (b gbrainMemoryBackend) MCPServer() (*memoryMCPServer, error) {
	bin := gbrain.BinaryPath()
	if bin == "" {
		return nil, nil
	}
	return &memoryMCPServer{
		Name:    "gbrain",
		Command: bin,
		Args:    []string{"serve"},
		Env:     gbrainMCPEnv(),
		EnvVars: gbrainMCPEnvVars(),
	}, nil
}
func (b gbrainMemoryBackend) FetchBrief(ctx context.Context, notification string) string {
	query := strings.TrimSpace(notification)
	if query == "" {
		return ""
	}
	if len(query) > 400 {
		query = query[:400]
	}
	results, err := b.resolveClient().Query(ctx, query, 5)
	if err != nil || len(results) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "== GBRAIN CONTEXT ==")
	seen := map[string]struct{}{}
	for _, result := range results {
		if _, ok := seen[result.Slug]; ok {
			continue
		}
		seen[result.Slug] = struct{}{}
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = strings.TrimSpace(result.Slug)
		}
		snippet := strings.TrimSpace(strings.ReplaceAll(result.ChunkText, "\n", " "))
		if snippet == "" {
			snippet = "Relevant context found in the brain."
		}
		lines = append(lines, fmt.Sprintf("- %s (%s): %s", title, strings.TrimSpace(result.Type), truncate(snippet, 220)))
		if len(lines) >= 4 {
			break
		}
	}
	if len(lines) == 1 {
		return ""
	}
	lines = append(lines, "== END GBRAIN CONTEXT ==")
	return strings.Join(lines, "\n")
}
func (b gbrainMemoryBackend) QueryShared(ctx context.Context, query string, limit int) ([]ScopedMemoryHit, error) {
	if limit <= 0 {
		limit = 5
	}
	results, err := b.resolveClient().Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]ScopedMemoryHit, 0, len(results))
	seen := map[string]struct{}{}
	for _, result := range results {
		if _, ok := seen[result.Slug]; ok {
			continue
		}
		seen[result.Slug] = struct{}{}
		hits = append(hits, scopedMemoryHitFromGBrainResult(result))
		if len(hits) >= limit && limit > 0 {
			break
		}
	}
	return hits, nil
}

func scopedMemoryHitFromGBrainResult(result gbrain.SearchResult) ScopedMemoryHit {
	title := strings.TrimSpace(result.Title)
	if title == "" {
		title = strings.TrimSpace(result.Slug)
	}
	snippet := strings.TrimSpace(strings.ReplaceAll(result.ChunkText, "\n", " "))
	if snippet == "" {
		snippet = "Relevant context found in the brain."
	}
	score := result.Score
	stale := result.Stale
	slug := strings.TrimSpace(result.Slug)
	return ScopedMemoryHit{
		Scope:      "shared",
		Backend:    config.MemoryBackendGBrain,
		Identifier: slug,
		Title:      title,
		Snippet:    truncate(snippet, 220),
		OwnerSlug:  inferSharedMemoryOwner(slug, snippet),
		Slug:       slug,
		PageID:     result.PageID,
		ChunkID:    result.ChunkID,
		ChunkIndex: result.ChunkIndex,
		Source:     strings.TrimSpace(result.ChunkSource),
		Score:      &score,
		Stale:      &stale,
	}
}
func (b gbrainMemoryBackend) WriteShared(ctx context.Context, note SharedMemoryWrite) (string, error) {
	// Slug derivation is unchanged from the legacy CLI path: gbrain's put_page
	// requires a slug, so the backend has always derived a kebab slug here
	// rather than relying on gbrain to infer one from the frontmatter title.
	slug := slugify(firstNonEmpty(note.Key, note.Title, note.Content))
	if slug == "" {
		slug = "shared-note"
	}
	actor := slugify(firstNonEmpty(note.Actor, "wuphf"))
	slug = fmt.Sprintf("wuphf-shared--%s--%s--%s", actor, slug, time.Now().UTC().Format("20060102-150405"))
	content := renderGBrainSharedMemoryPage(slug, note)
	if _, err := b.resolveClient().PutPage(ctx, content, gbrain.PutOptions{
		Slug:       slug,
		SourceKind: gbrainSharedMemorySourceKind,
	}); err != nil {
		return "", fmt.Errorf("write shared gbrain memory: %w", err)
	}
	return slug, nil
}

// gbrainOpenAIConfigured reports whether an OpenAI key is configured. OpenAI is
// the strongest gbrain embedder (one key serves both chat and embeddings); it
// distinguishes the OpenAI status detail from the local-Ollama detail.
func gbrainOpenAIConfigured() bool {
	return strings.TrimSpace(config.ResolveOpenAIAPIKey()) != ""
}

func ResolveMemoryBackendStatus() MemoryBackendStatus {
	selected := config.ResolveMemoryBackend("")
	status := MemoryBackendStatus{
		SelectedKind:  selected,
		SelectedLabel: config.MemoryBackendLabel(selected),
		ActiveKind:    config.MemoryBackendNone,
		ActiveLabel:   config.MemoryBackendLabel(config.MemoryBackendNone),
	}

	switch selected {
	case config.MemoryBackendNone:
		status.ActiveKind = config.MemoryBackendNone
		status.ActiveLabel = config.MemoryBackendLabel(config.MemoryBackendNone)
		if config.ResolveNoNex() {
			status.Detail = "Nex is disabled for this run, so the office is operating without an external memory backend."
			status.NextStep = "Restart without --no-nex or select --memory-backend gbrain when you want external context."
		} else {
			status.Detail = "External memory is disabled for this run."
			status.NextStep = "Set --memory-backend nex or --memory-backend gbrain to enable organizational context."
		}
	case config.MemoryBackendNex:
		if strings.TrimSpace(config.ResolveAPIKey("")) == "" {
			status.Detail = "Nex backend selected, but no WUPHF/Nex API key is configured."
			status.NextStep = "Run /init or set WUPHF_API_KEY to enable Nex-backed context."
			return status
		}
		if nexMCPBinaryPath() == "" {
			status.Detail = "Nex backend selected, but the nex-mcp server is not installed."
			status.NextStep = "Install the latest Nex CLI bundle so the Nex MCP server is available."
			return status
		}
		status.ActiveKind = config.MemoryBackendNex
		status.ActiveLabel = config.MemoryBackendLabel(config.MemoryBackendNex)
		status.Detail = "Nex-backed organizational context is configured."
	case config.MemoryBackendMarkdown:
		// Markdown is the file-over-app default: a git repo under
		// ~/.wuphf/wiki. No API keys, no external service. Always "ready"
		// as long as `git` is on PATH — and if it isn't, Repo.Init surfaces
		// ErrGitUnavailable at launch, so we don't need to guard here.
		status.ActiveKind = config.MemoryBackendMarkdown
		status.ActiveLabel = config.MemoryBackendLabel(config.MemoryBackendMarkdown)
		status.Detail = "Markdown-backed team wiki at ~/.wuphf/wiki is configured. Every edit commits to git with per-agent authorship."
	case config.MemoryBackendGBrain:
		if !gbrain.EmbeddingAvailable() {
			// gbrain needs an embedding provider for semantic retrieval.
			// Anthropic has no embeddings API, so an Anthropic key alone does
			// not qualify. Without an embedder gbrain can only run keyword-only,
			// which is ~equivalent to the markdown wiki, so we report it
			// inactive and steer the user toward a real embedder.
			status.Detail = "GBrain backend selected, but no embedding provider is configured. Semantic vector search needs an OpenAI key or a local Ollama embedding model; an Anthropic key alone has no embeddings API."
			status.NextStep = "Run /init and add an OpenAI key for full GBrain vector search, or pull a local Ollama embedding model (e.g. nomic-embed-text)."
			return status
		}
		if !gbrain.IsInstalled() {
			status.Detail = "GBrain backend selected, but the gbrain CLI is not installed."
			status.NextStep = "Install GBrain and initialize a brain before launching the office."
			return status
		}
		status.ActiveKind = config.MemoryBackendGBrain
		status.ActiveLabel = config.MemoryBackendLabel(config.MemoryBackendGBrain)
		if gbrainOpenAIConfigured() {
			status.Detail = "GBrain-backed organizational context is configured with an OpenAI key, so embeddings and vector search are available."
		} else {
			status.Detail = "GBrain-backed organizational context is configured with a local Ollama embedding model, so embeddings and vector search run on your machine."
		}
	default:
		status.SelectedKind = config.MemoryBackendNone
		status.SelectedLabel = config.MemoryBackendLabel(config.MemoryBackendNone)
		status.Detail = "External memory is disabled for this run."
		status.NextStep = "Select a supported memory backend to enable external context."
	}

	return status
}

func selectedMemoryBackend() memoryBackend {
	switch config.ResolveMemoryBackend("") {
	case config.MemoryBackendNex:
		return nexMemoryBackend{}
	case config.MemoryBackendGBrain:
		return gbrainMemoryBackend{}
	default:
		return noMemoryBackend{}
	}
}

func activeMemoryBackend() memoryBackend {
	backend := selectedMemoryBackend()
	if backend.Ready() {
		return backend
	}
	return noMemoryBackend{}
}

func activeMemoryBackendKind() string {
	return activeMemoryBackend().Kind()
}

func shouldPollNexNotifications() bool {
	return activeMemoryBackendKind() == config.MemoryBackendNex
}

func fetchMemoryBrief(ctx context.Context, notification string) string {
	return activeMemoryBackend().FetchBrief(ctx, notification)
}

func QuerySharedMemory(ctx context.Context, query string, limit int) ([]ScopedMemoryHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	backend := activeMemoryBackend()
	if backend.Kind() == config.MemoryBackendNone {
		return nil, nil
	}
	return backend.QueryShared(ctx, query, limit)
}

func WriteSharedMemory(ctx context.Context, note SharedMemoryWrite) (string, error) {
	note.Content = strings.TrimSpace(note.Content)
	if note.Content == "" {
		return "", fmt.Errorf("content is required")
	}
	return activeMemoryBackend().WriteShared(ctx, note)
}

func resolvedMemoryMCPServer() (*memoryMCPServer, error) {
	return activeMemoryBackend().MCPServer()
}

func nexMCPBinaryPath() string {
	path, err := exec.LookPath("nex-mcp")
	if err != nil {
		return ""
	}
	return path
}

func gbrainMCPEnv() map[string]string {
	env := map[string]string{}
	// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — gbrain is a
	// user-global MCP subprocess that needs the real HOME for its own auth.
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		env["HOME"] = home
	}
	if key := strings.TrimSpace(config.ResolveOpenAIAPIKey()); key != "" {
		env["OPENAI_API_KEY"] = key
	}
	if key := strings.TrimSpace(config.ResolveAnthropicAPIKey()); key != "" {
		env["ANTHROPIC_API_KEY"] = key
	}
	return env
}

func gbrainMCPEnvVars() []string {
	var envVars []string
	// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — gbrain is a
	// user-global MCP subprocess; HOME is passed through for subprocess auth.
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		envVars = append(envVars, "HOME")
	}
	if key := strings.TrimSpace(config.ResolveOpenAIAPIKey()); key != "" {
		envVars = append(envVars, "OPENAI_API_KEY")
	}
	if key := strings.TrimSpace(config.ResolveAnthropicAPIKey()); key != "" {
		envVars = append(envVars, "ANTHROPIC_API_KEY")
	}
	return envVars
}

func directMemoryPromptBlock() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "Memory scopes:\n- team_memory_query: Read your private notes (`scope=private`) or shared org memory backed by Nex (`scope=shared`)\n- team_memory_write: Store private notes by default; only write shared memory after a durable outcome is real\n- team_memory_promote: Copy one of your private notes into shared Nex memory when it becomes canonical\n- If shared memory points at another agent, ask them in the office for fresher working detail instead of guessing\n\n"
	case config.MemoryBackendGBrain:
		return "Memory scopes:\n- team_memory_query: Read your private notes (`scope=private`) or shared org memory backed by GBrain (`scope=shared`)\n- team_memory_write: Store private notes by default; only write shared memory after a durable outcome is real\n- team_memory_promote: Copy one of your private notes into shared GBrain memory when it becomes canonical\n- If shared memory points at another agent, ask them in the office for fresher working detail instead of guessing\n\n"
	default:
		return "Memory scopes:\n- team_memory_query: Your private notes still work with `scope=private`\n- team_memory_write: Store private notes for yourself\n- Shared org memory is not active for this run, so `scope=shared` and team_memory_promote are unavailable\n\n"
	}
}

func directMemoryStorageRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "7. Keep scratch notes private by default. Only claim shared storage after team_memory_write visibility=shared or team_memory_promote actually succeeded.\n"
	case config.MemoryBackendGBrain:
		return "7. Keep scratch notes private by default. Only claim shared storage after team_memory_write visibility=shared or team_memory_promote actually succeeded.\n"
	default:
		return "7. Do not pretend anything was stored outside your private note scope.\n"
	}
}

func leadMemoryPromptBlock() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "Memory scopes: use team_memory_query with scope=shared for org memory backed by Nex, scope=private for your own notes, and team_memory_promote when a private note becomes durable shared knowledge. If shared memory points at another agent, ask them in the office for the freshest working context.\n\n"
	case config.MemoryBackendGBrain:
		return "Memory scopes: use team_memory_query with scope=shared for org memory backed by GBrain, scope=private for your own notes, and team_memory_promote when a private note becomes durable shared knowledge. If shared memory points at another agent, ask them in the office for the freshest working context. Keep task coordination in the office, not in shared memory.\n\n"
	default:
		return "Shared org memory is not active for this run. You can still use private notes with team_memory_query/team_memory_write scope=private.\n\n"
	}
}

func leadMemoryFirstRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "1. On strategy or prior decisions, call team_memory_query early. Use scope=shared for org memory and scope=private for your own retained notes.\n"
	case config.MemoryBackendGBrain:
		return "1. On strategy, relationships, or prior decisions, start with team_memory_query. Use shared scope for org context and private scope for your own retained notes.\n"
	default:
		return "1. Coordinate inside the office channel first, and use private memory only for your own scratch history.\n"
	}
}

func leadMemoryStorageRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "8. When you lock a durable decision, promote it into shared memory before claiming it is stored\n"
	case config.MemoryBackendGBrain:
		return "8. When you lock a durable decision, promote it into shared memory before claiming the brain knows it\n"
	default:
		return "8. Summarize final decisions clearly in-channel; shared org memory is unavailable in this run\n"
	}
}

func leadMemoryFinalWarning() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "Do not pretend shared memory was updated; verify team_memory_write visibility=shared or team_memory_promote succeeded.\n"
	case config.MemoryBackendGBrain:
		return "Do not pretend shared memory was updated; verify team_memory_write visibility=shared or team_memory_promote succeeded.\n"
	default:
		return "Do not claim you stored anything outside your private notes.\n"
	}
}

func specialistMemoryPromptBlock() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "Memory scopes: use team_memory_query with scope=shared for org memory backed by Nex, scope=private for your own notes, and team_memory_promote when a private note becomes durable shared knowledge. If shared memory points at another agent, ask them in the office for the freshest working context.\n\n"
	case config.MemoryBackendGBrain:
		return "Memory scopes: use team_memory_query with scope=shared for org memory backed by GBrain, scope=private for your own notes, and team_memory_promote when a private note becomes durable shared knowledge. If shared memory points at another agent, ask them in the office for the freshest working context.\n\n"
	default:
		return "Shared org memory is not active for this run. You can still use private notes with team_memory_query/team_memory_write scope=private.\n\n"
	}
}

func specialistMemoryStorageRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "9. Use team_memory_query when prior knowledge matters. Keep notes private by default, and only promote durable conclusions into shared memory once they are real.\n\n"
	case config.MemoryBackendGBrain:
		return "9. Use team_memory_query when prior knowledge matters. Keep notes private by default, and only promote durable conclusions into shared memory once they are real.\n\n"
	default:
		return "9. Don't fake shared memory. Surface uncertainty in-channel and keep any retained notes private.\n\n"
	}
}

func renderNexSharedMemoryContent(note SharedMemoryWrite) string {
	title := strings.TrimSpace(note.Title)
	if title == "" {
		title = firstNonEmpty(strings.TrimSpace(note.Key), "WUPHF shared memory")
	}
	actor := strings.TrimSpace(note.Actor)
	if actor == "" {
		actor = "wuphf"
	}
	return fmt.Sprintf("[WUPHF shared memory]\nTitle: %s\nAuthor: @%s\nRecorded at: %s\n\n%s",
		title,
		actor,
		time.Now().UTC().Format(time.RFC3339),
		strings.TrimSpace(note.Content),
	)
}

func renderGBrainSharedMemoryPage(slug string, note SharedMemoryWrite) string {
	title := strings.TrimSpace(note.Title)
	if title == "" {
		title = strings.TrimSpace(note.Key)
	}
	if title == "" {
		title = "WUPHF shared memory"
	}
	actor := strings.TrimSpace(note.Actor)
	if actor == "" {
		actor = "wuphf"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	yamlTitle := strings.ReplaceAll(title, `"`, `\"`)
	return fmt.Sprintf(`---
title: "%s"
type: note
tags:
  - wuphf
  - shared-memory
  - agent-%s
slug: %s
updated_at: %s
---

# %s

Recorded by @%s on %s.

%s
	`,
		yamlTitle,
		slugify(actor),
		slug,
		now,
		title,
		actor,
		now,
		strings.TrimSpace(note.Content),
	)
}

func inferSharedMemoryOwner(identifier string, snippet string) string {
	if owner := parseGBrainSharedMemoryOwner(identifier); owner != "" {
		return owner
	}
	for _, marker := range []string{"author: @", "recorded by @"} {
		if owner := parseSharedMemoryOwnerAfterMarker(snippet, marker); owner != "" {
			return owner
		}
	}
	return ""
}

func parseGBrainSharedMemoryOwner(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return ""
	}
	parts := strings.Split(identifier, "--")
	if len(parts) < 4 {
		return ""
	}
	if strings.TrimSpace(parts[0]) != "wuphf-shared" {
		return ""
	}
	return slugify(parts[1])
}

func parseSharedMemoryOwnerAfterMarker(text string, marker string) string {
	text = strings.TrimSpace(text)
	marker = strings.ToLower(strings.TrimSpace(marker))
	if text == "" || marker == "" {
		return ""
	}
	lower := strings.ToLower(text)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	if start >= len(text) {
		return ""
	}
	var b strings.Builder
	for _, r := range text[start:] {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_':
			b.WriteRune(unicode.ToLower(r))
		default:
			value := slugify(b.String())
			if value != "" {
				return value
			}
			return ""
		}
	}
	return slugify(b.String())
}
