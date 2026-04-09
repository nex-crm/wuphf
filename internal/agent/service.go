package agent

import (
	"fmt"
	"sort"
	"sync"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// ManagedAgent wraps an AgentLoop with its config and last-known state.
type ManagedAgent struct {
	Config AgentConfig
	State  AgentState
	Loop   *AgentLoop
}

// ConfigUpdate holds optional fields for updating an agent's configuration.
type ConfigUpdate struct {
	Name          *string
	Expertise     []string
	HeartbeatCron *string
}

// StreamFnResolver creates a StreamFn for a given agent slug.
// The service calls this when creating an AgentLoop. It is the caller's
// responsibility to wire up the correct provider logic (e.g., based on config).
type StreamFnResolver func(agentSlug string) StreamFn

// AgentService is the singleton that manages all agent instances.
type AgentService struct {
	agents           map[string]*ManagedAgent
	toolRegistry     *ToolRegistry
	sessionStore     *SessionStore
	queues           *MessageQueues
	gossipLayer      *GossipLayer
	credTracker      *CredibilityTracker
	client           *api.Client
	streamFnResolver StreamFnResolver
	listeners        []func()
	tickTimers       map[string]chan struct{} // per-agent worker stop channels
	mu               sync.Mutex
}

// AgentServiceOption configures an AgentService.
type AgentServiceOption func(*AgentService)

// WithToolRegistry sets the tool registry.
func WithToolRegistry(r *ToolRegistry) AgentServiceOption {
	return func(s *AgentService) { s.toolRegistry = r }
}

// WithSessionStore sets the session store.
func WithSessionStore(ss *SessionStore) AgentServiceOption {
	return func(s *AgentService) { s.sessionStore = ss }
}

// WithQueues sets the message queues.
func WithQueues(q *MessageQueues) AgentServiceOption {
	return func(s *AgentService) { s.queues = q }
}

// WithClient sets the API client.
func WithClient(c *api.Client) AgentServiceOption {
	return func(s *AgentService) { s.client = c }
}

// WithGossipLayer sets the gossip layer.
func WithGossipLayer(g *GossipLayer) AgentServiceOption {
	return func(s *AgentService) { s.gossipLayer = g }
}

// WithCredibilityTracker sets the credibility tracker.
func WithCredibilityTracker(ct *CredibilityTracker) AgentServiceOption {
	return func(s *AgentService) { s.credTracker = ct }
}

// WithStreamFnResolver sets the function that resolves a StreamFn per agent slug.
// This is the integration point for provider selection (nex-ask, claude-code, gemini).
func WithStreamFnResolver(r StreamFnResolver) AgentServiceOption {
	return func(s *AgentService) { s.streamFnResolver = r }
}

// defaultStreamFnResolver returns a StreamFn that emits a configuration error.
// This is used when no real provider resolver is wired in — it tells the user
// to run /init so a provider gets configured.
func defaultStreamFnResolver(client *api.Client) StreamFnResolver {
	return func(agentSlug string) StreamFn {
		return func(msgs []Message, tools []AgentTool) <-chan StreamChunk {
			ch := make(chan StreamChunk, 1)
			go func() {
				defer close(ch)
				ch <- StreamChunk{
					Type:    "text",
					Content: "No LLM provider configured. Run /init to set up.",
				}
			}()
			return ch
		}
	}
}

// NewAgentService creates an AgentService with sensible defaults.
// Defaults: creates an API client with the resolved API key, creates a ToolRegistry
// with builtin tools, etc. Options override defaults.
func NewAgentService(opts ...AgentServiceOption) *AgentService {
	s := &AgentService{
		agents:     make(map[string]*ManagedAgent),
		tickTimers: make(map[string]chan struct{}),
	}

	for _, opt := range opts {
		opt(s)
	}

	// Defaults.
	if s.client == nil {
		apiKey := config.ResolveAPIKey("")
		s.client = api.NewClient(apiKey)
	}
	if s.toolRegistry == nil {
		s.toolRegistry = NewToolRegistry()
		if !config.ResolveNoNex() {
			for _, tool := range CreateBuiltinTools(s.client) {
				s.toolRegistry.Register(tool)
			}
		}
	}
	if s.sessionStore == nil {
		ss, err := NewSessionStore()
		if err == nil {
			s.sessionStore = ss
		}
	}
	if s.queues == nil {
		s.queues = NewMessageQueues()
	}
	if s.streamFnResolver == nil {
		s.streamFnResolver = defaultStreamFnResolver(s.client)
	}

	return s
}

// Create creates a new managed agent from the given config.
// Returns an error if the slug already exists.
func (s *AgentService) Create(cfg AgentConfig) (*ManagedAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.agents[cfg.Slug]; exists {
		return nil, fmt.Errorf("agent %q already exists", cfg.Slug)
	}

	streamFn := s.streamFnResolver(cfg.Slug)

	loop := NewAgentLoop(cfg, s.toolRegistry, s.sessionStore, s.queues, streamFn, s.gossipLayer, s.credTracker)

	ma := &ManagedAgent{
		Config: cfg,
		State:  loop.GetState(),
		Loop:   loop,
	}

	// Keep the cached state responsive without requiring callers to lock the loop.
	loop.On(EventPhaseChange, func(args ...any) {
		if len(args) >= 2 {
			if phase, ok := args[1].(AgentPhase); ok {
				ma.State.Phase = phase
			}
		}
	})
	loop.On(EventError, func(args ...any) {
		ma.State.Phase = PhaseError
		if len(args) > 0 {
			if errText, ok := args[0].(string); ok {
				ma.State.Error = errText
			}
		}
	})
	loop.On(EventDone, func(args ...any) {
		ma.State.Phase = PhaseDone
		ma.State.CurrentTask = ""
		ma.State.TaskID = ""
		ma.State.Error = ""
	})
	s.agents[cfg.Slug] = ma
	s.notify()
	return ma, nil
}

// CreateFromTemplate looks up a template by name, merges the slug, and calls Create.
func (s *AgentService) CreateFromTemplate(slug, templateName string) (*ManagedAgent, error) {
	tmpl, ok := Templates[templateName]
	if !ok {
		return nil, fmt.Errorf("unknown template: %q", templateName)
	}
	cfg := tmpl
	cfg.Slug = slug
	return s.Create(cfg)
}

// Start starts the agent loop.
func (s *AgentService) Start(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ma, err := s.requireAgent(slug)
	if err != nil {
		return err
	}

	ma.Loop.Start()
	ma.State = ma.Loop.GetState()
	s.notify()
	return nil
}

// Stop stops the agent loop and its tick timer.
func (s *AgentService) Stop(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ma, err := s.requireAgent(slug)
	if err != nil {
		return err
	}

	// Stop the tick timer goroutine if running.
	if stopCh, ok := s.tickTimers[slug]; ok {
		close(stopCh)
		delete(s.tickTimers, slug)
	}

	ma.Loop.Stop()
	ma.State = ma.Loop.GetState()
	s.notify()
	return nil
}

// Steer pushes a steering message to the agent's queue.
func (s *AgentService) Steer(slug, message string) error {
	s.mu.Lock()
	ma, err := s.requireAgent(slug)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.queues.Steer(slug, message)
	s.mu.Unlock()
	if ma.Loop.IsBusy() {
		ma.Loop.Interrupt()
	}
	s.EnsureRunning(slug)
	return nil
}

// FollowUp pushes a follow-up message to the agent's queue.
func (s *AgentService) FollowUp(slug, message string) error {
	s.mu.Lock()
	ma, err := s.requireAgent(slug)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.queues.FollowUp(slug, message)
	s.mu.Unlock()
	if ma.Loop.IsBusy() {
		ma.Loop.Interrupt()
	}
	s.EnsureRunning(slug)
	return nil
}

// EnsureRunning starts an idempotent worker that drives the agent loop immediately.
// The worker exits as soon as the agent is idle and has no queued messages.
func (s *AgentService) EnsureRunning(slug string) {
	s.mu.Lock()
	if _, ok := s.tickTimers[slug]; ok {
		s.mu.Unlock()
		return
	}

	ma, err := s.requireAgent(slug)
	if err != nil {
		s.mu.Unlock()
		return
	}
	if !ma.Loop.CanProcess() {
		s.mu.Unlock()
		return
	}

	stopCh := make(chan struct{})
	s.tickTimers[slug] = stopCh
	s.mu.Unlock()

	go s.runAgentWorker(slug, ma, stopCh)
}

func (s *AgentService) runAgentWorker(slug string, ma *ManagedAgent, stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		s.mu.Lock()
		current, ok := s.agents[slug]
		if !ok || current != ma {
			delete(s.tickTimers, slug)
			s.mu.Unlock()
			return
		}
		state := ma.Loop.GetState()
		hasMessages := s.queues.HasMessages(slug)
		shouldStop := !hasMessages && (state.Phase == PhaseIdle || state.Phase == PhaseDone || state.Phase == PhaseError)
		s.mu.Unlock()

		if shouldStop {
			s.mu.Lock()
			if current, ok := s.agents[slug]; ok && current == ma {
				delete(s.tickTimers, slug)
			}
			s.mu.Unlock()
			return
		}

		_ = ma.Loop.Tick()
		nextState := ma.Loop.GetState()

		s.mu.Lock()
		current, ok = s.agents[slug]
		if !ok || current != ma {
			delete(s.tickTimers, slug)
			s.mu.Unlock()
			return
		}
		ma.State = nextState
		running := ma.Loop.CanProcess() &&
			((nextState.Phase != PhaseDone && nextState.Phase != PhaseIdle) || s.queues.HasMessages(slug))
		s.mu.Unlock()

		if !running {
			s.mu.Lock()
			if current, ok := s.agents[slug]; ok && current == ma {
				delete(s.tickTimers, slug)
			}
			s.mu.Unlock()
			return
		}
	}
}

// Get returns the managed agent for the given slug.
func (s *AgentService) Get(slug string) (*ManagedAgent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ma, ok := s.agents[slug]
	return ma, ok
}

// List returns all managed agents, sorted by slug.
func (s *AgentService) List() []*ManagedAgent {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]*ManagedAgent, 0, len(s.agents))
	for _, ma := range s.agents {
		list = append(list, ma)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Config.Slug < list[j].Config.Slug
	})
	return list
}

// GetState returns the current state for the given agent slug.
func (s *AgentService) GetState(slug string) (AgentState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ma, ok := s.agents[slug]
	if !ok {
		return AgentState{}, false
	}
	return ma.State, true
}

// Remove stops and removes the agent from the service.
func (s *AgentService) Remove(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.agents[slug]; !ok {
		return fmt.Errorf("agent %q not found", slug)
	}

	// Stop tick timer if running.
	if stopCh, ok := s.tickTimers[slug]; ok {
		close(stopCh)
		delete(s.tickTimers, slug)
	}

	s.agents[slug].Loop.Stop()
	delete(s.agents, slug)
	s.notify()
	return nil
}

// Subscribe registers a listener that is called whenever agent state changes.
// Returns an unsubscribe function.
func (s *AgentService) Subscribe(listener func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)

	idx := len(s.listeners) - 1
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if idx < len(s.listeners) {
			s.listeners = append(s.listeners[:idx], s.listeners[idx+1:]...)
		}
	}
}

// GetTemplateNames returns the names of all built-in templates, sorted.
func (s *AgentService) GetTemplateNames() []string {
	names := make([]string, 0, len(Templates))
	for name := range Templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetTemplate returns the config for a named template.
func (s *AgentService) GetTemplate(name string) (AgentConfig, bool) {
	cfg, ok := Templates[name]
	return cfg, ok
}

// UpdateConfig updates mutable fields on a running agent's config.
func (s *AgentService) UpdateConfig(slug string, updates ConfigUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ma, err := s.requireAgent(slug)
	if err != nil {
		return err
	}

	if updates.Name != nil {
		ma.Config.Name = *updates.Name
	}
	if updates.Expertise != nil {
		ma.Config.Expertise = updates.Expertise
	}
	if updates.HeartbeatCron != nil {
		ma.Config.HeartbeatCron = *updates.HeartbeatCron
	}

	s.notify()
	return nil
}

// notify calls all listeners, swallowing panics. Must be called with mu held.
func (s *AgentService) notify() {
	for _, fn := range s.listeners {
		func() {
			defer func() { recover() }()
			fn()
		}()
	}
}

// requireAgent returns the managed agent for slug or an error if not found.
// Must be called with mu held.
func (s *AgentService) requireAgent(slug string) (*ManagedAgent, error) {
	ma, ok := s.agents[slug]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", slug)
	}
	return ma, nil
}
