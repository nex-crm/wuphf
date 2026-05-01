package team

// launcher_options.go owns the user-facing config knob surface: the
// CLI-bound Set* methods, the small derived predicates that read
// those knobs back (isOneOnOne, oneOnOneAgent, usesCodexRuntime,
// usesOpencodeRuntime, UsesTmuxRuntime), and the exported accessors
// the channel TUI / cmd/wuphf use (BrokerToken, OneOnOneAgent).
// isBlankSlateLaunchSlug is the literal-form recognizer used by the
// blank-slate path. Together these are the Launcher's "what mode am
// I in?" surface — separate from construction (NewLauncher),
// orchestration (Launch), and sub-type wiring.

import (
	"strings"
)

// SetUnsafe enables unrestricted permissions for all agents (CLI-only flag).
func (l *Launcher) SetUnsafe(v bool) { l.unsafe = v }

// SetOpusCEO upgrades the CEO agent from Sonnet to Opus.
func (l *Launcher) SetOpusCEO(v bool) { l.opusCEO = v }

// SetFocusMode enables CEO-routed delegation mode.
func (l *Launcher) SetFocusMode(v bool) { l.focusMode = v }

// SetNoOpen suppresses automatic browser launch on startup.
func (l *Launcher) SetNoOpen(v bool) { l.noOpen = v }

// SetBrokerConfigurator registers startup wiring that must run immediately
// after the launcher constructs its broker and before that broker starts
// serving requests.
func (l *Launcher) SetBrokerConfigurator(fn func(*Broker)) {
	if l == nil {
		return
	}
	l.brokerConfigurator = fn
}

func (l *Launcher) SetOneOnOne(slug string) {
	l.sessionMode = SessionModeOneOnOne
	l.oneOnOne = NormalizeOneOnOneAgent(slug)
}

func isBlankSlateLaunchSlug(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "from-scratch", "blank-slate", blankSlateLaunchSlug:
		return true
	default:
		return false
	}
}

func (l *Launcher) isOneOnOne() bool {
	if l.broker != nil {
		mode, _ := l.broker.SessionModeState()
		return mode == SessionModeOneOnOne
	}
	return NormalizeSessionMode(l.sessionMode) == SessionModeOneOnOne
}

func (l *Launcher) oneOnOneAgent() string {
	if l.broker != nil {
		_, agent := l.broker.SessionModeState()
		return NormalizeOneOnOneAgent(agent)
	}
	return NormalizeOneOnOneAgent(l.oneOnOne)
}

// usesCodexRuntime reports whether the active install-wide provider uses the
// headless one-shot runtime (shared by Codex and Opencode — both skip the
// tmux/claude pane infrastructure and drive a fresh CLI per turn through the
// broker queue in headless_codex.go).
//
// Prefer the capability helpers (usesPaneRuntime, requiresClaudeSessionReset)
// for new code asking "is this a non-pane runtime" — they're Registry-driven
// and pick up future providers (Ollama, vLLM, exo, OpenAI-compatible) without
// further edits here. usesCodexRuntime stays for codex/opencode-binary-specific
// concerns (Preflight, launch routing).
func (l *Launcher) usesCodexRuntime() bool {
	p := strings.TrimSpace(strings.ToLower(l.provider))
	return p == "codex" || p == "opencode"
}

// usesOpencodeRuntime reports whether the install-wide provider is Opencode
// specifically. Used only where the per-turn CLI invocation differs from Codex
// (binary name, args, prompt layout).
func (l *Launcher) usesOpencodeRuntime() bool {
	return strings.EqualFold(strings.TrimSpace(l.provider), "opencode")
}

// UsesTmuxRuntime reports whether agents run in tmux panes. Exported
// for cmd/wuphf/main.go and tests; thin delegator over the targeter.
func (l *Launcher) UsesTmuxRuntime() bool {
	return l.targeter().UsesPaneRuntime()
}

func (l *Launcher) BrokerToken() string {
	if l == nil || l.broker == nil {
		return ""
	}
	return l.broker.Token()
}

// Broker returns the Launcher's internal broker instance. Used by
// cmd/wuphf/main.go to wire workspace orchestration after launch.
func (l *Launcher) Broker() *Broker {
	if l == nil {
		return nil
	}
	return l.broker
}

// OneOnOneAgent returns the active direct-session agent slug, if any.
func (l *Launcher) OneOnOneAgent() string {
	return l.oneOnOneAgent()
}
