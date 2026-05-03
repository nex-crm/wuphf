package team

import "net/http"

const (
	// RouteMethodAny marks legacy routes that do not enforce a method yet.
	RouteMethodAny = "*"
	// RouteMethodGetPost marks routes that intentionally support GET and POST.
	RouteMethodGetPost = "GET|POST"

	// RouteAuthNone marks routes that are intentionally available without a bearer token.
	RouteAuthNone = "none"
	// RouteAuthBearer marks routes protected by the broker bearer token.
	RouteAuthBearer = "bearer"
	// RouteAuthLoopback marks routes guarded by loopback RemoteAddr and Host checks.
	RouteAuthLoopback = "loopback"
)

// RouteContract describes the stable HTTP contract for one broker route.
type RouteContract struct {
	Domain       string
	Capability   string
	Path         string
	Method       string
	Auth         string
	RequestType  string
	ResponseType string
}

type brokerRoute struct {
	contract RouteContract
	handler  func(*Broker) http.HandlerFunc
}

var platformBrokerRoutes = []brokerRoute{
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/health",
			Method:       RouteMethodAny,
			Auth:         RouteAuthNone,
			RequestType:  "none",
			ResponseType: "team.HealthResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleHealth
		},
	},
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/version",
			Method:       http.MethodGet,
			Auth:         RouteAuthNone,
			RequestType:  "none",
			ResponseType: "buildinfo.Info",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleVersion
		},
	},
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/upgrade-check",
			Method:       http.MethodGet,
			Auth:         RouteAuthBearer,
			RequestType:  "none",
			ResponseType: "team.UpgradeCheckResponse | team.UpgradeCheckErrorResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleUpgradeCheck
		},
	},
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/upgrade-changelog",
			Method:       http.MethodGet,
			Auth:         RouteAuthBearer,
			RequestType:  "query: from, to",
			ResponseType: "team.UpgradeChangelogResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleUpgradeChangelog
		},
	},
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/upgrade/run",
			Method:       http.MethodPost,
			Auth:         RouteAuthBearer,
			RequestType:  "{}",
			ResponseType: "team.upgradeRunResult",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleUpgradeRun
		},
	},
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/usage",
			Method:       http.MethodGet,
			Auth:         RouteAuthBearer,
			RequestType:  "none",
			ResponseType: "team.teamUsageState",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleUsage
		},
	},
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/queue",
			Method:       http.MethodGet,
			Auth:         RouteAuthBearer,
			RequestType:  "none",
			ResponseType: "team.queueSnapshot",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleQueue
		},
	},
	{
		contract: RouteContract{
			Domain:       "platform",
			Capability:   "Health, usage, version, upgrade",
			Path:         "/web-token",
			Method:       RouteMethodAny,
			Auth:         RouteAuthLoopback,
			RequestType:  "none",
			ResponseType: "map[string]string",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleWebToken
		},
	},
}

var taskBrokerRoutes = []brokerRoute{
	{
		contract: RouteContract{
			Domain:       "tasks",
			Capability:   "Tasks and work evidence",
			Path:         "/tasks",
			Method:       RouteMethodGetPost,
			Auth:         RouteAuthBearer,
			RequestType:  "team.TaskListRequest | team.TaskPostRequest",
			ResponseType: "team.TaskListResponse | team.TaskResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleTasks
		},
	},
	{
		contract: RouteContract{
			Domain:       "tasks",
			Capability:   "Tasks and work evidence",
			Path:         "/tasks/ack",
			Method:       http.MethodPost,
			Auth:         RouteAuthBearer,
			RequestType:  "team.TaskAckRequest",
			ResponseType: "team.TaskResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleTaskAck
		},
	},
	{
		contract: RouteContract{
			Domain:       "tasks",
			Capability:   "Tasks and work evidence",
			Path:         "/tasks/memory-workflow",
			Method:       http.MethodPost,
			Auth:         RouteAuthBearer,
			RequestType:  "team.TaskMemoryWorkflowRequest",
			ResponseType: "team.TaskMemoryWorkflowResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleTaskMemoryWorkflow
		},
	},
	{
		contract: RouteContract{
			Domain:       "tasks",
			Capability:   "Tasks and work evidence",
			Path:         "/tasks/memory-workflow/reconcile",
			Method:       http.MethodPost,
			Auth:         RouteAuthBearer,
			RequestType:  "none",
			ResponseType: "team.TaskMemoryWorkflowReconcileResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleTaskMemoryWorkflowReconcile
		},
	},
	{
		contract: RouteContract{
			Domain:       "tasks",
			Capability:   "Tasks and work evidence",
			Path:         "/agent-logs",
			Method:       http.MethodGet,
			Auth:         RouteAuthBearer,
			RequestType:  "query: task, limit",
			ResponseType: "team.AgentLogTasksResponse | team.AgentLogEntriesResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleAgentLogs
		},
	},
	{
		contract: RouteContract{
			Domain:       "tasks",
			Capability:   "Tasks and work evidence",
			Path:         "/task-plan",
			Method:       http.MethodPost,
			Auth:         RouteAuthBearer,
			RequestType:  "team.TaskPlanRequest",
			ResponseType: "team.TaskListResponse",
		},
		handler: func(b *Broker) http.HandlerFunc {
			return b.handleTaskPlan
		},
	},
}

// BrokerRouteContracts returns the contract registry for routes that have been
// moved under explicit domain registration.
func BrokerRouteContracts() []RouteContract {
	routes := make([]RouteContract, 0, len(platformBrokerRoutes)+len(taskBrokerRoutes))
	appendContracts := func(source []brokerRoute) {
		for _, route := range source {
			routes = append(routes, route.contract)
		}
	}
	appendContracts(platformBrokerRoutes)
	appendContracts(taskBrokerRoutes)
	return routes
}

func (b *Broker) registerPlatformRoutes(mux *http.ServeMux) {
	for _, route := range platformBrokerRoutes {
		mux.HandleFunc(route.contract.Path, b.routeHandler(route))
	}
}

func (b *Broker) registerTaskRoutes(mux *http.ServeMux) {
	for _, route := range taskBrokerRoutes {
		mux.HandleFunc(route.contract.Path, b.routeHandler(route))
	}
}

func (b *Broker) routeHandler(route brokerRoute) http.HandlerFunc {
	handler := route.handler(b)
	switch route.contract.Auth {
	case RouteAuthBearer:
		return b.withAuth(handler)
	default:
		return handler
	}
}
