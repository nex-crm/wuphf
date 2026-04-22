package commands

// RegisterAllCommands populates r with the full set of nex slash commands.
// One canonical command per action. No aliases.
//
// WebSupported flags are set against the web composer's current handler set
// (web/src/components/messages/Composer.tsx). Flip WebSupported on a command
// the moment a web handler exists; leave it off until then. This is the
// source of truth for what the web autocomplete shows — see
// broker_commands.go / GET /commands.
func RegisterAllCommands(r *Registry) {
	// AI
	r.Register(SlashCommand{Name: "ask", Description: "Ask the team lead", WebSupported: true, Execute: cmdAsk})
	r.Register(SlashCommand{Name: "search", Description: "Search messages + KB", WebSupported: true, Execute: cmdSearch})
	r.Register(SlashCommand{Name: "remember", Description: "Store a fact in memory", WebSupported: true, Execute: cmdRemember})
	r.Register(SlashCommand{Name: "youtube-pack", Description: "Generate YouTube content packages", Execute: cmdYouTubePack})

	// Data
	r.Register(SlashCommand{Name: "object", Description: "Object commands (list/get/create/update/delete)", Execute: cmdObject})
	r.Register(SlashCommand{Name: "record", Description: "Record commands (list/get/create/upsert/update/delete/timeline)", Execute: cmdRecord})
	r.Register(SlashCommand{Name: "note", Description: "Note commands (list/get/create/update/delete)", Execute: cmdNote})
	r.Register(SlashCommand{Name: "task", Description: "Task actions (claim/release/complete/block/approve)", WebSupported: true, Execute: cmdTask})
	r.Register(SlashCommand{Name: "list", Description: "List commands (list/get/create/delete/records/add-member)", Execute: cmdList})
	r.Register(SlashCommand{Name: "rel", Description: "Relationship commands (list-defs/create-def/create/delete)", Execute: cmdRel})
	r.Register(SlashCommand{Name: "attribute", Description: "Attribute commands (create/update/delete)", Execute: cmdAttribute})

	// Views
	r.Register(SlashCommand{Name: "graph", Description: "View context graph", Execute: cmdGraph})
	r.Register(SlashCommand{Name: "insights", Description: "View insights", Execute: cmdInsights})
	r.Register(SlashCommand{Name: "calendar", Description: "View schedule", WebSupported: true, Execute: cmdCalendar})
	r.Register(SlashCommand{Name: "chat", Description: "Switch to chat view"})

	// Agents
	r.Register(SlashCommand{Name: "agent", Description: "Agent commands (list/details)", Execute: cmdAgent})

	// Config
	r.Register(SlashCommand{Name: "config", Description: "Config commands (show/set/path)", Execute: cmdConfig})
	r.Register(SlashCommand{Name: "detect", Description: "Detect installed AI platforms", Execute: cmdDetect})
	r.Register(SlashCommand{Name: "init", Description: "Run setup", Execute: cmdInit})
	r.Register(SlashCommand{Name: "provider", Description: "Switch runtime provider", WebSupported: true, Execute: cmdProvider})

	// System
	r.Register(SlashCommand{Name: "help", Description: "Show all commands + keys", WebSupported: true, Execute: cmdHelp})
	r.Register(SlashCommand{Name: "clear", Description: "Clear messages", WebSupported: true, Execute: cmdClear})
	r.Register(SlashCommand{Name: "quit", Description: "Exit WUPHF", Execute: cmdQuit})

	// Web-only surfaces. No TUI Execute handler yet; the web composer owns the
	// behaviour (navigate to a view, post to /signals, etc). Listed here so
	// GET /commands — the single source of truth for the web autocomplete —
	// keeps them discoverable. See Composer.tsx's handleSlashCommand switch.
	r.Register(SlashCommand{Name: "reset", Description: "Reset the office", WebSupported: true})
	r.Register(SlashCommand{Name: "requests", Description: "Open requests", WebSupported: true})
	r.Register(SlashCommand{Name: "policies", Description: "View policies", WebSupported: true})
	r.Register(SlashCommand{Name: "skills", Description: "View skills", WebSupported: true})
	r.Register(SlashCommand{Name: "tasks", Description: "Open task board", WebSupported: true})
	r.Register(SlashCommand{Name: "recover", Description: "Health Check view", WebSupported: true})
	r.Register(SlashCommand{Name: "threads", Description: "See every active thread", WebSupported: true})
	r.Register(SlashCommand{Name: "focus", Description: "Switch to delegation mode", WebSupported: true})
	r.Register(SlashCommand{Name: "collab", Description: "Switch to collaborative mode", WebSupported: true})
	r.Register(SlashCommand{Name: "pause", Description: "Pause all agents", WebSupported: true})
	r.Register(SlashCommand{Name: "resume", Description: "Resume all agents", WebSupported: true})
	r.Register(SlashCommand{Name: "1o1", Description: "1:1 with agent", WebSupported: true})
	r.Register(SlashCommand{Name: "cancel", Description: "Cancel a task", WebSupported: true})
}
