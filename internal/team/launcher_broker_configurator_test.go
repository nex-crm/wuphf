package team

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLauncherInstallBrokerRunsConfiguratorBeforeWorkspaceTraffic(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{}
	orch := &fakeOrchestrator{
		listResp: []Workspace{{
			Name:        "main",
			RuntimeHome: t.TempDir(),
			BrokerPort:  7890,
			WebPort:     7891,
			State:       "running",
		}},
	}
	l.SetBrokerConfigurator(func(b *Broker) {
		b.SetWorkspaceOrchestrator(orch)
	})

	l.installBroker(b)

	req := httptest.NewRequest(http.MethodGet, "/workspaces/list", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	rec := httptest.NewRecorder()
	b.withAuth(b.handleWorkspacesList).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("workspace list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "workspaces not configured") {
		t.Fatalf("workspace route saw an unconfigured broker: %s", rec.Body.String())
	}
	if got := orch.callTrace(); len(got) != 1 || got[0] != "list" {
		t.Fatalf("workspace orchestrator calls = %v, want [list]", got)
	}
}
