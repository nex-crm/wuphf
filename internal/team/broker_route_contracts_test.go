package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nex-crm/wuphf/internal/buildinfo"
)

func TestBrokerRouteContracts_PlatformRoutes(t *testing.T) {
	routes := routeContractsByPath(BrokerRouteContracts())
	tests := []struct {
		path         string
		method       string
		auth         string
		responseType string
	}{
		{path: "/health", method: RouteMethodAny, auth: RouteAuthNone, responseType: "team.HealthResponse"},
		{path: "/version", method: http.MethodGet, auth: RouteAuthNone, responseType: "buildinfo.Info"},
		{path: "/upgrade-check", method: http.MethodGet, auth: RouteAuthBearer, responseType: "team.UpgradeCheckResponse | team.UpgradeCheckErrorResponse"},
		{path: "/upgrade-changelog", method: http.MethodGet, auth: RouteAuthBearer, responseType: "team.UpgradeChangelogResponse"},
		{path: "/upgrade/run", method: http.MethodPost, auth: RouteAuthBearer, responseType: "team.upgradeRunResult"},
		{path: "/usage", method: http.MethodGet, auth: RouteAuthBearer, responseType: "team.teamUsageState"},
		{path: "/queue", method: http.MethodGet, auth: RouteAuthBearer, responseType: "team.queueSnapshot"},
		{path: "/web-token", method: RouteMethodAny, auth: RouteAuthLoopback, responseType: "map[string]string"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			route, ok := routes[tt.path]
			if !ok {
				t.Fatalf("missing route contract")
			}
			if route.Domain != "platform" {
				t.Fatalf("domain: want platform, got %q", route.Domain)
			}
			if route.Method != tt.method {
				t.Fatalf("method: want %q, got %q", tt.method, route.Method)
			}
			if route.Auth != tt.auth {
				t.Fatalf("auth: want %q, got %q", tt.auth, route.Auth)
			}
			if route.ResponseType != tt.responseType {
				t.Fatalf("response type: want %q, got %q", tt.responseType, route.ResponseType)
			}
		})
	}
}

func TestBrokerRouteContracts_TaskRoutes(t *testing.T) {
	routes := routeContractsByPath(BrokerRouteContracts())
	tests := []struct {
		path         string
		method       string
		requestType  string
		responseType string
	}{
		{
			path:         "/tasks",
			method:       RouteMethodGetPost,
			requestType:  "team.TaskListRequest | team.TaskPostRequest",
			responseType: "team.TaskListResponse | team.TaskResponse",
		},
		{
			path:         "/tasks/ack",
			method:       http.MethodPost,
			requestType:  "team.TaskAckRequest",
			responseType: "team.TaskResponse",
		},
		{
			path:         "/tasks/memory-workflow",
			method:       http.MethodPost,
			requestType:  "team.TaskMemoryWorkflowRequest",
			responseType: "team.TaskMemoryWorkflowResponse",
		},
		{
			path:         "/tasks/memory-workflow/reconcile",
			method:       http.MethodPost,
			requestType:  "none",
			responseType: "team.TaskMemoryWorkflowReconcileResponse",
		},
		{
			path:         "/agent-logs",
			method:       http.MethodGet,
			requestType:  "query: task, limit",
			responseType: "team.AgentLogTasksResponse | team.AgentLogEntriesResponse",
		},
		{
			path:         "/task-plan",
			method:       http.MethodPost,
			requestType:  "team.TaskPlanRequest",
			responseType: "team.TaskListResponse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			route, ok := routes[tt.path]
			if !ok {
				t.Fatalf("missing route contract")
			}
			if route.Domain != "tasks" {
				t.Fatalf("domain: want tasks, got %q", route.Domain)
			}
			if route.Capability != "Tasks and work evidence" {
				t.Fatalf("capability: want Tasks and work evidence, got %q", route.Capability)
			}
			if route.Method != tt.method {
				t.Fatalf("method: want %q, got %q", tt.method, route.Method)
			}
			if route.Auth != RouteAuthBearer {
				t.Fatalf("auth: want bearer, got %q", route.Auth)
			}
			if route.RequestType != tt.requestType {
				t.Fatalf("request type: want %q, got %q", tt.requestType, route.RequestType)
			}
			if route.ResponseType != tt.responseType {
				t.Fatalf("response type: want %q, got %q", tt.responseType, route.ResponseType)
			}
		})
	}
}

func TestRegisterPlatformRoutesServesContractedRoutes(t *testing.T) {
	b := newTestBroker(t)
	mux := http.NewServeMux()
	b.registerPlatformRoutes(mux)

	healthRec := httptest.NewRecorder()
	mux.ServeHTTP(healthRec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if healthRec.Code != http.StatusOK {
		t.Fatalf("/health: want 200, got %d", healthRec.Code)
	}
	var health HealthResponse
	if err := json.NewDecoder(healthRec.Body).Decode(&health); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("/health status: want ok, got %q", health.Status)
	}

	versionRec := httptest.NewRecorder()
	mux.ServeHTTP(versionRec, httptest.NewRequest(http.MethodGet, "/version", nil))
	if versionRec.Code != http.StatusOK {
		t.Fatalf("/version: want 200, got %d", versionRec.Code)
	}
	var version buildinfo.Info
	if err := json.NewDecoder(versionRec.Body).Decode(&version); err != nil {
		t.Fatalf("decode /version: %v", err)
	}
	if version != buildinfo.Current() {
		t.Fatalf("/version: want %+v, got %+v", buildinfo.Current(), version)
	}
}

func TestRegisterPlatformRoutesProtectsBearerRoutes(t *testing.T) {
	b := newTestBroker(t)
	b.token = "route-contract-token"
	mux := http.NewServeMux()
	b.registerPlatformRoutes(mux)

	for _, path := range []string{"/upgrade-check", "/upgrade-changelog", "/upgrade/run", "/usage", "/queue"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("want 401 without bearer auth, got %d", rec.Code)
			}
		})
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web-token", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("/web-token should keep loopback guard when registered through platform routes, got %d", rec.Code)
	}
}

func TestRegisterTaskRoutesProtectsBearerRoutes(t *testing.T) {
	b := newTestBroker(t)
	b.token = "route-contract-token"
	mux := http.NewServeMux()
	b.registerTaskRoutes(mux)

	for _, path := range []string{"/tasks", "/tasks/ack", "/tasks/memory-workflow", "/tasks/memory-workflow/reconcile", "/agent-logs", "/task-plan"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("want 401 without bearer auth, got %d", rec.Code)
			}
		})
	}
}

func routeContractsByPath(routes []RouteContract) map[string]RouteContract {
	out := make(map[string]RouteContract, len(routes))
	for _, route := range routes {
		out[route.Path] = route
	}
	return out
}
