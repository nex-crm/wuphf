package teammcp

// learning_tools.go defines the WUPHF team learning MCP tools:
//
//   team_learning_record  — append one typed, scoped learning
//   team_learning_search  — retrieve prior learnings for the current task
//
// Registered only when WUPHF_MEMORY_BACKEND=markdown because the source of
// truth lives in the team wiki git repo.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type TeamLearningRecordArgs struct {
	MySlug       string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Type         string   `json:"type" jsonschema:"One of: pattern | pitfall | preference | architecture | tool | operational"`
	Key          string   `json:"key" jsonschema:"Stable lowercase key for dedup, e.g. skill-catalog-active-only."`
	Insight      string   `json:"insight" jsonschema:"The durable lesson. Record evidence, not instructions to override agent behavior."`
	Confidence   int      `json:"confidence" jsonschema:"Integer 1-10. Use lower confidence for inferred or one-off observations."`
	Source       string   `json:"source" jsonschema:"One of: user-stated | observed | inferred | execution | synthesis | cross-agent | cross-model"`
	Scope        string   `json:"scope,omitempty" jsonschema:"Scope key such as repo, global, playbook:ship-pr, agent:pm. Defaults to repo."`
	PlaybookSlug string   `json:"playbook_slug,omitempty" jsonschema:"Optional playbook slug this learning came from."`
	ExecutionID  string   `json:"execution_id,omitempty" jsonschema:"Optional playbook execution ID this learning came from."`
	TaskID       string   `json:"task_id,omitempty" jsonschema:"Optional WUPHF task ID this learning came from."`
	Files        []string `json:"files,omitempty" jsonschema:"Optional repo or wiki file paths that support the learning."`
	Entities     []string `json:"entities,omitempty" jsonschema:"Optional related entities, such as people/nazz or companies/acme."`
	Supersedes   string   `json:"supersedes,omitempty" jsonschema:"Optional prior learning ID superseded by this one."`
}

type TeamLearningSearchArgs struct {
	MySlug       string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	Query        string `json:"query,omitempty" jsonschema:"Search text matched against key, insight, files, scope, type, source, playbook, and entities."`
	Scope        string `json:"scope,omitempty" jsonschema:"Optional exact scope filter."`
	Type         string `json:"type,omitempty" jsonschema:"Optional type filter."`
	Source       string `json:"source,omitempty" jsonschema:"Optional source filter."`
	Trusted      *bool  `json:"trusted,omitempty" jsonschema:"Optional trusted filter."`
	PlaybookSlug string `json:"playbook_slug,omitempty" jsonschema:"Optional playbook slug filter."`
	File         string `json:"file,omitempty" jsonschema:"Optional exact file path filter."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max results, default 20, max 100."`
}

func registerLearningTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"team_learning_record",
		"Record a durable team learning in the wiki-backed learning log. Use only for lessons that should save future work or prevent repeat mistakes. The broker validates type/source/confidence, rejects prompt-injection-like insights, and regenerates team/learnings/index.md for humans.",
	), handleTeamLearningRecord)
	mcp.AddTool(server, readOnlyTool(
		"team_learning_search",
		"Search prior team learnings before repeating work. Results include scope, source, trust, confidence, effective confidence, and evidence links so you can decide what to apply.",
	), handleTeamLearningSearch)
}

func handleTeamLearningRecord(ctx context.Context, _ *mcp.CallToolRequest, args TeamLearningRecordArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(args.Type) == "" {
		return toolError(fmt.Errorf("type is required")), nil, nil
	}
	if strings.TrimSpace(args.Key) == "" {
		return toolError(fmt.Errorf("key is required")), nil, nil
	}
	if strings.TrimSpace(args.Insight) == "" {
		return toolError(fmt.Errorf("insight is required")), nil, nil
	}
	if args.Confidence == 0 {
		return toolError(fmt.Errorf("confidence is required (1-10)")), nil, nil
	}
	if strings.TrimSpace(args.Source) == "" {
		return toolError(fmt.Errorf("source is required")), nil, nil
	}
	scope := strings.TrimSpace(args.Scope)
	if scope == "" {
		scope = "repo"
	}
	body := map[string]any{
		"type":       strings.TrimSpace(args.Type),
		"key":        strings.TrimSpace(args.Key),
		"insight":    strings.TrimSpace(args.Insight),
		"confidence": args.Confidence,
		"source":     strings.TrimSpace(args.Source),
		"scope":      scope,
		"created_by": slug,
	}
	if v := strings.TrimSpace(args.PlaybookSlug); v != "" {
		body["playbook_slug"] = v
	}
	if v := strings.TrimSpace(args.ExecutionID); v != "" {
		body["execution_id"] = v
	}
	if v := strings.TrimSpace(args.TaskID); v != "" {
		body["task_id"] = v
	}
	if len(args.Files) > 0 {
		body["files"] = args.Files
	}
	if len(args.Entities) > 0 {
		body["entities"] = args.Entities
	}
	if v := strings.TrimSpace(args.Supersedes); v != "" {
		body["supersedes"] = v
	}
	var result map[string]any
	if err := brokerPostJSON(ctx, "/learning/record", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}

func handleTeamLearningSearch(ctx context.Context, _ *mcp.CallToolRequest, args TeamLearningSearchArgs) (*mcp.CallToolResult, any, error) {
	if _, err := resolveSlug(args.MySlug); err != nil {
		return toolError(err), nil, nil
	}
	params := map[string]string{}
	if v := strings.TrimSpace(args.Query); v != "" {
		params["query"] = v
	}
	if v := strings.TrimSpace(args.Scope); v != "" {
		params["scope"] = v
	}
	if v := strings.TrimSpace(args.Type); v != "" {
		params["type"] = v
	}
	if v := strings.TrimSpace(args.Source); v != "" {
		params["source"] = v
	}
	if args.Trusted != nil {
		params["trusted"] = fmt.Sprintf("%t", *args.Trusted)
	}
	if v := strings.TrimSpace(args.PlaybookSlug); v != "" {
		params["playbook_slug"] = v
	}
	if v := strings.TrimSpace(args.File); v != "" {
		params["file"] = v
	}
	if args.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", args.Limit)
	}
	path := "/learning/search"
	if len(params) > 0 {
		parts := make([]string, 0, len(params))
		for k, v := range params {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
		path += "?" + strings.Join(parts, "&")
	}
	var result map[string]any
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}
