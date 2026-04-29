package team

// prompts.go owns per-agent prompt construction (PLAN.md §C13).
// Hosts buildPrompt (delegates to promptBuilder), newPromptBuilder
// (snapshot-accessor closure assembly), writeAgentPromptFile +
// cleanupAgentTempFiles for the prompt-file lifecycle, and
// resolvePermissionFlags. Split out of launcher.go so prompt-shape
// changes don't require navigating the lifecycle code.
//
// claudeCommand stays in launcher.go for now — moving it would trigger
// the no-secrets pre-commit hook on the ONE_SECRET= env-var assembly
// (false positive). Future PR can move it once the regex is loosened.

import (
	"os"
	"path/filepath"
	"sort"

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
			policies := l.broker.ListPolicies()
			// Sort so the policies section is deterministic and
			// cache-friendly across turns.
			sort.Slice(policies, func(i, j int) bool { return policies[i].ID < policies[j].ID })
			return policies
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
	path := filepath.Join(os.TempDir(), "wuphf-prompt-"+slug+".txt")
	if err := os.WriteFile(path, []byte(prompt), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// cleanupAgentTempFiles removes the per-agent MCP config + system prompt
// temp files for every known office member. Safe to call multiple times and
// idempotent — missing files are ignored. Called from Shutdown so the broker
// token + prompt content do not linger in $TMPDIR after the session ends.
func (l *Launcher) cleanupAgentTempFiles() {
	tmp := os.TempDir()
	for _, m := range l.officeMembersSnapshot() {
		for _, path := range []string{
			filepath.Join(tmp, "wuphf-mcp-"+m.Slug+".json"),
			filepath.Join(tmp, "wuphf-prompt-"+m.Slug+".txt"),
		} {
			_ = os.Remove(path)
		}
	}
}

// resolvePermissionFlags returns the Claude Code permission flags for an agent.
// All agents run in bypass mode by default — the team is autonomous.
func (l *Launcher) resolvePermissionFlags(slug string) string {
	return "--permission-mode bypassPermissions --dangerously-skip-permissions"
}
