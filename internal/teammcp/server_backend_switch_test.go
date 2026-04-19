package teammcp

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestConfigureServerToolsBackendMatrix is the IRON regression test for
// backend-axis tool registration. Markdown and Nex/GBrain must NEVER coexist
// on one server instance — if someone breaks this, existing users lose
// shared memory silently.
//
// For each backend we assert:
//   - the expected tool set is registered
//   - the other backend's tool set is NOT registered
//   - the office communication tools (team_broadcast, etc.) are unaffected
func TestConfigureServerToolsBackendMatrix(t *testing.T) {
	testCases := []struct {
		name          string
		backend       string
		oneOnOne      bool
		mustHave      []string
		mustNotHave   []string
		commonPresent []string // must be present regardless of backend
	}{
		{
			name:    "markdown/office",
			backend: "markdown",
			mustHave: []string{
				"team_wiki_read",
				"team_wiki_write",
				"team_wiki_search",
				"team_wiki_list",
			},
			mustNotHave: []string{
				"team_memory_query",
				"team_memory_write",
				"team_memory_promote",
			},
			commonPresent: []string{"team_broadcast", "team_poll"},
		},
		{
			name:    "nex/office",
			backend: "nex",
			mustHave: []string{
				"team_memory_query",
				"team_memory_write",
				"team_memory_promote",
			},
			mustNotHave: []string{
				"team_wiki_read",
				"team_wiki_write",
				"team_wiki_search",
				"team_wiki_list",
			},
			commonPresent: []string{"team_broadcast", "team_poll"},
		},
		{
			name:    "gbrain/office",
			backend: "gbrain",
			mustHave: []string{
				"team_memory_query",
				"team_memory_write",
				"team_memory_promote",
			},
			mustNotHave: []string{
				"team_wiki_read",
				"team_wiki_write",
				"team_wiki_search",
				"team_wiki_list",
			},
			commonPresent: []string{"team_broadcast", "team_poll"},
		},
		{
			name:    "none/office",
			backend: "none",
			mustHave: []string{},
			mustNotHave: []string{
				"team_memory_query",
				"team_memory_write",
				"team_memory_promote",
				"team_wiki_read",
				"team_wiki_write",
				"team_wiki_search",
				"team_wiki_list",
			},
			commonPresent: []string{"team_broadcast", "team_poll"},
		},
		{
			name:     "markdown/dm",
			backend:  "markdown",
			oneOnOne: false,
			mustHave: []string{
				"team_wiki_read",
				"team_wiki_write",
				"team_wiki_search",
				"team_wiki_list",
			},
			mustNotHave: []string{
				"team_memory_query",
				"team_memory_write",
				"team_memory_promote",
			},
			commonPresent: []string{"team_broadcast", "team_poll"},
		},
		{
			name:     "markdown/oneOnOne",
			backend:  "markdown",
			oneOnOne: true,
			mustHave: []string{
				"team_wiki_read",
				"team_wiki_write",
				"team_wiki_search",
				"team_wiki_list",
			},
			mustNotHave: []string{
				"team_memory_query",
				"team_memory_write",
				"team_memory_promote",
			},
			commonPresent: []string{"reply", "read_conversation"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			t.Setenv("WUPHF_MEMORY_BACKEND", tc.backend)

			channel := "general"
			if tc.name == "markdown/dm" {
				channel = "dm-ceo"
			}

			// Act
			names := listRegisteredTools(t, channel, tc.oneOnOne)

			// Assert
			for _, want := range tc.mustHave {
				if !slices.Contains(names, want) {
					t.Errorf("backend=%s expected %q to be registered; got %v", tc.backend, want, names)
				}
			}
			for _, wantAbsent := range tc.mustNotHave {
				if slices.Contains(names, wantAbsent) {
					t.Errorf("backend=%s expected %q to NOT be registered; got %v", tc.backend, wantAbsent, names)
				}
			}
			for _, common := range tc.commonPresent {
				if !slices.Contains(names, common) {
					t.Errorf("backend=%s expected common tool %q to be registered; got %v", tc.backend, common, names)
				}
			}
		})
	}
}

// listRegisteredTools stands up an in-memory MCP server, calls
// configureServerTools with the given social-axis context, and returns the
// list of registered tool names.
func listRegisteredTools(t *testing.T, channel string, oneOnOne bool) []string {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "wuphf-team-test", Version: "0.1.0"}, nil)
	configureServerTools(server, "workflow-architect", channel, oneOnOne)

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
