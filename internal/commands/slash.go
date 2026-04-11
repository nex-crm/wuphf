package commands

// RegisterAllCommands populates r with the full set of nex slash commands.
// One canonical command per action. No aliases.
func RegisterAllCommands(r *Registry) {
	// AI
	r.Register(SlashCommand{Name: "ask", Description: "Ask the AI a question", Execute: cmdAsk})
	r.Register(SlashCommand{Name: "search", Description: "Search knowledge base", Execute: cmdSearch})
	r.Register(SlashCommand{Name: "remember", Description: "Store information", Execute: cmdRemember})

	// Data
	r.Register(SlashCommand{Name: "object", Description: "Object commands (list/get/create/update/delete)", Execute: cmdObject})
	r.Register(SlashCommand{Name: "record", Description: "Record commands (list/get/create/upsert/update/delete/timeline)", Execute: cmdRecord})
	r.Register(SlashCommand{Name: "note", Description: "Note commands (list/get/create/update/delete)", Execute: cmdNote})
	r.Register(SlashCommand{Name: "task", Description: "Task commands (list/get/create/update/delete)", Execute: cmdTask})
	r.Register(SlashCommand{Name: "list", Description: "List commands (list/get/create/delete/records/add-member)", Execute: cmdList})
	r.Register(SlashCommand{Name: "rel", Description: "Relationship commands (list-defs/create-def/create/delete)", Execute: cmdRel})
	r.Register(SlashCommand{Name: "attribute", Description: "Attribute commands (create/update/delete)", Execute: cmdAttribute})

	// Views
	r.Register(SlashCommand{Name: "graph", Description: "View context graph", Execute: cmdGraph})
	r.Register(SlashCommand{Name: "insights", Description: "View insights", Execute: cmdInsights})
	r.Register(SlashCommand{Name: "calendar", Description: "View calendar", Execute: cmdCalendar})
	r.Register(SlashCommand{Name: "chat", Description: "Switch to chat view"})

	// Agents
	r.Register(SlashCommand{Name: "agent", Description: "Agent commands (list/details)", Execute: cmdAgent})
	r.Register(SlashCommand{
		Name:        "managed-agent",
		Description: "Managed agent HITL: approve|respond|deny|stop|events <run_id>",
		Execute:     cmdManagedAgent,
	})

	// Config
	r.Register(SlashCommand{Name: "config", Description: "Config commands (show/set/path)", Execute: cmdConfig})
	r.Register(SlashCommand{Name: "detect", Description: "Detect installed AI platforms", Execute: cmdDetect})
	r.Register(SlashCommand{Name: "init", Description: "Run setup", Execute: cmdInit})
	r.Register(SlashCommand{Name: "provider", Description: "Switch LLM provider", Execute: cmdProvider})

	// System
	r.Register(SlashCommand{Name: "help", Description: "Show all commands", Execute: cmdHelp})
	r.Register(SlashCommand{Name: "clear", Description: "Clear messages", Execute: cmdClear})
	r.Register(SlashCommand{Name: "quit", Description: "Exit WUPHF", Execute: cmdQuit})
}
