package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func handleTeamMembers(ctx context.Context, _ *mcp.CallToolRequest, args TeamMembersArgs) (*mcp.CallToolResult, any, error) {
	viewer := strings.TrimSpace(resolveSlugOptional(args.MySlug))
	channel := resolveConversationChannel(ctx, viewer, args.Channel)
	var result brokerMembersResponse
	values := url.Values{}
	values.Set("channel", channel)
	if viewer != "" {
		values.Set("viewer_slug", viewer)
	}
	if err := brokerGetJSON(ctx, "/members?"+values.Encode(), &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Members) == 0 {
		return textResult("No active team members yet."), nil, nil
	}
	lines := make([]string, 0, len(result.Members))
	for _, member := range result.Members {
		line := "- @" + member.Slug
		if member.Name != "" {
			line += " (" + member.Name + ")"
		}
		if member.Role != "" {
			line += " · " + member.Role
		}
		if member.Disabled {
			line += " · disabled"
		}
		if member.LastTime != "" {
			line += " at " + member.LastTime
		}
		if member.LastMessage != "" {
			line += " — " + member.LastMessage
		}
		lines = append(lines, line)
	}
	return textResult("Active team members in #" + channel + ":\n" + strings.Join(lines, "\n")), nil, nil
}

func handleTeamOfficeMembers(ctx context.Context, _ *mcp.CallToolRequest, _ TeamOfficeMembersArgs) (*mcp.CallToolResult, any, error) {
	var result brokerOfficeMembersResponse
	if err := brokerGetJSON(ctx, "/office-members", &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Members) == 0 {
		return textResult("No office members."), nil, nil
	}
	lines := make([]string, 0, len(result.Members))
	for _, member := range result.Members {
		line := fmt.Sprintf("- @%s (%s)", member.Slug, member.Name)
		if member.Role != "" {
			line += " · " + member.Role
		}
		if len(member.Expertise) > 0 {
			line += " · " + strings.Join(member.Expertise, ", ")
		}
		if member.BuiltIn {
			line += " · built-in"
		}
		lines = append(lines, line)
	}
	return textResult("Office members:\n" + strings.Join(lines, "\n")), nil, nil
}
