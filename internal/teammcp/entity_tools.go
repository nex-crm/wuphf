package teammcp

// entity_tools.go defines the two v1.2 entity-brief MCP tools:
//
//   entity_fact_record     — record one fact about an entity (person, company, customer)
//   entity_brief_synthesize — explicitly request a brief refresh
//
// Registered only when WUPHF_MEMORY_BACKEND=markdown, matching the wiki and
// notebook gates — the entity brief surface rides on the same markdown git
// substrate, so it has the same backend precondition.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TeamEntityFactRecordArgs is the contract for entity_fact_record.
type TeamEntityFactRecordArgs struct {
	MySlug     string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	EntityKind string `json:"entity_kind" jsonschema:"One of: people | companies | customers"`
	EntitySlug string `json:"entity_slug" jsonschema:"Kebab-case slug matching the canonical wiki file (e.g. team/people/nazz.md -> nazz)"`
	Fact       string `json:"fact" jsonschema:"One atomic observation. Max 4000 chars. Never invent or generalise — record only what you directly observed."`
	SourcePath string `json:"source_path,omitempty" jsonschema:"Optional wiki/notebook path this fact came from (must start with agents/ or team/)."`
}

// TeamEntityBriefSynthesizeArgs is the contract for entity_brief_synthesize.
type TeamEntityBriefSynthesizeArgs struct {
	MySlug     string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	EntityKind string `json:"entity_kind" jsonschema:"One of: people | companies | customers"`
	EntitySlug string `json:"entity_slug" jsonschema:"Kebab-case slug."`
}

// registerEntityTools attaches the two entity tools to the MCP server.
// Caller (registerSharedMemoryTools, markdown branch) is responsible for
// gating on WUPHF_MEMORY_BACKEND.
func registerEntityTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"entity_fact_record",
		"Record one atomic fact about an entity (person, company, or customer). The broker appends to an append-only fact log and — if enough new facts have accumulated since the last synthesis — triggers a background rewrite of that entity's brief. Facts are single observations, never interpretations. Wrong facts get counter-facts, not deletions.",
	), handleEntityFactRecord)
	mcp.AddTool(server, officeWriteTool(
		"entity_brief_synthesize",
		"Explicitly request a fresh synthesis of an entity brief. Runs as a broker-level background job (no agent turn consumed). Use this when you've just recorded several facts and want the canonical brief updated now instead of at the next threshold.",
	), handleEntityBriefSynthesize)
}

func handleEntityFactRecord(ctx context.Context, _ *mcp.CallToolRequest, args TeamEntityFactRecordArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	kind := strings.TrimSpace(args.EntityKind)
	entitySlug := strings.TrimSpace(args.EntitySlug)
	fact := strings.TrimSpace(args.Fact)
	if kind == "" {
		return toolError(fmt.Errorf("entity_kind is required")), nil, nil
	}
	if entitySlug == "" {
		return toolError(fmt.Errorf("entity_slug is required")), nil, nil
	}
	if fact == "" {
		return toolError(fmt.Errorf("fact is required")), nil, nil
	}
	source := strings.TrimSpace(args.SourcePath)
	if source != "" && !(strings.HasPrefix(source, "agents/") || strings.HasPrefix(source, "team/")) {
		return toolError(fmt.Errorf("source_path must start with agents/ or team/ when provided; got %q", source)), nil, nil
	}

	var result struct {
		FactID           string `json:"fact_id"`
		FactCount        int    `json:"fact_count"`
		ThresholdCrossed bool   `json:"threshold_crossed"`
	}
	body := map[string]any{
		"entity_kind": kind,
		"entity_slug": entitySlug,
		"fact":        fact,
		"recorded_by": slug,
	}
	if source != "" {
		body["source_path"] = source
	}
	if err := brokerPostJSON(ctx, "/entity/fact", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}

func handleEntityBriefSynthesize(ctx context.Context, _ *mcp.CallToolRequest, args TeamEntityBriefSynthesizeArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	kind := strings.TrimSpace(args.EntityKind)
	entitySlug := strings.TrimSpace(args.EntitySlug)
	if kind == "" {
		return toolError(fmt.Errorf("entity_kind is required")), nil, nil
	}
	if entitySlug == "" {
		return toolError(fmt.Errorf("entity_slug is required")), nil, nil
	}

	var result struct {
		SynthesisID uint64 `json:"synthesis_id"`
		QueuedAt    string `json:"queued_at"`
	}
	body := map[string]any{
		"entity_kind": kind,
		"entity_slug": entitySlug,
		"actor_slug":  slug,
	}
	if err := brokerPostJSON(ctx, "/entity/brief/synthesize", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}
