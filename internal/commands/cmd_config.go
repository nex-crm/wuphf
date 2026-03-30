package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

func cmdConfig(ctx *SlashContext, args string) error {
	positional, _ := parseFlags(args)

	sub := "show"
	if len(positional) > 0 {
		sub = positional[0]
	}

	switch sub {
	case "show":
		return configShow(ctx)
	case "set":
		if len(positional) < 3 {
			ctx.AddMessage("system", "Usage: /config set <key> <value>")
			return nil
		}
		return configSet(ctx, positional[1], positional[2])
	case "path":
		ctx.AddMessage("system", config.ConfigPath())
		return nil
	default:
		ctx.AddMessage("system", "Unknown subcommand: "+sub+"\nUsage: /config [show|set|path]")
		return nil
	}
}

func configShow(ctx *SlashContext) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	masked := "(not set)"
	if cfg.APIKey != "" {
		if len(cfg.APIKey) > 8 {
			masked = cfg.APIKey[:4] + "…" + cfg.APIKey[len(cfg.APIKey)-4:]
		} else {
			masked = "****"
		}
	}
	workspace := cfg.WorkspaceSlug
	if workspace == "" {
		workspace = cfg.WorkspaceID
	}
	if workspace == "" {
		workspace = "(not set)"
	}

	provider := cfg.LLMProvider
	if provider == "" {
		provider = "(not set)"
	}

	pack := cfg.Pack
	if pack == "" {
		pack = "(not set)"
	}

	baseURL := config.BaseURL()
	actionProvider := config.ResolveActionProvider()
	if actionProvider == "" {
		actionProvider = "auto"
	}

	var sb strings.Builder
	sb.WriteString("Configuration:\n")
	sb.WriteString(fmt.Sprintf("  API Key:   %s\n", masked))
	sb.WriteString(fmt.Sprintf("  Integrations: %s\n", config.OneSetupSummary()))
	sb.WriteString(fmt.Sprintf("  Action provider: %s\n", actionProvider))
	sb.WriteString(fmt.Sprintf("  Workspace: %s\n", workspace))
	sb.WriteString(fmt.Sprintf("  Provider:  %s\n", provider))
	sb.WriteString(fmt.Sprintf("  Pack:      %s\n", pack))
	sb.WriteString(fmt.Sprintf("  Base URL:  %s", baseURL))
	ctx.AddMessage("system", sb.String())
	return nil
}

func configSet(ctx *SlashContext, key, value string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	switch key {
	case "api_key":
		cfg.APIKey = value
	case "composio_api_key":
		cfg.ComposioAPIKey = value
	case "action_provider":
		cfg.ActionProvider = value
	case "workspace_id":
		cfg.WorkspaceID = value
	case "workspace_slug":
		cfg.WorkspaceSlug = value
	case "llm_provider":
		cfg.LLMProvider = value
	case "gemini_api_key":
		cfg.GeminiAPIKey = value
	case "pack":
		cfg.Pack = value
	case "team_lead_slug":
		cfg.TeamLeadSlug = value
	case "dev_url":
		cfg.DevURL = value
	case "default_format":
		cfg.DefaultFormat = value
	default:
		ctx.AddMessage("system", "Unknown config key: "+key+
			"\nValid keys: api_key, composio_api_key, action_provider, workspace_id, workspace_slug, llm_provider, gemini_api_key, pack, team_lead_slug, dev_url, default_format")
		return nil
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	ctx.AddMessage("system", fmt.Sprintf("Set %s = %s", key, value))
	return nil
}

// cmdDetect checks for installed AI platform CLIs.
func cmdDetect(ctx *SlashContext, args string) error {
	platforms := []struct {
		name string
		cmd  string
	}{
		{"Claude", "claude"},
		{"Cursor", "cursor"},
		{"Windsurf", "windsurf"},
		{"VS Code", "code"},
		{"Cline", "cline"},
		{"Aider", "aider"},
	}

	var sb strings.Builder
	sb.WriteString("AI platform detection:\n")
	found := 0
	for _, p := range platforms {
		path, err := exec.LookPath(p.cmd)
		if err == nil {
			sb.WriteString(fmt.Sprintf("  ✓ %s — %s\n", p.name, path))
			found++
		} else {
			sb.WriteString(fmt.Sprintf("  ✗ %s — not found\n", p.name))
		}
	}
	sb.WriteString(fmt.Sprintf("\n%d of %d platforms detected.", found, len(platforms)))
	ctx.AddMessage("system", sb.String())
	return nil
}

// cmdSession handles session management subcommands.
func cmdSession(ctx *SlashContext, args string) error {
	positional, _ := parseFlags(args)

	sub := "list"
	if len(positional) > 0 {
		sub = positional[0]
	}

	switch sub {
	case "list":
		ctx.AddMessage("system", "Session management — coming soon.")
	case "clear":
		ctx.AddMessage("system", "Sessions cleared.")
	default:
		ctx.AddMessage("system", "Unknown subcommand: "+sub+"\nUsage: /session [list|clear]")
	}
	return nil
}
