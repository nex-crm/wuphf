package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/team"
)

func handleTeamMemoryQuery(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemoryQueryArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return toolError(fmt.Errorf("query is required")), nil, nil
	}
	scope, err := normalizeMemoryScope(args.Scope)
	if err != nil {
		return toolError(err), nil, nil
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}

	lines := []string{}
	privateEntries := []brokerMemoryNote{}
	sharedHits := []team.ScopedMemoryHit{}
	if scope == "auto" || scope == "private" {
		values := url.Values{}
		values.Set("namespace", privateMemoryNamespace(mySlug))
		values.Set("query", query)
		values.Set("limit", fmt.Sprintf("%d", limit))
		var result brokerMemoryResponse
		if err := brokerGetJSON(ctx, "/memory?"+values.Encode(), &result); err != nil {
			return toolError(err), nil, nil
		}
		privateEntries = append(privateEntries, result.Entries...)
		if len(result.Entries) > 0 {
			lines = append(lines, "Private memory:")
			for _, entry := range result.Entries {
				title := strings.TrimSpace(entry.Title)
				if title == "" {
					title = strings.TrimSpace(entry.Key)
				}
				lines = append(lines, fmt.Sprintf("- %s (%s): %s", title, entry.Key, truncate(strings.TrimSpace(strings.ReplaceAll(entry.Content, "\n", " ")), 220)))
			}
		}
	}
	if scope == "auto" || scope == "shared" {
		hits, err := team.QuerySharedMemory(ctx, query, limit)
		if err != nil {
			return toolError(err), nil, nil
		}
		sharedHits = append(sharedHits, hits...)
		if len(hits) > 0 {
			header := "Shared memory:"
			if scope == "shared" {
				header = fmt.Sprintf("Shared %s memory:", strings.ToUpper(hits[0].Backend[:1])+hits[0].Backend[1:])
			}
			lines = append(lines, header)
			for _, hit := range hits {
				lines = append(lines, fmt.Sprintf("- %s (%s): %s", hit.Title, hit.Identifier, truncate(strings.TrimSpace(hit.Snippet), 220)))
			}
		} else if scope == "shared" {
			lines = append(lines, "Shared memory: no relevant hits.")
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "No memory hits.")
	}
	if hints := promotionHintsForNotes(privateEntries); len(hints) > 0 {
		lines = append(lines, "", "Promotion hints:")
		lines = append(lines, hints...)
	}
	if len(sharedHits) > 0 {
		var office brokerOfficeMembersResponse
		if err := brokerGetJSON(ctx, "/office-members", &office); err == nil {
			if hints := sharedMemoryRoutingHints(mySlug, sharedHits, office); len(hints) > 0 {
				lines = append(lines, "", "Routing hints:")
				lines = append(lines, hints...)
			}
		}
	}
	return textResult(strings.Join(lines, "\n")), nil, nil
}

func handleTeamMemoryWrite(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemoryWriteArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	visibility, err := normalizeMemoryVisibility(args.Visibility)
	if err != nil {
		return toolError(err), nil, nil
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	key := derivedMemoryKey(args.Key, args.Title, content)
	title := strings.TrimSpace(args.Title)
	if visibility == "shared" {
		identifier, err := team.WriteSharedMemory(ctx, team.SharedMemoryWrite{
			Actor:   mySlug,
			Key:     key,
			Title:   title,
			Content: content,
		})
		if err != nil {
			return toolError(err), nil, nil
		}
		return textResult(fmt.Sprintf("Stored shared memory %s.", strings.TrimSpace(identifier))), nil, nil
	}
	if err := brokerPostJSON(ctx, "/memory", map[string]any{
		"namespace": privateMemoryNamespace(mySlug),
		"key":       key,
		"value": map[string]any{
			"key":     key,
			"title":   title,
			"content": content,
			"author":  mySlug,
		},
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	lines := []string{fmt.Sprintf("Saved private note %s.", key)}
	if hints := promotionHintsForNotes([]brokerMemoryNote{{
		Key:     key,
		Title:   title,
		Content: content,
		Author:  mySlug,
	}}); len(hints) > 0 {
		lines = append(lines, hints...)
	}
	return textResult(strings.Join(lines, "\n")), nil, nil
}

func handleTeamMemoryPromote(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemoryPromoteArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	key := normalizeMemoryKey(args.Key)
	if key == "" {
		return toolError(fmt.Errorf("key is required")), nil, nil
	}
	values := url.Values{}
	values.Set("namespace", privateMemoryNamespace(mySlug))
	values.Set("key", key)
	var result brokerMemoryResponse
	if err := brokerGetJSON(ctx, "/memory?"+values.Encode(), &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Entries) == 0 {
		return toolError(fmt.Errorf("private note %q not found", key)), nil, nil
	}
	entry := result.Entries[0]
	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = strings.TrimSpace(entry.Title)
	}
	identifier, err := team.WriteSharedMemory(ctx, team.SharedMemoryWrite{
		Actor:   mySlug,
		Key:     entry.Key,
		Title:   title,
		Content: strings.TrimSpace(entry.Content),
	})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Promoted private note %s into shared memory as %s.", entry.Key, strings.TrimSpace(identifier))), nil, nil
}

func normalizeMemoryScope(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "auto":
		return "auto", nil
	case "private":
		return "private", nil
	case "shared":
		return "shared", nil
	default:
		return "", fmt.Errorf("invalid scope %q", value)
	}
}

func normalizeMemoryVisibility(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "private":
		return "private", nil
	case "shared":
		return "shared", nil
	default:
		return "", fmt.Errorf("invalid visibility %q", value)
	}
}

func privateMemoryNamespace(slug string) string {
	return "agent/" + strings.TrimSpace(slug)
}

func normalizeMemoryKey(key string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(strings.ToLower(key)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func derivedMemoryKey(explicit string, title string, content string) string {
	if key := normalizeMemoryKey(explicit); key != "" {
		return key
	}
	if key := normalizeMemoryKey(title); key != "" {
		return key + "-" + time.Now().UTC().Format("20060102-150405")
	}
	words := strings.Fields(content)
	if len(words) > 6 {
		words = words[:6]
	}
	if key := normalizeMemoryKey(strings.Join(words, " ")); key != "" {
		return key + "-" + time.Now().UTC().Format("20060102-150405")
	}
	return "note-" + time.Now().UTC().Format("20060102-150405")
}
