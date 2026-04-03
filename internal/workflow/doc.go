// Package workflow implements the interactive workflow runtime for WUPHF.
//
// The workflow runtime executes JSON workflow specs that agents emit.
// Each spec defines steps, actions, transitions, and data sources.
// The runtime renders steps using A2UI components, handles user input
// via keybindings, and executes actions through pluggable providers.
//
// Architecture:
//
//	┌─────────────────────────────────────────────────────────┐
//	│                    Workflow Runtime                      │
//	│                                                         │
//	│  WorkflowSpec ──▶ Validator ──▶ StateMachine            │
//	│                                    │                    │
//	│                    ┌───────────────┤                    │
//	│                    ▼               ▼                    │
//	│              StepHandler     ActionDispatcher            │
//	│              (select,        (composio,                 │
//	│               confirm,        broker,                   │
//	│               edit,           agent)                    │
//	│               submit,                                   │
//	│               run)                                      │
//	│                    │               │                    │
//	│                    ▼               ▼                    │
//	│              A2UI Render     Provider Interface          │
//	│              (generative     (injected at               │
//	│               registry)       startup)                  │
//	└─────────────────────────────────────────────────────────┘
//
// This package also provides shared template rendering utilities
// used by both the interactive workflow runtime and the existing
// Composio sequential workflow engine (internal/action/).
package workflow
