package workflow

// WorkflowSpec is the top-level JSON schema for an interactive workflow.
// Agents emit these specs; the runtime executes them step by step.
//
//	┌─────────────────────────────────────────────────┐
//	│  WorkflowSpec                                   │
//	│  ├── id, title, description                     │
//	│  ├── steps[]                                    │
//	│  │   ├── id, type (select/confirm/edit/submit/run)
//	│  │   ├── prompt (what the user sees)            │
//	│  │   ├── display (A2UI component spec)          │
//	│  │   ├── dataRef (JSON Pointer into data store) │
//	│  │   ├── actions[] (keybindings + transitions)  │
//	│  │   └── onError (error transition target)      │
//	│  └── dataSources[]                              │
//	│      ├── id, provider, action                   │
//	│      └── poll (v2, ignored in v1)               │
//	└─────────────────────────────────────────────────┘
type WorkflowSpec struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Steps       []StepSpec   `json:"steps"`
	DataSources []DataSource `json:"dataSources,omitempty"`
	DryRun      bool         `json:"dryRun,omitempty"`
}

// StepSpec defines a single step in a workflow.
// The type determines the interaction pattern:
//   - select: choose from a list (cursor navigation + actions)
//   - confirm: yes/no decision (display content + action keys)
//   - edit: modify a field (text input)
//   - submit: execute an action silently (spinner, auto-transition)
//   - run: dispatch to an agent or sub-workflow (spinner, auto-transition)
type StepSpec struct {
	ID        string       `json:"id"`
	Type      string       `json:"type"`
	Prompt    string       `json:"prompt,omitempty"`
	DataRef   string       `json:"dataRef,omitempty"`
	Display   *DisplaySpec `json:"display,omitempty"`
	Actions   []ActionSpec `json:"actions,omitempty"`
	Execute   *ExecuteSpec `json:"execute,omitempty"`
	Transition string      `json:"transition,omitempty"` // auto-transition target for submit/run steps
	OnError    string      `json:"onError,omitempty"`
	AllowLoop  bool        `json:"allowLoop,omitempty"`

	// Run step fields
	Agent     string `json:"agent,omitempty"`
	AgentPrompt string `json:"agentPrompt,omitempty"`
	Workflow  string `json:"workflow,omitempty"`
	OutputRef string `json:"outputRef,omitempty"`
}

// DisplaySpec controls how a step renders using the A2UI component system.
type DisplaySpec struct {
	Component string         `json:"component"`
	Props     map[string]any `json:"props,omitempty"`
	DataRef   string         `json:"dataRef,omitempty"`
}

// ActionSpec binds a key to a label, optional side-effect, and transition.
type ActionSpec struct {
	Key        string       `json:"key"`
	Label      string       `json:"label"`
	Execute    *ExecuteSpec `json:"execute,omitempty"`
	Transition string       `json:"transition,omitempty"`
}

// ExecuteSpec defines a side-effect triggered by an action.
// Provider determines which execution backend handles it.
type ExecuteSpec struct {
	Provider      string         `json:"provider"`
	Action        string         `json:"action,omitempty"`
	Method        string         `json:"method,omitempty"`
	ConnectionKey string         `json:"connectionKey,omitempty"`
	Data          map[string]any `json:"data,omitempty"`

	// Agent provider fields
	Slug      string `json:"slug,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	OutputRef string `json:"outputRef,omitempty"`
}

// DataSource populates the workflow's data store before the first step.
type DataSource struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	Action        string `json:"action"`
	ConnectionKey string `json:"connectionKey,omitempty"`
	Poll          string `json:"poll,omitempty"` // v2: ignored in v1
}

// Valid step types.
const (
	StepSelect  = "select"
	StepConfirm = "confirm"
	StepEdit    = "edit"
	StepSubmit  = "submit"
	StepRun     = "run"
)

// Valid execution providers.
const (
	ProviderComposio = "composio"
	ProviderBroker   = "broker"
	ProviderAgent    = "agent"
)

// TransitionDone is the conventional target for "workflow complete."
const TransitionDone = "done"
