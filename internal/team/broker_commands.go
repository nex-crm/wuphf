package team

// broker_commands.go exposes the slash-command registry (the TUI source of
// truth at internal/commands) over HTTP so the web composer can render its
// autocomplete from the same list as the TUI. Before this endpoint the web
// had its own hardcoded SLASH_COMMANDS constant which drifted the moment a
// TUI command was added or renamed.
//
// Route: GET /commands
//
// Payload shape:
//
//	[
//	  { "name": "ask", "description": "...", "webSupported": true },
//	  { "name": "object", "description": "...", "webSupported": false },
//	  ...
//	]
//
// Sorted alphabetically (matches commands.Registry.List). The web filters
// for webSupported=true; the TUI ignores this endpoint entirely.

import (
	"encoding/json"
	"net/http"

	"github.com/nex-crm/wuphf/internal/commands"
)

// commandDescriptor is the JSON shape returned by GET /commands. JSON tags
// are camelCase to match the web's existing API conventions.
type commandDescriptor struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	WebSupported bool   `json:"webSupported"`
}

// registryLister is the narrow interface GET /commands depends on. Tests
// substitute a fake so the handler can be exercised without touching the
// global registry.
type registryLister interface {
	List() []commands.SlashCommand
}

// newCommandsRegistry builds the canonical registry. Overridable so tests
// can inject a smaller, deterministic command set.
var newCommandsRegistry = func() registryLister {
	r := commands.NewRegistry()
	commands.RegisterAllCommands(r)
	return r
}

// handleCommands answers GET /commands. Non-GET requests get 405 — we do
// not accept writes to the registry over HTTP; the TUI registry is the
// source of truth and mutating it at runtime would make the web/TUI parity
// guarantee meaningless.
func (b *Broker) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	registry := newCommandsRegistry()
	list := registry.List()

	out := make([]commandDescriptor, 0, len(list))
	for _, cmd := range list {
		out = append(out, commandDescriptor{
			Name:         cmd.Name,
			Description:  cmd.Description,
			WebSupported: cmd.WebSupported,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	// Small, stable payload — cache for a minute so rapid reloads don't
	// thrash the broker. The only way the list changes is a rebuild.
	w.Header().Set("Cache-Control", "private, max-age=60")
	_ = json.NewEncoder(w).Encode(out)
}
