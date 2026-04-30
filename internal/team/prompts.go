package team

// prompts.go owns per-agent prompt + claude-command construction
// (PLAN.md §C13/§C22). buildPrompt delegates to promptBuilder;
// newPromptBuilder is the snapshot-accessor closure assembly;
// writeAgentPromptFile + cleanupAgentTempFiles handle the prompt-file
// lifecycle; resolvePermissionFlags returns the claude permission
// flags; claudeCommand assembles the long shell-command string the
// launcher feeds to tmux split-window for an interactive claude pane.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// buildPrompt generates the system prompt for an agent. The body lives on
// promptBuilder (see prompt_builder.go) so it can be tested without a
// Launcher; this wrapper assembles the snapshot accessors from launcher
// state and delegates.
func (l *Launcher) buildPrompt(slug string) string {
	return l.newPromptBuilder().Build(slug)
}

// newPromptBuilder captures the launcher state the prompt depends on into
// a promptBuilder. Built fresh per call so each prompt sees the current
// member roster and active policies; the construction itself is cheap
// (closures + a couple of map lookups).
func (l *Launcher) newPromptBuilder() *promptBuilder {
	memoryBackend := config.ResolveMemoryBackend("")
	noNex := config.ResolveNoNex() || config.ResolveAPIKey("") == ""
	return &promptBuilder{
		isOneOnOne:  l.isOneOnOne,
		isFocusMode: l.isFocusModeEnabled,
		packName:    l.PackName,
		leadSlug:    l.targeter().LeadSlug,
		members:     l.officeMembersSnapshot,
		policies: func() []officePolicy {
			if l == nil || l.broker == nil {
				return nil
			}
			// promptBuilder.Build sorts the returned slice for
			// prompt-cache byte-stability, so this callback hands
			// back the broker's order as-is. Sorting both here and
			// inside Build is redundant.
			return l.broker.ListPolicies()
		},
		nameFor:        l.targeter().NameFor,
		markdownMemory: memoryBackend == config.MemoryBackendMarkdown,
		nexDisabled:    noNex,
	}
}

// writeAgentPromptFile persists the per-agent system prompt to a stable
// per-slug temp file so it can be passed to `claude --append-system-prompt-file`
// without bloating the tmux command.
//
// File naming mirrors ensureAgentMCPConfig (wuphf-mcp-<slug>.json) so both
// artifacts are easy to clean up together. Perms are 0o600 because the prompt
// can contain team-internal instructions and tool lists.
func (l *Launcher) writeAgentPromptFile(slug, prompt string) (string, error) {
	dir, err := l.launchTempDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "wuphf-prompt-"+slug+".txt")
	if err := os.WriteFile(path, []byte(prompt), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// launchTempDir returns the per-launch temp directory, lazily
// creating it via os.MkdirTemp on first call. Pre-fix every launcher
// wrote to $TMPDIR/wuphf-{prompt,mcp}-<slug>; two offices launching
// the same slug clobbered each other's prompt during startup, and one
// shutdown's cleanupAgentTempFiles would delete the other session's
// prompt out from under it. With a per-launch directory each office
// gets its own scoped namespace and cleanupAgentTempFiles can rm -rf
// the whole directory atomically.
func (l *Launcher) launchTempDir() (string, error) {
	l.launchTempDirOnce.Do(func() {
		dir, err := os.MkdirTemp("", "wuphf-launch-*")
		if err != nil {
			l.launchTempDirErr = err
			return
		}
		l.launchTempDirPath = dir
	})
	return l.launchTempDirPath, l.launchTempDirErr
}

// cleanupAgentTempFiles removes the per-launch temp directory (which
// holds every per-agent MCP config + system prompt). Safe to call
// multiple times and idempotent. Called from Shutdown so the broker
// token + prompt content do not linger in $TMPDIR after the session
// ends. Pre-fix this iterated office members and tried to delete each
// file by name from the global $TMPDIR — that worked but didn't
// catch members removed mid-session and could collide with peer
// launches; rm -rf on the launch-scoped dir solves both.
func (l *Launcher) cleanupAgentTempFiles() {
	if l.launchTempDirPath != "" {
		_ = os.RemoveAll(l.launchTempDirPath)
	}
}

// resolvePermissionFlags returns the Claude Code permission flags for an agent.
// All agents run in bypass mode by default — the team is autonomous.
func (l *Launcher) resolvePermissionFlags(slug string) string {
	return "--permission-mode bypassPermissions --dangerously-skip-permissions"
}

// claudeCommand returns the shell command that launches an interactive
// `claude` session for the given agent. The command is passed as a single
// argument to tmux split-window; if it grows past tmux's internal
// command-parse buffer, tmux rejects it with "command too long" before the
// shell ever runs. Keep the command bounded — put the bulky system prompt in
// a file and pass --append-system-prompt-file <path> instead of inlining.
//
// Sets WUPHF_AGENT_SLUG so the MCP knows which agent this session serves.
// Returns an error if the per-agent temp files (MCP config or prompt) cannot
// be written; callers should fall back to the headless path so agents do not
// silently launch with a missing system prompt.
func (l *Launcher) claudeCommand(slug, systemPrompt string) (string, error) {
	agentMCP, err := l.ensureAgentMCPConfig(slug)
	if err != nil {
		if l.mcpConfig == "" {
			return "", fmt.Errorf("claudeCommand(%s): write agent MCP config: %w", slug, err)
		}
		agentMCP = l.mcpConfig
	}
	mcpConfig := strings.ReplaceAll(agentMCP, "'", "'\\''")

	promptPath, err := l.writeAgentPromptFile(slug, systemPrompt)
	if err != nil {
		return "", fmt.Errorf("claudeCommand(%s): write prompt file: %w", slug, err)
	}
	promptPathQuoted := strings.ReplaceAll(promptPath, "'", "'\\''")

	name := strings.ReplaceAll(l.targeter().NameFor(slug), "'", "'\\''")
	permFlags := l.resolvePermissionFlags(slug)

	brokerToken := ""
	if l.broker != nil {
		brokerToken = l.broker.Token()
	}

	oneOnOneEnv := ""
	if l.isOneOnOne() {
		oneOnOneEnv = fmt.Sprintf("WUPHF_ONE_ON_ONE=1 WUPHF_ONE_ON_ONE_AGENT=%s ", l.oneOnOneAgent())
	}
	oneSecretEnv := ""
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		oneSecretEnv = "ONE_SECRET=" + shellQuote(secret) + " "
	}
	oneIdentityEnv := ""
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		oneIdentityEnv = "ONE_IDENTITY=" + shellQuote(identity) + " "
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			oneIdentityEnv += "ONE_IDENTITY_TYPE=" + shellQuote(identityType) + " "
		}
	}

	model := l.headlessClaudeModel(slug)

	return fmt.Sprintf(
		"%s%s%sWUPHF_AGENT_SLUG=%s WUPHF_BROKER_TOKEN=%s WUPHF_BROKER_BASE_URL=%s WUPHF_NO_NEX=%t ANTHROPIC_PROMPT_CACHING=1 CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=otlp OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/json OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=%s/v1/logs OTEL_EXPORTER_OTLP_HEADERS='Authorization=Bearer %s' OTEL_RESOURCE_ATTRIBUTES='agent.slug=%s,wuphf.channel=office' claude --model %s %s --append-system-prompt-file '%s' --mcp-config '%s' --strict-mcp-config -n '%s'",
		oneOnOneEnv,
		oneSecretEnv,
		oneIdentityEnv,
		slug,
		brokerToken,
		l.BrokerBaseURL(),
		config.ResolveNoNex(),
		l.BrokerBaseURL(),
		brokerToken,
		slug,
		model,
		permFlags,
		promptPathQuoted,
		mcpConfig,
		name,
	), nil
}
