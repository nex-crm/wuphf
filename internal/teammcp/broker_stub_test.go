package teammcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// broker_stub_test.go holds the shared broker-stub test helpers used across
// the teammcp tool tests. They were previously colocated with the (now
// removed) notebook tool tests; they live here so the surviving tests keep
// compiling.

// stubBroker is an httptest server that records the last request and returns
// whatever the supplied handler writes.
func stubBroker(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *testingAuth) {
	t.Helper()
	auth := &testingAuth{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.lastAuth = r.Header.Get("Authorization")
		auth.lastPath = r.URL.Path
		auth.lastRaw = r.URL.RawQuery
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			auth.lastBody = string(body)
		}
		handler(w, r)
	}))
	return srv, auth
}

type testingAuth struct {
	lastAuth string
	lastPath string
	lastRaw  string
	lastBody string
}

func withBrokerURL(t *testing.T, url string) {
	t.Helper()
	t.Setenv("WUPHF_TEAM_BROKER_URL", url)
	t.Setenv("WUPHF_BROKER_TOKEN", "test-token")
	t.Setenv("WUPHF_BROKER_TOKEN_FILE", "/dev/null")
}

// isToolError reports whether the MCP tool result is an error result.
func isToolError(res *mcp.CallToolResult) bool {
	if res == nil {
		return false
	}
	return res.IsError
}

// toolErrorText returns the first text content of an MCP tool result.
func toolErrorText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// listRegisteredToolsWithSlug boots an in-memory MCP server with the given
// actor identity and returns the names of every registered tool.
func listRegisteredToolsWithSlug(t *testing.T, slug, channel string, oneOnOne bool) []string {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "wuphf-team-test", Version: "0.1.0"}, nil)
	configureServerTools(server, slug, channel, oneOnOne)

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
	defer func() { _ = clientSession.Close() }()

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
