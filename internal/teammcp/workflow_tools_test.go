package teammcp

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/team"
)

// toolNamesForSlug spins up an in-memory MCP server configured for the given
// office-mode slug and returns the registered tool names.
func toolNamesForSlug(t *testing.T, slug string) []string {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "wuphf-team-test", Version: "0.1.0"}, nil)
	configureServerTools(server, slug, "general", false)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Wait()

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestWorkflowToolsGatedToBuilder: the workflow_* tools are registered ONLY for
// the Workflow Builder. A generic office specialist must not see them — every
// other agent hands a spec to @workflow-builder instead.
func TestWorkflowToolsGatedToBuilder(t *testing.T) {
	workflowTools := []string{
		"workflow_list", "workflow_inspect", "workflow_draft",
		"workflow_freeze", "workflow_run", "workflow_proposals", "workflow_improve",
	}

	builderNames := toolNamesForSlug(t, team.WorkflowBuilderSlug)
	for _, name := range workflowTools {
		if !slices.Contains(builderNames, name) {
			t.Errorf("workflow-builder missing tool %q; has %v", name, builderNames)
		}
	}
	// Wiki context tools ride the shared memory backend, so the Builder can
	// ground a contract.
	if !slices.Contains(builderNames, "team_wiki_search") {
		t.Errorf("workflow-builder should have team_wiki_search for Wiki context, got %v", builderNames)
	}

	specialistNames := toolNamesForSlug(t, "growth")
	for _, name := range workflowTools {
		if slices.Contains(specialistNames, name) {
			t.Errorf("generic specialist should NOT have workflow tool %q; has %v", name, specialistNames)
		}
	}
}
