package team

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/config"
)

type CapabilityLevel string

const (
	CapabilityReady CapabilityLevel = "ready"
	CapabilityWarn  CapabilityLevel = "warn"
	CapabilityInfo  CapabilityLevel = "info"
)

type CapabilityStatus struct {
	Name     string
	Level    CapabilityLevel
	Detail   string
	NextStep string
}

type TmuxSessionStatus struct {
	Name     string
	Attached int
	Windows  int
}

type TmuxCapability struct {
	BinaryPath    string
	Version       string
	SocketName    string
	SessionName   string
	InsideTmux    bool
	InsideTmuxEnv string
	ServerRunning bool
	Sessions      []TmuxSessionStatus
	ProbeError    string
}

type RuntimeCapabilities struct {
	Tmux  TmuxCapability
	Items []CapabilityStatus
}

var lookPathFn = exec.LookPath
var commandCombinedOutputFn = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}
var actionProviderProbe = detectActionProvider

func DetectRuntimeCapabilities() RuntimeCapabilities {
	tmuxStatus, tmux := probeTmuxCapability()
	items := []CapabilityStatus{
		tmuxStatus,
		probeBinaryCapability("claude", "Needed for teammate runtime sessions."),
		probeNexCapability(),
		probeActionCapability(),
	}
	return RuntimeCapabilities{Tmux: tmux, Items: items}
}

func (c RuntimeCapabilities) Counts() (ready, warn, info int) {
	for _, item := range c.Items {
		switch item.Level {
		case CapabilityReady:
			ready++
		case CapabilityWarn:
			warn++
		case CapabilityInfo:
			info++
		}
	}
	return ready, warn, info
}

func probeBinaryCapability(name, next string) CapabilityStatus {
	if _, err := lookPathFn(name); err != nil {
		return CapabilityStatus{
			Name:     name,
			Level:    CapabilityWarn,
			Detail:   fmt.Sprintf("%s is not available on PATH.", name),
			NextStep: next,
		}
	}
	return CapabilityStatus{
		Name:   name,
		Level:  CapabilityReady,
		Detail: fmt.Sprintf("%s is installed.", name),
	}
}

func probeTmuxCapability() (CapabilityStatus, TmuxCapability) {
	capability := TmuxCapability{
		SocketName:    tmuxSocketName,
		SessionName:   SessionName,
		InsideTmux:    strings.TrimSpace(os.Getenv("TMUX")) != "",
		InsideTmuxEnv: strings.TrimSpace(os.Getenv("TMUX")),
	}

	path, err := lookPathFn("tmux")
	if err != nil {
		return CapabilityStatus{
			Name:     "tmux",
			Level:    CapabilityWarn,
			Detail:   "tmux is not available on PATH.",
			NextStep: "Install tmux so WUPHF can manage the office session.",
		}, capability
	}
	capability.BinaryPath = path

	if out, err := commandCombinedOutputFn("tmux", "-V"); len(out) > 0 {
		capability.Version = strings.TrimSpace(string(out))
	} else if err != nil {
		capability.ProbeError = strings.TrimSpace(err.Error())
	}

	if out, err := commandCombinedOutputFn("tmux", "-L", tmuxSocketName, "list-sessions", "-F", "#{session_name}\t#{session_attached}\t#{session_windows}"); err != nil || len(out) > 0 {
		capability.Sessions = parseTmuxSessions(out)
		if len(capability.Sessions) > 0 {
			capability.ServerRunning = true
		}
		if err != nil && len(capability.Sessions) == 0 {
			if note := strings.TrimSpace(string(out)); note != "" {
				capability.ProbeError = note
			} else {
				capability.ProbeError = strings.TrimSpace(err.Error())
			}
		}
	}

	status := capability.status()
	return status, capability
}

func parseTmuxSessions(out []byte) []TmuxSessionStatus {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sessions := make([]TmuxSessionStatus, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		attached, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		windows, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		sessions = append(sessions, TmuxSessionStatus{
			Name:     strings.TrimSpace(parts[0]),
			Attached: attached,
			Windows:  windows,
		})
	}
	return sessions
}

func (t TmuxCapability) hasData() bool {
	return t.BinaryPath != "" || t.Version != "" || t.SocketName != "" || t.SessionName != "" || t.InsideTmux || t.InsideTmuxEnv != "" || t.ServerRunning || len(t.Sessions) > 0 || t.ProbeError != ""
}

func (t TmuxCapability) targetSession() (TmuxSessionStatus, bool) {
	for _, session := range t.Sessions {
		if strings.EqualFold(strings.TrimSpace(session.Name), strings.TrimSpace(t.SessionName)) {
			return session, true
		}
	}
	return TmuxSessionStatus{}, false
}

func (t TmuxCapability) summaryDetail() string {
	if t.BinaryPath == "" {
		return "tmux is not available on PATH."
	}
	version := strings.TrimSpace(t.Version)
	if version == "" {
		version = "tmux"
	}
	if !t.ServerRunning {
		return fmt.Sprintf("%s is installed, but the WUPHF tmux server on socket %s is not running yet.", version, t.SocketName)
	}
	if session, ok := t.targetSession(); ok {
		return fmt.Sprintf("%s on socket %s is running with session %s (%d attached, %d windows).", version, t.SocketName, session.Name, session.Attached, session.Windows)
	}
	return fmt.Sprintf("%s on socket %s has %d active tmux session(s), but %s is not running.", version, t.SocketName, len(t.Sessions), t.SessionName)
}

func (t TmuxCapability) nextStep() string {
	if t.BinaryPath == "" {
		return "Install tmux so WUPHF can manage the office session."
	}
	if !t.ServerRunning {
		return "Launch WUPHF to create the tmux office session."
	}
	if _, ok := t.targetSession(); !ok {
		return "Restart WUPHF to recreate the missing office session."
	}
	return ""
}

func (t TmuxCapability) status() CapabilityStatus {
	if t.BinaryPath == "" {
		return CapabilityStatus{
			Name:     "tmux",
			Level:    CapabilityWarn,
			Detail:   "tmux is not available on PATH.",
			NextStep: t.nextStep(),
		}
	}
	if !t.ServerRunning {
		return CapabilityStatus{
			Name:     "tmux",
			Level:    CapabilityInfo,
			Detail:   t.summaryDetail(),
			NextStep: t.nextStep(),
		}
	}
	if _, ok := t.targetSession(); !ok {
		return CapabilityStatus{
			Name:     "tmux",
			Level:    CapabilityWarn,
			Detail:   t.summaryDetail(),
			NextStep: t.nextStep(),
		}
	}
	return CapabilityStatus{
		Name:   "tmux",
		Level:  CapabilityReady,
		Detail: t.summaryDetail(),
	}
}

func (t TmuxCapability) FormatLines() []string {
	if !t.hasData() {
		return nil
	}
	lines := []string{
		fmt.Sprintf("- Binary: %s", displayOrUnknown(t.BinaryPath)),
		fmt.Sprintf("- Version: %s", displayOrUnknown(t.Version)),
		fmt.Sprintf("- Socket: %s", displayOrUnknown(t.SocketName)),
		fmt.Sprintf("- Inside tmux: %s", yesNo(t.InsideTmux)),
	}
	if t.InsideTmuxEnv != "" {
		lines = append(lines, fmt.Sprintf("- TMUX env: %s", t.InsideTmuxEnv))
	}
	if !t.ServerRunning {
		lines = append(lines, fmt.Sprintf("- WUPHF session: not running yet (%s)", t.SessionName))
	} else if session, ok := t.targetSession(); ok {
		lines = append(lines, fmt.Sprintf("- WUPHF session: running (%d attached, %d windows)", session.Attached, session.Windows))
	} else {
		lines = append(lines, fmt.Sprintf("- WUPHF session: missing from socket %s", t.SocketName))
	}
	if len(t.Sessions) > 0 {
		lines = append(lines, "- tmux sessions:")
		for _, session := range t.Sessions {
			lines = append(lines, fmt.Sprintf("- %s: %d attached, %d windows", session.Name, session.Attached, session.Windows))
		}
	}
	if strings.TrimSpace(t.ProbeError) != "" {
		lines = append(lines, fmt.Sprintf("- Probe note: %s", t.ProbeError))
	}
	return lines
}

func displayOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func probeNexCapability() CapabilityStatus {
	if config.ResolveNoNex() {
		return CapabilityStatus{
			Name:     "nex",
			Level:    CapabilityInfo,
			Detail:   "Disabled for this session with --no-nex.",
			NextStep: "Restart without --no-nex to enable memory, integrations, and provider-backed actions.",
		}
	}
	if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey == "" {
		return CapabilityStatus{
			Name:     "nex",
			Level:    CapabilityWarn,
			Detail:   "Missing WUPHF/Nex API key.",
			NextStep: "Run /init and save your WUPHF API key.",
		}
	}
	email := strings.TrimSpace(config.ResolveComposioUserID())
	if email == "" {
		return CapabilityStatus{
			Name:     "nex",
			Level:    CapabilityWarn,
			Detail:   "API key is configured, but workspace identity is missing.",
			NextStep: "Finish /init so WUPHF can scope integrations to your Nex email.",
		}
	}
	return CapabilityStatus{
		Name:   "nex",
		Level:  CapabilityReady,
		Detail: fmt.Sprintf("Configured with workspace identity %s.", email),
	}
}

func probeActionCapability() CapabilityStatus {
	name, err := actionProviderProbe()
	if err != nil {
		return CapabilityStatus{
			Name:     "actions",
			Level:    CapabilityWarn,
			Detail:   err.Error(),
			NextStep: "Configure an external action provider or switch WUPHF to a working provider.",
		}
	}
	return CapabilityStatus{
		Name:   "actions",
		Level:  CapabilityReady,
		Detail: fmt.Sprintf("External actions available through %s.", name),
	}
}

func detectActionProvider() (string, error) {
	provider, err := action.NewRegistryFromEnv().ProviderFor(action.CapabilityConnections)
	if err != nil {
		return "", err
	}
	return provider.Name(), nil
}
