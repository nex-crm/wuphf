package teammcp

// workflow_tools.go registers the Workflow Builder's MCP tools. These wrap the
// broker's /workflows/* endpoints so the built-in Workflow Builder agent
// (slug "workflow-builder") can drive the full workflow press from a
// headless turn: list spotted/frozen workflows, inspect a contract, draft from
// a detected shape, freeze (with shipcheck), run, see auto-proposed overlays,
// and accept an overlay to change a frozen contract.
//
// Gating: configureServerTools registers these ONLY for the Workflow Builder
// (and never in DM/1:1 mode). Every OTHER agent is told — via the prompt's
// workflowDelegationBlock — to hand a full spec to @workflow-builder instead of
// touching workflows, and simply does not have these tools. This keeps a single
// owner of the workflow press, the way the Librarian is the single wiki writer.
//
// Why the broker round-trip: the workflow store, miner, shipcheck, and overlay
// engine all live in the broker process. The MCP server runs in a separate
// stdio process, so each tool calls the HTTP endpoint rather than reaching into
// the broker directly.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/workflow"
)

// registerWorkflowTools attaches the Workflow Builder's tools to the MCP
// server. The caller (configureServerTools) is responsible for the
// workflow-builder gate; this function does not re-check the role.
func registerWorkflowTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool(
		"workflow_list",
		"List workflows the office has spotted and frozen. Spotted shapes carry a fingerprint (pass it to workflow_draft / workflow_freeze); frozen contracts carry a spec_id (pass it to workflow_inspect / workflow_run / workflow_improve). Call this before drafting so you extend or improve an existing contract instead of duplicating it.",
	), handleWorkflowList)

	mcp.AddTool(server, readOnlyTool(
		"workflow_inspect",
		"Return a frozen workflow's full contract (states, events, transitions, actions), its triggers (manual / schedule / event), and its most recent runs. Pass spec_id. Use this to understand a contract before changing it or to read run details after a workflow_run.",
	), handleWorkflowInspect)

	mcp.AddTool(server, officeWriteTool(
		"workflow_draft",
		"Draft a typed workflow contract from a spotted shape. Pass the fingerprint from workflow_list. Returns the draft spec plus a shipcheck report. Review and enrich the draft against the spec you were handed, then pass the edited spec to workflow_freeze.",
	), handleWorkflowDraft)

	mcp.AddTool(server, officeWriteTool(
		"workflow_freeze",
		"Freeze a workflow contract so it becomes a runnable, registered workflow. Pass the fingerprint; optionally pass an edited spec (from workflow_draft) to override the baseline draft. Freezing runs shipcheck — if it fails, fix the spec and re-freeze. Returns the registered skill, spec_id, and the shipcheck report.",
	), handleWorkflowFreeze)

	mcp.AddTool(server, officeWriteTool(
		"workflow_run",
		"Run a frozen workflow end-to-end and return the run record (state path, actions fired, audit trail, and any produced outputs). Pass spec_id. Use a run to prove a contract or validate a change, not as background polling.",
	), handleWorkflowRun)

	mcp.AddTool(server, readOnlyTool(
		"workflow_proposals",
		"List self-healing overlays the system has auto-proposed for a frozen workflow from recurring run exceptions. Pass spec_id. Returns the recorded run count and the proposed overlays; accept a good one by passing it to workflow_improve.",
	), handleWorkflowProposals)

	mcp.AddTool(server, officeWriteTool(
		"workflow_improve",
		"Change a frozen workflow by applying an overlay (states/events/transitions/actions to add or replace). Pass spec_id and the overlay object — either one you authored or one from workflow_proposals. The patched spec must pass shipcheck before it lands; a rejected overlay returns the review so you can fix and retry.",
	), handleWorkflowImprove)

	mcp.AddTool(server, officeWriteTool(
		"workflow_extract",
		"Extract a reusable workflow from a COMPLETED task's real trace. Pass task_id. Reads the task's integration actions (with their args + response shapes), judges whether they form a workflow worth automating, and returns a named, parameterized, executable contract (or is_workflow=false with a reason). Use after finishing a multi-step task to turn it into a contract you can freeze.",
	), handleWorkflowExtract)

	mcp.AddTool(server, readOnlyTool(
		"workflow_detected",
		"List workflows the office auto-extracted from completed tasks, grouped by recurrence (how many distinct tasks ran the same shape). Each carries a fingerprint (pass it to workflow_freeze_extracted) and an executable contract. Use this to see what work is worth pressing into a workflow.",
	), handleWorkflowDetected)

	mcp.AddTool(server, officeWriteTool(
		"workflow_freeze_extracted",
		"Freeze an auto-extracted workflow into a runnable, registered workflow. Pass the fingerprint from workflow_detected. The proposal already carries a shipchecked contract; freezing proves it again and creates the binding. Returns the registered skill, spec_id, and shipcheck report.",
	), handleWorkflowFreezeExtracted)
}

// ─── Tool argument contracts ───

// WorkflowListArgs takes no inputs; listing is unfiltered.
type WorkflowListArgs struct{}

// WorkflowInspectArgs identifies a frozen contract.
type WorkflowInspectArgs struct {
	SpecID string `json:"spec_id" jsonschema:"The frozen workflow's spec_id (from workflow_list)."`
}

// WorkflowDraftArgs identifies a spotted shape.
type WorkflowDraftArgs struct {
	Fingerprint string `json:"fingerprint" jsonschema:"The spotted shape's fingerprint (from workflow_list)."`
}

// WorkflowFreezeArgs freezes a draft, optionally overriding it with an edited
// spec. Typing Spec against the real workflow.Spec gives the model a full schema
// to fill (rather than an opaque JSON blob it has to hand-author and routinely
// malforms). nil Spec freezes the baseline draft.
type WorkflowFreezeArgs struct {
	Fingerprint string         `json:"fingerprint" jsonschema:"The spotted shape's fingerprint (from workflow_list)."`
	Spec        *workflow.Spec `json:"spec,omitempty" jsonschema:"Optional edited workflow spec (from workflow_draft) to freeze instead of the baseline draft. Omit to freeze the baseline draft."`
}

// WorkflowRunArgs / WorkflowProposalsArgs identify a frozen contract.
type WorkflowRunArgs struct {
	SpecID string `json:"spec_id" jsonschema:"The frozen workflow's spec_id (from workflow_list)."`
}

type WorkflowProposalsArgs struct {
	SpecID string `json:"spec_id" jsonschema:"The frozen workflow's spec_id (from workflow_list)."`
}

// WorkflowImproveArgs applies an overlay to a frozen contract. Typing Overlay
// against the real workflow.Overlay gives the model a structured schema to fill
// — the json.RawMessage version forced it to hand-author nested JSON, which it
// routinely malformed into invalid_json. The broker reviews + shipchecks the
// patched spec. The overlay MERGES BY ID: an add_* item whose id/name/(from,on)
// already exists REPLACES it (so you can edit an existing step in place by
// re-declaring it with the same id and the new content); a new id is appended.
// An overlay that changes nothing is rejected as no_change.
type WorkflowImproveArgs struct {
	SpecID  string           `json:"spec_id" jsonschema:"The frozen workflow's spec_id (from workflow_list)."`
	Overlay workflow.Overlay `json:"overlay" jsonschema:"The overlay to apply — add_states / add_events / add_actions / add_transitions / add_scenarios / add_terminal / add_allowed_reads / set_goal. Merges by id: re-declare an existing step (same id) with new content to EDIT it in place; a new id is appended. An overlay that changes nothing is rejected. Use one from workflow_proposals or author your own."`
}

// WorkflowExtractArgs identifies a completed task to extract a workflow from.
type WorkflowExtractArgs struct {
	TaskID string `json:"task_id" jsonschema:"The completed task to extract a workflow from (e.g. OFFICE-123)."`
}

// WorkflowDetectedArgs takes no inputs; the detected feed is unfiltered.
type WorkflowDetectedArgs struct{}

// WorkflowFreezeExtractedArgs freezes an auto-extracted proposal.
type WorkflowFreezeExtractedArgs struct {
	Fingerprint string `json:"fingerprint" jsonschema:"The extracted workflow's fingerprint (from workflow_detected)."`
}

// ─── Handlers ───

func handleWorkflowExtract(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowExtractArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.TaskID)
	if id == "" {
		return toolError(fmt.Errorf("task_id is required")), nil, nil
	}
	var out map[string]any
	if err := brokerPostJSON(ctx, "/workflows/extract", map[string]any{"task_id": id}, &out); err != nil {
		return toolError(fmt.Errorf("extract workflow: %w", err)), nil, nil
	}
	return jsonResult(out)
}

func handleWorkflowDetected(ctx context.Context, _ *mcp.CallToolRequest, _ WorkflowDetectedArgs) (*mcp.CallToolResult, any, error) {
	var out map[string]any
	if err := brokerGetJSON(ctx, "/workflows/extracted", &out); err != nil {
		return toolError(fmt.Errorf("list detected workflows: %w", err)), nil, nil
	}
	return jsonResult(out)
}

func handleWorkflowFreezeExtracted(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowFreezeExtractedArgs) (*mcp.CallToolResult, any, error) {
	fp := strings.TrimSpace(args.Fingerprint)
	if fp == "" {
		return toolError(fmt.Errorf("fingerprint is required")), nil, nil
	}
	var out map[string]any
	if err := brokerPostJSON(ctx, "/workflows/freeze-extracted", map[string]any{"fingerprint": fp}, &out); err != nil {
		return toolError(fmt.Errorf("freeze extracted workflow: %w", err)), nil, nil
	}
	return jsonResult(out)
}

func handleWorkflowList(ctx context.Context, _ *mcp.CallToolRequest, _ WorkflowListArgs) (*mcp.CallToolResult, any, error) {
	var spotted struct {
		Workflows []map[string]any `json:"workflows"`
	}
	if err := brokerGetJSON(ctx, "/workflows/spotted", &spotted); err != nil {
		return toolError(fmt.Errorf("list spotted workflows: %w", err)), nil, nil
	}
	frozen := make([]map[string]any, 0, len(spotted.Workflows))
	spottedOut := make([]map[string]any, 0, len(spotted.Workflows))
	for _, w := range spotted.Workflows {
		if isFrozen, _ := w["frozen"].(bool); isFrozen {
			frozen = append(frozen, map[string]any{
				"spec_id": w["spec_id"],
				"title":   w["title"],
				"agent":   w["agent"],
				"outcome": w["outcome"],
			})
			continue
		}
		spottedOut = append(spottedOut, map[string]any{
			"fingerprint": w["fingerprint"],
			"title":       w["title"],
			"agent":       w["agent"],
			"shape":       w["shape"],
			"outcome":     w["outcome"],
			"count":       w["count"],
		})
	}
	return jsonResult(map[string]any{
		"spotted": spottedOut,
		"frozen":  frozen,
	})
}

func handleWorkflowInspect(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowInspectArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.SpecID)
	if id == "" {
		return toolError(fmt.Errorf("spec_id is required")), nil, nil
	}
	q := url.Values{}
	q.Set("spec_id", id)
	var spec map[string]any
	if err := brokerGetJSON(ctx, "/workflows/spec?"+q.Encode(), &spec); err != nil {
		return toolError(fmt.Errorf("inspect workflow %s: %w", id, err)), nil, nil
	}
	var runs map[string]any
	if err := brokerGetJSON(ctx, "/workflows/runs?"+q.Encode(), &runs); err != nil {
		// Runs are best-effort context; a contract with no run history is fine.
		runs = map[string]any{"runs": []any{}}
	}
	out := map[string]any{}
	for k, v := range spec {
		out[k] = v
	}
	out["runs"] = runs["runs"]
	return jsonResult(out)
}

func handleWorkflowDraft(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowDraftArgs) (*mcp.CallToolResult, any, error) {
	fp := strings.TrimSpace(args.Fingerprint)
	if fp == "" {
		return toolError(fmt.Errorf("fingerprint is required")), nil, nil
	}
	var out map[string]any
	if err := brokerPostJSON(ctx, "/workflows/draft", map[string]any{"fingerprint": fp}, &out); err != nil {
		return toolError(fmt.Errorf("draft workflow: %w", err)), nil, nil
	}
	return jsonResult(out)
}

func handleWorkflowFreeze(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowFreezeArgs) (*mcp.CallToolResult, any, error) {
	fp := strings.TrimSpace(args.Fingerprint)
	if fp == "" {
		return toolError(fmt.Errorf("fingerprint is required")), nil, nil
	}
	body := map[string]any{"fingerprint": fp}
	if args.Spec != nil {
		body["spec"] = args.Spec
	}
	var out map[string]any
	if err := brokerPostJSON(ctx, "/workflows/freeze", body, &out); err != nil {
		return toolError(fmt.Errorf("freeze workflow: %w", err)), nil, nil
	}
	return jsonResult(out)
}

func handleWorkflowRun(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowRunArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.SpecID)
	if id == "" {
		return toolError(fmt.Errorf("spec_id is required")), nil, nil
	}
	var out map[string]any
	if err := brokerPostJSON(ctx, "/workflows/run", map[string]any{"spec_id": id}, &out); err != nil {
		return toolError(fmt.Errorf("run workflow %s: %w", id, err)), nil, nil
	}
	return jsonResult(out)
}

func handleWorkflowProposals(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowProposalsArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.SpecID)
	if id == "" {
		return toolError(fmt.Errorf("spec_id is required")), nil, nil
	}
	var out map[string]any
	if err := brokerPostJSON(ctx, "/workflows/proposals", map[string]any{"spec_id": id}, &out); err != nil {
		return toolError(fmt.Errorf("list proposals for %s: %w", id, err)), nil, nil
	}
	return jsonResult(out)
}

func handleWorkflowImprove(ctx context.Context, _ *mcp.CallToolRequest, args WorkflowImproveArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.SpecID)
	if id == "" {
		return toolError(fmt.Errorf("spec_id is required")), nil, nil
	}
	overlay := args.Overlay
	// Default the overlay's spec_id/source so the model doesn't have to repeat
	// them; the spec_id arg is authoritative.
	overlay.SpecID = id
	if strings.TrimSpace(overlay.Source) == "" {
		overlay.Source = "operator_edit"
	}
	var out map[string]any
	if err := brokerPostJSON(ctx, "/workflows/improve", map[string]any{
		"spec_id": id,
		"overlay": overlay,
	}, &out); err != nil {
		return toolError(fmt.Errorf("improve workflow %s: %w", id, err)), nil, nil
	}
	return jsonResult(out)
}

// jsonResult marshals an arbitrary payload into a successful tool result.
func jsonResult(payload any) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return toolError(fmt.Errorf("marshal workflow tool result: %w", err)), nil, nil
	}
	return textResult(string(data)), nil, nil
}
