// Package commands implements the slash command registry and dispatch layer.
package commands

import (
	"sort"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// PickerOption is a selectable item in a picker dialog.
// Mirrors tui.PickerOption but kept here to avoid circular imports.
type PickerOption struct {
	Label       string
	Value       string
	Description string
}

// SlashCommand is a named command with an optional Execute handler.
type SlashCommand struct {
	Name        string
	Description string
	Execute     func(ctx *SlashContext, args string) error
}

// SlashContext provides services and UI callbacks to command implementations.
type SlashContext struct {
	AgentService *agent.AgentService
	APIClient    *api.Client
	Config       *config.Config
	AddMessage   func(role, content string)
	SetLoading   func(bool)
	ShowPicker   func(title string, options []PickerOption)
	ShowConfirm  func(question string)
	SendResult   func(output string, err error)
}

// Registry holds registered slash commands keyed by name.
type Registry struct {
	commands map[string]SlashCommand
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]SlashCommand)}
}

// Register adds or replaces a command in the registry.
func (r *Registry) Register(cmd SlashCommand) {
	r.commands[cmd.Name] = cmd
}

// Get returns the command for the given name, if registered.
func (r *Registry) Get(name string) (SlashCommand, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

// List returns all registered commands sorted by name.
func (r *Registry) List() []SlashCommand {
	list := make([]SlashCommand, 0, len(r.commands))
	for _, cmd := range r.commands {
		list = append(list, cmd)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// ParseSlashInput parses "/name args" into name, trimmed args, and isSlash=true.
// Non-slash input returns ("", "", false).
func ParseSlashInput(input string) (name string, args string, isSlash bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}
	rest := strings.TrimPrefix(trimmed, "/")
	parts := strings.SplitN(rest, " ", 2)
	name = parts[0]
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return name, args, true
}
