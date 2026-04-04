package workflow

import (
	"encoding/json"
	"fmt"
)

// GenerationPrompt returns the system prompt that teaches an agent how to
// generate valid workflow specs. It includes the JSON schema, the 5
// interaction primitives, and two complete example specs for few-shot learning.
func GenerationPrompt() string {
	return generationPromptText
}

// ValidateAndFix attempts to parse and validate a JSON workflow spec string.
// On success it returns the parsed WorkflowSpec. On failure it returns an
// error whose message is suitable for feeding back to the generating agent
// so it can correct its output.
func ValidateAndFix(specJSON string) (*WorkflowSpec, error) {
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return nil, fmt.Errorf("JSON parse error: %v", err)
	}
	if err := ValidateSpec(spec); err != nil {
		return nil, fmt.Errorf("validation error: %v", err)
	}
	return &spec, nil
}

const generationPromptText = `You are a workflow spec generator. Your job is to produce a valid JSON workflow spec that the WUPHF A2UI runtime can execute.

## JSON Schema

A workflow spec is a JSON object with these fields:

{
  "id":          string (required, unique identifier),
  "title":       string (required, human-readable title),
  "description": string (optional),
  "steps":       [StepSpec...] (required, at least one step),
  "dataSources": [DataSource...] (optional),
  "dryRun":      boolean (optional, default false)
}

### StepSpec

{
  "id":          string (required, unique within the spec),
  "type":        string (required, one of: "select", "confirm", "edit", "submit", "run"),
  "prompt":      string (optional, what the user sees),
  "dataRef":     string (optional, JSON Pointer starting with "/"),
  "display":     DisplaySpec (optional),
  "actions":     [ActionSpec...] (optional),
  "execute":     ExecuteSpec (optional, for submit steps),
  "transition":  string (optional, auto-transition target for submit/run steps),
  "onError":     string (optional, step ID to go to on error),
  "allowLoop":   boolean (optional, permits circular transitions back to this step),
  "agent":       string (optional, agent slug for run steps),
  "agentPrompt": string (optional, prompt sent to the agent for run steps),
  "workflow":    string (optional, sub-workflow key for run steps),
  "outputRef":   string (optional, JSON Pointer for storing run step output)
}

### DisplaySpec

{
  "component": string (required, e.g. "card", "text", "textfield"),
  "props":     object (optional),
  "dataRef":   string (optional, JSON Pointer)
}

### ActionSpec

{
  "key":        string (required, the key binding e.g. "a", "y", "Enter"),
  "label":      string (required, what the user sees),
  "execute":    ExecuteSpec (optional),
  "transition": string (optional, target step ID or "done")
}

### ExecuteSpec

{
  "provider":      string (required, one of: "composio", "broker", "agent"),
  "action":        string (required for composio),
  "method":        string (required for broker),
  "connectionKey": string (optional),
  "data":          object (optional),
  "slug":          string (required for agent),
  "prompt":        string (optional for agent),
  "outputRef":     string (optional)
}

### DataSource

{
  "id":            string (required),
  "provider":      string (required),
  "action":        string (required),
  "connectionKey": string (optional)
}

## The 5 Interaction Primitives

1. **select** - User chooses from a list. Has a dataRef pointing to list data, actions for each choice. Use display.component "card" to render items.
   Example step:
   {"id": "pick", "type": "select", "prompt": "Choose one:", "dataRef": "/items", "actions": [{"key": "a", "label": "Accept", "transition": "next"}]}

2. **confirm** - Yes/no decision. Shows content via display, user picks an action.
   Example step:
   {"id": "ok", "type": "confirm", "prompt": "Proceed?", "actions": [{"key": "y", "label": "Yes", "transition": "next"}, {"key": "n", "label": "No", "transition": "done"}]}

3. **edit** - User modifies a field. Has dataRef pointing to the value, display with "textfield" component.
   Example step:
   {"id": "fix", "type": "edit", "prompt": "Edit the message:", "dataRef": "/draft", "display": {"component": "textfield", "props": {"multiline": true}}, "actions": [{"key": "Enter", "label": "Done", "transition": "next"}]}

4. **submit** - Execute a side-effect silently (spinner shown). Has an execute spec and auto-transitions.
   Example step:
   {"id": "send", "type": "submit", "execute": {"provider": "composio", "action": "GMAIL_SEND_EMAIL", "data": {"to": "user@example.com"}}, "transition": "done"}

5. **run** - Dispatch to an agent or sub-workflow. Has agent/agentPrompt OR workflow field, plus outputRef for storing results.
   Example step:
   {"id": "analyze", "type": "run", "prompt": "Analyzing...", "agent": "analyzer", "agentPrompt": "Analyze the data.", "outputRef": "/result", "actions": [{"key": "Enter", "label": "Continue", "transition": "next"}]}

## Example Specs

### Email Triage (branching workflow with loops)

{"id":"email-triage","title":"Email Triage","steps":[{"id":"list","type":"select","prompt":"Select emails to triage:","dataRef":"/emails","display":{"component":"card","props":{"showPriority":true}},"actions":[{"key":"a","label":"Approve","transition":"triage"},{"key":"r","label":"Reject","transition":"list"},{"key":"d","label":"Dismiss","transition":"dismiss"}],"allowLoop":true},{"id":"triage","type":"run","prompt":"Triaging email...","agent":"email-triage","agentPrompt":"Triage this email and draft a reply.","outputRef":"/triageResult","actions":[{"key":"Enter","label":"Continue","transition":"confirm-send"}]},{"id":"confirm-send","type":"confirm","prompt":"Send this reply?","allowLoop":true,"display":{"component":"text","dataRef":"/triageResult/draftReply"},"actions":[{"key":"y","label":"Yes, send","execute":{"provider":"composio","action":"GMAIL_SEND_EMAIL","data":{"ref":"/triageResult/draftReply"}},"transition":"list"},{"key":"e","label":"Edit first","transition":"edit-reply"},{"key":"n","label":"Cancel","transition":"list"}]},{"id":"edit-reply","type":"edit","prompt":"Edit the reply:","dataRef":"/triageResult/draftReply","display":{"component":"textfield","props":{"label":"Reply","multiline":true}},"actions":[{"key":"Enter","label":"Done","transition":"confirm-send"}]},{"id":"dismiss","type":"submit","execute":{"provider":"broker","method":"AddEmailDecision","data":{"decision":"dismiss"}},"transition":"list"}],"dataSources":[{"id":"emails","provider":"composio","action":"GMAIL_LIST_MESSAGES"}]}

### Deploy Check (linear workflow)

{"id":"deploy-check","title":"Deploy Check","description":"Pre-deploy verification workflow","steps":[{"id":"fetch-status","type":"run","prompt":"Checking deploy readiness...","agent":"deploy-checker","agentPrompt":"Check CI status, test results, and open PRs. Return a readiness report.","outputRef":"/deployStatus","actions":[{"key":"Enter","label":"Continue","transition":"review"}]},{"id":"review","type":"confirm","prompt":"Deploy readiness report:","display":{"component":"card","props":{"title":"Deploy Status"},"dataRef":"/deployStatus"},"actions":[{"key":"d","label":"Deploy","transition":"notify-team"},{"key":"a","label":"Abort","transition":"done"}]},{"id":"notify-team","type":"submit","prompt":"Notifying team...","execute":{"provider":"composio","action":"SLACK_SEND_MESSAGE","data":{"channel":"#deploys","text":"Deploying to production..."}},"transition":"execute-deploy"},{"id":"execute-deploy","type":"run","prompt":"Deploying...","agent":"deploy-runner","agentPrompt":"Run the deploy pipeline. Report success or failure.","outputRef":"/deployResult","actions":[{"key":"Enter","label":"Done","transition":"done"}]}],"dataSources":[]}

## Validation Rules

- The spec must have a non-empty "id" and at least one step.
- Every step must have a unique "id" and a valid "type".
- All transition targets (in actions, step-level transition, onError) must reference an existing step ID or "done".
- Circular transitions are forbidden unless the target step has "allowLoop": true.
- "dataRef" fields must start with "/".
- Every action must have a non-empty "key" and "label".
- Action keys must be unique within a step.
- Execute specs must have a valid provider: "composio" (requires action), "broker" (requires method), or "agent" (requires slug).
- Run steps must specify at least one of: agent, workflow, or execute.

## Instructions

Output ONLY valid JSON. No markdown fencing. No explanation.`
