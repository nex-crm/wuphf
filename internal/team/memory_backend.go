package team

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
}

type noMemoryBackend struct{}

func (noMemoryBackend) Kind() string  { return config.MemoryBackendNone }
func (noMemoryBackend) Label() string { return config.MemoryBackendLabel(config.MemoryBackendNone) }
func (noMemoryBackend) Ready() bool   { return true }
func (noMemoryBackend) MCPServer() (*memoryMCPServer, error) {
	return nil, nil
}
func (noMemoryBackend) FetchBrief(context.Context, string) string { return "" }

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

type gbrainMemoryBackend struct{}

func (gbrainMemoryBackend) Kind() string { return config.MemoryBackendGBrain }
func (gbrainMemoryBackend) Label() string {
	return config.MemoryBackendLabel(config.MemoryBackendGBrain)
}
func (gbrainMemoryBackend) Ready() bool { return gbrain.IsInstalled() && gbrainProviderKeyConfigured() }
func (gbrainMemoryBackend) MCPServer() (*memoryMCPServer, error) {
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
func (gbrainMemoryBackend) FetchBrief(ctx context.Context, notification string) string {
	query := strings.TrimSpace(notification)
	if query == "" {
		return ""
	}
	if len(query) > 400 {
		query = query[:400]
	}
	results, err := gbrain.Query(ctx, query, 5)
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

func gbrainProviderKeyConfigured() bool {
	return strings.TrimSpace(config.ResolveOpenAIAPIKey()) != "" ||
		strings.TrimSpace(config.ResolveAnthropicAPIKey()) != ""
}

func gbrainOpenAIConfigured() bool {
	return strings.TrimSpace(config.ResolveOpenAIAPIKey()) != ""
}

func gbrainAnthropicConfigured() bool {
	return strings.TrimSpace(config.ResolveAnthropicAPIKey()) != ""
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
	case config.MemoryBackendGBrain:
		if !gbrainProviderKeyConfigured() {
			status.Detail = "GBrain backend selected, but no provider key is configured. OpenAI is required for embeddings and vector search; Anthropic alone only enables reduced-mode retrieval."
			status.NextStep = "Run /init and add an OpenAI key for full GBrain search, or add an Anthropic key for reduced mode."
			return status
		}
		if !gbrain.IsInstalled() {
			status.Detail = "GBrain backend selected, but the gbrain CLI is not installed."
			status.NextStep = "Install GBrain and initialize a brain before launching the office."
			return status
		}
		status.ActiveKind = config.MemoryBackendGBrain
		status.ActiveLabel = config.MemoryBackendLabel(config.MemoryBackendGBrain)
		switch {
		case gbrainOpenAIConfigured():
			status.Detail = "GBrain-backed organizational context is configured with an OpenAI key, so embeddings and vector search are available."
		case gbrainAnthropicConfigured():
			status.Detail = "GBrain-backed organizational context is configured in Anthropic-only mode. Keyword search and query expansion work, but embeddings and vector search still require OpenAI."
			status.NextStep = "Add WUPHF_OPENAI_API_KEY if you want full GBrain embeddings and vector search."
		default:
			status.Detail = "GBrain-backed organizational context is configured with provider credentials."
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
		return "Use the Nex context graph when it materially helps:\n- query_context: Look up prior decisions, people, projects, and history before guessing\n- add_context: Store durable conclusions only after you have actually landed them\n\n"
	case config.MemoryBackendGBrain:
		return "Use GBrain as durable world knowledge when it materially helps:\n- query: Brain-first semantic lookup for people, projects, decisions, and patterns\n- search: Exact term or slug lookup when you already know the entity\n- get_page: Load the full page once a hit looks relevant\n- put_page or add_timeline_entry: Write durable world knowledge only after it actually lands; keep task chatter in the conversation\n\n"
	default:
		return "External memory is not active for this run. Base your work on the conversation and direct human answers only.\n\n"
	}
}

func directMemoryStorageRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "7. If Nex is enabled, do not claim something is stored unless add_context actually succeeded.\n"
	case config.MemoryBackendGBrain:
		return "7. If GBrain is enabled, do not claim the brain was updated unless put_page or add_timeline_entry actually succeeded.\n"
	default:
		return "7. Do not pretend anything was stored outside this session.\n"
	}
}

func leadMemoryPromptBlock() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "Nex memory: query_context before reinventing; add_context only after a decision is actually landed.\n\n"
	case config.MemoryBackendGBrain:
		return "GBrain operating loop: query before reinventing, search when you know the entity, get_page before relying on a hit, and only write durable world knowledge back with put_page or add_timeline_entry once it has landed. Keep task coordination in the office, not in the brain.\n\n"
	default:
		return "External memory is not active for this run. Work only with the shared office channel and human answers.\n\n"
	}
}

func leadMemoryFirstRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "1. On strategy or prior decisions, call query_context early\n"
	case config.MemoryBackendGBrain:
		return "1. On strategy, relationships, or prior decisions, start in GBrain: query broadly, then search or get_page to verify the relevant entity page\n"
	default:
		return "1. Coordinate inside the office channel first and keep the team aligned there\n"
	}
}

func leadMemoryStorageRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "8. When you lock a decision, call add_context before claiming it is stored\n"
	case config.MemoryBackendGBrain:
		return "8. When you lock a durable decision, update the relevant page or timeline before claiming the brain knows it\n"
	default:
		return "8. Summarize final decisions clearly in-channel\n"
	}
}

func leadMemoryFinalWarning() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "Do not pretend the graph was updated; verify add_context succeeded.\n"
	case config.MemoryBackendGBrain:
		return "Do not pretend the brain was updated; verify put_page or add_timeline_entry succeeded.\n"
	default:
		return "Do not claim you stored anything outside the office.\n"
	}
}

func specialistMemoryPromptBlock() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "Nex memory: query_context before making assumptions; add_context only for durable conclusions.\n\n"
	case config.MemoryBackendGBrain:
		return "GBrain operating loop: query when prior knowledge matters, search when you already know the entity, get_page before leaning on a hit, and only write durable world knowledge back with put_page or add_timeline_entry once the outcome has actually landed.\n\n"
	default:
		return "External memory is not active for this run. Base your work on the office conversation and direct human answers only.\n\n"
	}
}

func specialistMemoryStorageRule() string {
	switch activeMemoryBackendKind() {
	case config.MemoryBackendNex:
		return "9. Use query_context when prior knowledge matters. Only use add_context for durable conclusions, and don't claim something stored unless add_context actually succeeded.\n\n"
	case config.MemoryBackendGBrain:
		return "9. Use GBrain when prior knowledge matters. Query first, verify with get_page when needed, and only write durable world knowledge back with put_page or add_timeline_entry after the outcome is real.\n\n"
	default:
		return "9. Don't fake outside memory. Surface uncertainty in-channel and keep outcomes explicit in-thread.\n\n"
	}
}
