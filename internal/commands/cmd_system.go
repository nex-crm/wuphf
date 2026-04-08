package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/setup"
	"github.com/nex-crm/wuphf/internal/tui/render"
)

// ErrQuit is returned by quit commands so the caller can signal clean exit.
var ErrQuit = errors.New("quit")

func cmdHelp(ctx *SlashContext, args string) error {
	help := "Commands:\n\n" +
		"  /ask <question>        Ask the AI\n" +
		"  /search <query>        Search knowledge base\n" +
		"  /remember <text>       Store information\n\n" +
		"  /object <sub>          list | get | create | update | delete\n" +
		"  /record <sub>          list | get | create | upsert | update | delete | timeline\n" +
		"  /note <sub>            list | get | create | update | delete\n" +
		"  /task <sub>            list | get | create | update | delete\n" +
		"  /list <sub>            list | get | create | delete | records | add-member\n" +
		"  /rel <sub>             list-defs | create-def | create | delete\n" +
		"  /attribute <sub>       create | update | delete\n\n" +
		"  /agent                 list | <slug>\n" +
		"  /graph                 View context graph\n" +
		"  /insights              View insights\n" +
		"  /calendar              View calendar\n\n" +
		"  /config <sub>          show | set | path\n" +
		"  /detect                Detect AI platforms\n" +
		"  /init                  Run setup\n" +
		"  /provider              Switch LLM provider\n\n" +
		"  /help                  This help\n" +
		"  /clear                 Clear messages\n" +
		"  /quit                  Exit WUPHF"
	ctx.AddMessage("system", help)
	return nil
}

func cmdClear(ctx *SlashContext, args string) error {
	ctx.AddMessage("system", "Messages cleared.")
	return nil
}

func cmdQuit(ctx *SlashContext, args string) error {
	return ErrQuit
}

func cmdInit(ctx *SlashContext, args string) error {
	if config.ResolveNoNex() {
		ctx.AddMessage("system", "Nex integration is disabled for this session (--no-nex). Start WUPHF without --no-nex to run setup.")
		return nil
	}

	cfg, _ := config.Load()
	if cfg.APIKey == "" {
		cfg.APIKey = config.ResolveAPIKey("")
	}
	if cfg.LLMProvider == "" {
		cfg.LLMProvider = "claude-code"
	}
	if cfg.Pack == "" {
		cfg.Pack = "founding-team"
	}
	if pack := agent.GetPack(cfg.Pack); pack != nil {
		cfg.TeamLeadSlug = pack.LeadSlug
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	notice, err := setup.InstallLatestCLI()
	if err != nil {
		return err
	}
	ctx.AddMessage("system", notice)
	ctx.AddMessage("system", fmt.Sprintf("Setup defaults saved. Provider: %s | Pack: %s", cfg.LLMProvider, cfg.Pack))
	if cfg.APIKey == "" {
		ctx.AddMessage("system", "No WUPHF API key is configured yet. Run interactive /init inside WUPHF to add one.")
	}
	ctx.AddMessage("system", config.OneSetupBlurb())
	return nil
}

func cmdProvider(ctx *SlashContext, args string) error {
	options := []PickerOption{
		{Label: "Codex CLI", Value: "codex", Description: "Codex via codex CLI"},
		{Label: "Claude Code", Value: "claude-code", Description: "Claude via claude-code CLI"},
	}
	if ctx.ShowPicker != nil {
		ctx.ShowPicker("Switch LLM Provider", options)
		return nil
	}
	var sb strings.Builder
	sb.WriteString("LLM providers:\n")
	for _, opt := range options {
		sb.WriteString(fmt.Sprintf("  • %s — %s\n", opt.Label, opt.Description))
	}
	ctx.AddMessage("system", strings.TrimRight(sb.String(), "\n"))
	return nil
}

// graphResponse is the shape returned by GET /v1/graph.
type graphResponse struct {
	Nodes []graphAPINode `json:"nodes"`
	Edges []graphAPIEdge `json:"edges"`
}

type graphAPINode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type graphAPIEdge struct {
	Source           string `json:"source"`
	Target           string `json:"target"`
	RelationshipName string `json:"relationship_name"`
}

func cmdGraph(ctx *SlashContext, args string) error {
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Get[graphResponse](ctx.APIClient, "/v1/graph?limit=50", 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	if len(result.Nodes) == 0 {
		ctx.AddMessage("system", "No graph data found.")
		return nil
	}

	nodes := make([]render.GraphNode, len(result.Nodes))
	for i, n := range result.Nodes {
		nodes[i] = render.GraphNode{ID: n.ID, Label: n.Name, Type: n.Type}
	}
	edges := make([]render.GraphEdge, len(result.Edges))
	for i, e := range result.Edges {
		edges[i] = render.GraphEdge{From: e.Source, To: e.Target, Label: e.RelationshipName}
	}

	output := render.RenderGraph(nodes, edges, 80, 24)
	ctx.AddMessage("system", output)
	return nil
}

func cmdInsights(ctx *SlashContext, args string) error {
	if !requireAuth(ctx) {
		return nil
	}
	ctx.SetLoading(true)
	result, err := api.Get[[]map[string]any](ctx.APIClient, "/v1/insights?limit=10", 0)
	ctx.SetLoading(false)
	if err != nil {
		return err
	}
	if len(result) == 0 {
		ctx.AddMessage("system", "No insights found.")
		return nil
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	ctx.AddMessage("system", string(b))
	return nil
}

// --- shared helpers ---

func requireAuth(ctx *SlashContext) bool {
	if ctx.APIClient == nil || !ctx.APIClient.IsAuthenticated() {
		if config.ResolveNoNex() {
			ctx.AddMessage("system", "Nex integration is disabled for this session (--no-nex).")
		} else {
			ctx.AddMessage("system", "Not authenticated. Run /init to set up.")
		}
		return false
	}
	return true
}

func formatMapResult(m map[string]any) string {
	for _, key := range []string{"answer", "message", "result", "text"} {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", m)
	}
	return string(b)
}
