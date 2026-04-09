package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// GossipEvent represents an observed output event from one agent.
type GossipEvent struct {
	FromSlug  string
	Type      string // "thinking", "text", "tool_use", "tool_result"
	Content   string
	Timestamp time.Time
}

// GossipTarget is what the bus writes to (decoupled from TerminalPane).
type GossipTarget interface {
	SendText(text string)
	Slug() string
}

// Turn-taking constants.
const (
	leaderFloorDuration = 5 * time.Second
	agentCooldown       = 3 * time.Second
	injectionThrottle   = 3 * time.Second
	maxTextLen          = 300
	maxThinkingLen      = 200
	maxToolResultLen    = 100
)

// GossipBus captures each agent's output and shares it with others
// using a turn-taking protocol for organic, natural conversations.
type GossipBus struct {
	panes    map[string]GossipTarget
	eventLog []GossipEvent
	subs     map[int]chan GossipEvent
	nextSub  int

	// Turn-taking state
	leaderSlug  string
	floorHolder string
	floorUntil  time.Time
	cooldowns   map[string]time.Time

	// Throttling
	lastInjection map[string]time.Time
	pendingBatch  map[string][]GossipEvent

	// For testing: injectable clock
	now func() time.Time

	mu sync.Mutex
}

// NewGossipBus creates a GossipBus with the given leader slug (e.g. "ceo").
func NewGossipBus(leaderSlug string) *GossipBus {
	return &GossipBus{
		panes:         make(map[string]GossipTarget),
		leaderSlug:    leaderSlug,
		cooldowns:     make(map[string]time.Time),
		lastInjection: make(map[string]time.Time),
		pendingBatch:  make(map[string][]GossipEvent),
		subs:          make(map[int]chan GossipEvent),
		now:           time.Now,
	}
}

// RegisterTarget adds a pane to the bus.
func (b *GossipBus) RegisterTarget(target GossipTarget) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.panes[target.Slug()] = target
}

// SubscribeEvents receives raw emitted events as they happen.
func (b *GossipBus) SubscribeEvents(buffer int) (<-chan GossipEvent, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan GossipEvent, buffer)

	b.mu.Lock()
	id := b.nextSub
	b.nextSub++
	b.subs[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// BroadcastUserMessage sends text to all targets and gives the leader the floor.
func (b *GossipBus) BroadcastUserMessage(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()

	// Give leader the floor first
	b.floorHolder = b.leaderSlug
	b.floorUntil = now.Add(leaderFloorDuration)

	// Send to all targets
	formatted := fmt.Sprintf("[USER]: %s\n", text)
	for _, target := range b.panes {
		target.SendText(formatted)
	}
}

// GiveFloor explicitly gives an agent the floor for a duration.
func (b *GossipBus) GiveFloor(slug string, duration time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.floorHolder = slug
	b.floorUntil = b.now().Add(duration)
}

// Emit handles a gossip event with turn-taking and broadcasts to eligible targets.
func (b *GossipBus) Emit(event GossipEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = b.now()
	}
	b.eventLog = append(b.eventLog, event)
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}

	now := b.now()

	// Check if emitter has the floor or floor is expired
	if b.floorHolder != "" && b.floorHolder != event.FromSlug && now.Before(b.floorUntil) {
		// Someone else holds the floor — batch but don't inject yet
		b.batchForAll(event)
		return
	}

	// Check cooldown for emitter
	if cd, ok := b.cooldowns[event.FromSlug]; ok && now.Before(cd) {
		b.batchForAll(event)
		return
	}

	// Emitter can speak — set their cooldown
	if event.Type == "text" {
		b.cooldowns[event.FromSlug] = now.Add(agentCooldown)
		// If the leader just spoke, release the floor
		if event.FromSlug == b.leaderSlug && b.floorHolder == b.leaderSlug {
			b.floorHolder = ""
		}
	}

	// Inject to all OTHER targets
	for slug, target := range b.panes {
		if slug == event.FromSlug {
			continue
		}
		b.scheduleInjection(slug, target, event)
	}
}

// batchForAll queues an event for all targets except the emitter.
func (b *GossipBus) batchForAll(event GossipEvent) {
	for slug := range b.panes {
		if slug == event.FromSlug {
			continue
		}
		b.pendingBatch[slug] = append(b.pendingBatch[slug], event)
	}
}

// scheduleInjection injects context to a target, respecting throttle limits.
func (b *GossipBus) scheduleInjection(slug string, target GossipTarget, event GossipEvent) {
	now := b.now()

	// Prepend any pending batch
	pending := b.pendingBatch[slug]
	events := append(pending, event)
	b.pendingBatch[slug] = nil

	lastInj := b.lastInjection[slug]
	if now.Sub(lastInj) < injectionThrottle {
		// Re-batch: too soon
		b.pendingBatch[slug] = events
		return
	}

	b.lastInjection[slug] = now
	injectContext(target, events)
}

// FlushPending forces delivery of all batched events (used when floor expires or on timer).
func (b *GossipBus) FlushPending() {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	for slug, events := range b.pendingBatch {
		if len(events) == 0 {
			continue
		}
		target, ok := b.panes[slug]
		if !ok {
			continue
		}
		b.lastInjection[slug] = now
		injectContext(target, events)
		b.pendingBatch[slug] = nil
	}
}

// EventLog returns a copy of the event log.
func (b *GossipBus) EventLog() []GossipEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]GossipEvent, len(b.eventLog))
	copy(out, b.eventLog)
	return out
}

// GetActivity returns the latest activity type for an agent based on recent events.
// Returns "idle" if no events found for the slug.
func (b *GossipBus) GetActivity(slug string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Walk backward to find most recent event from this agent.
	for i := len(b.eventLog) - 1; i >= 0; i-- {
		if b.eventLog[i].FromSlug == slug {
			return b.eventLog[i].Type
		}
	}
	return "idle"
}

// injectContext writes formatted team context to a target.
func injectContext(target GossipTarget, events []GossipEvent) {
	var lines []string
	for _, ev := range events {
		lines = append(lines, formatEvent(ev))
	}
	text := strings.Join(lines, "\n") + "\n"
	target.SendText(text)
}

// formatEvent renders a GossipEvent as a team chat line.
func formatEvent(ev GossipEvent) string {
	content := truncate(ev.Content, maxLenForType(ev.Type))
	switch ev.Type {
	case "tool_use":
		return fmt.Sprintf("[TEAM @%s is coding]: %s", ev.FromSlug, content)
	case "tool_result":
		return fmt.Sprintf("[TEAM @%s tool done]: %s", ev.FromSlug, content)
	case "thinking":
		return fmt.Sprintf("[TEAM @%s thinking]: %s", ev.FromSlug, content)
	default:
		return fmt.Sprintf("[TEAM @%s]: %s", ev.FromSlug, content)
	}
}

func maxLenForType(t string) int {
	switch t {
	case "thinking":
		return maxThinkingLen
	case "tool_result":
		return maxToolResultLen
	default:
		return maxTextLen
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
