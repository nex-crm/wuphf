package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestSurfaceToolsRegisteredInOfficeMode(t *testing.T) {
	names := listRegisteredTools(t, "general", false)
	for _, want := range []string{
		"team_surface_list",
		"team_surface_create",
		"team_widget_read",
		"team_widget_upsert",
		"team_widget_patch",
		"team_widget_render_check",
	} {
		if !slices.Contains(names, want) {
			t.Fatalf("expected %s registered; got %v", want, names)
		}
	}
}

func TestHandleTeamWidgetPatchSendsBoundedPatch(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method=%s, want PATCH", r.Method)
		}
		if r.URL.Path != "/surfaces/launch/widgets/notes" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"widget": map[string]any{"id": "notes"},
			"render": map[string]any{"schema_ok": true, "render_ok": true},
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamWidgetPatch(context.Background(), nil, TeamWidgetPatchArgs{
		SurfaceID:   "launch",
		WidgetID:    "notes",
		Mode:        "snippet",
		Search:      "old",
		Replacement: "new",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("unexpected tool error: %s", toolErrorText(res))
	}
	if !strings.Contains(auth.lastAuth, "Bearer test-token") {
		t.Fatalf("missing auth header: %q", auth.lastAuth)
	}
	if !strings.Contains(auth.lastBody, `"actor":"pm"`) {
		t.Fatalf("expected actor in body, got %s", auth.lastBody)
	}
	if !strings.Contains(auth.lastBody, `"search":"old"`) || !strings.Contains(auth.lastBody, `"replacement":"new"`) {
		t.Fatalf("expected snippet patch body, got %s", auth.lastBody)
	}
}
