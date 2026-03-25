package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nex-crm/wuphf/internal/api"
)

// ToolRegistry manages a set of named AgentTools.
type ToolRegistry struct {
	tools map[string]AgentTool
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]AgentTool)}
}

// Register adds or replaces a tool in the registry.
func (r *ToolRegistry) Register(tool AgentTool) {
	r.tools[tool.Name] = tool
}

// Unregister removes a tool from the registry.
func (r *ToolRegistry) Unregister(name string) {
	delete(r.tools, name)
}

// Get looks up a tool by name.
func (r *ToolRegistry) Get(name string) (AgentTool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *ToolRegistry) List() []AgentTool {
	tools := make([]AgentTool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// Has reports whether a tool with the given name is registered.
func (r *ToolRegistry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// Validate checks whether params are valid for the named tool.
// Checks: tool exists, required params present, no unknown params.
// Returns (true, nil) on success; (false, []errors) on failure.
func (r *ToolRegistry) Validate(toolName string, params map[string]any) (bool, []string) {
	tool, ok := r.tools[toolName]
	if !ok {
		return false, []string{fmt.Sprintf("unknown tool: %q", toolName)}
	}

	props := map[string]any{}
	if p, ok := tool.Schema["properties"].(map[string]any); ok {
		props = p
	}

	var errs []string

	if req, ok := tool.Schema["required"].([]any); ok {
		for _, v := range req {
			if name, ok := v.(string); ok {
				if _, present := params[name]; !present {
					errs = append(errs, fmt.Sprintf("missing required param: %q", name))
				}
			}
		}
	}

	for k := range params {
		if _, known := props[k]; !known {
			errs = append(errs, fmt.Sprintf("unknown param: %q", k))
		}
	}

	if len(errs) > 0 {
		return false, errs
	}
	return true, nil
}

// marshalResult marshals v to a JSON string.
func marshalResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// CreateBuiltinTools returns the 7 standard Nex tools backed by the API client.
func CreateBuiltinTools(client *api.Client) []AgentTool {
	return []AgentTool{
		{
			Name:        "nex_search",
			Description: "Search organizational knowledge base",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "number"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Searching...")
				result, err := api.Post[any](client, "/search", map[string]any{
					"query": params["query"],
					"limit": params["limit"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_ask",
			Description: "Ask a question to the Nex knowledge base",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"question"},
				"properties": map[string]any{
					"question": map[string]any{"type": "string"},
					"context":  map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Asking...")
				result, err := api.Post[any](client, "/ask", map[string]any{
					"question": params["question"],
					"context":  params["context"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_remember",
			Description: "Store information in the Nex knowledge base",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"content"},
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
					"tags":    map[string]any{"type": "array"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Remembering...")
				result, err := api.Post[any](client, "/remember", map[string]any{
					"content": params["content"],
					"tags":    params["tags"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_list",
			Description: "List records of a given object type",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"limit":      map[string]any{"type": "number"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Fetching records...")
				objectType, _ := params["objectType"].(string)
				path := "/records/" + objectType
				if limit, ok := params["limit"]; ok {
					path += fmt.Sprintf("?limit=%v", limit)
				}
				result, err := api.Get[any](client, path, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_get",
			Description: "Get a specific record by type and ID",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType", "recordId"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"recordId":   map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Fetching record...")
				objectType, _ := params["objectType"].(string)
				recordId, _ := params["recordId"].(string)
				result, err := api.Get[any](client, "/records/"+objectType+"/"+recordId, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_create",
			Description: "Create a new record of a given object type",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType", "properties"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"properties": map[string]any{"type": "object"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Creating record...")
				objectType, _ := params["objectType"].(string)
				result, err := api.Post[any](client, "/records/"+objectType, map[string]any{
					"properties": params["properties"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
		{
			Name:        "nex_record_update",
			Description: "Update an existing record by type and ID",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"objectType", "recordId", "properties"},
				"properties": map[string]any{
					"objectType": map[string]any{"type": "string"},
					"recordId":   map[string]any{"type": "string"},
					"properties": map[string]any{"type": "object"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Updating record...")
				objectType, _ := params["objectType"].(string)
				recordId, _ := params["recordId"].(string)
				result, err := api.Patch[any](client, "/records/"+objectType+"/"+recordId, map[string]any{
					"properties": params["properties"],
				}, 0)
				if err != nil {
					return "", err
				}
				return marshalResult(result)
			},
		},
	}
}

// CreateGossipTools returns tools for publishing and querying the gossip network.
func CreateGossipTools(gossipLayer *GossipLayer, agentSlug string) []AgentTool {
	return []AgentTool{
		{
			Name:        "nex_gossip_publish",
			Description: "Publish an insight for other agents",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"insight"},
				"properties": map[string]any{
					"insight": map[string]any{"type": "string"},
					"context": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Publishing insight...")
				insight, _ := params["insight"].(string)
				contextStr, _ := params["context"].(string)
				return gossipLayer.Publish(agentSlug, insight, contextStr)
			},
		},
		{
			Name:        "nex_gossip_query",
			Description: "Query gossip network for insights",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"topic"},
				"properties": map[string]any{
					"topic": map[string]any{"type": "string"},
				},
			},
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				onUpdate("Querying gossip network...")
				topic, _ := params["topic"].(string)
				insights, err := gossipLayer.Query(agentSlug, topic)
				if err != nil {
					return "", err
				}
				b, err := json.Marshal(insights)
				if err != nil {
					return "", err
				}
				return string(b), nil
			},
		},
	}
}
