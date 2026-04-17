package agent

import "sync"

// MessageQueues manages per-agent steer and follow-up message queues.
type MessageQueues struct {
	mu             sync.Mutex
	steerQueues    map[string][]string
	followUpQueues map[string][]string
}

// NewMessageQueues creates an empty MessageQueues.
func NewMessageQueues() *MessageQueues {
	return &MessageQueues{
		steerQueues:    make(map[string][]string),
		followUpQueues: make(map[string][]string),
	}
}

// Steer enqueues a steering message for the given agent.
func (q *MessageQueues) Steer(agentSlug, message string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steerQueues[agentSlug] = append(q.steerQueues[agentSlug], message)
}

// FollowUp enqueues a follow-up message for the given agent.
func (q *MessageQueues) FollowUp(agentSlug, message string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.followUpQueues[agentSlug] = append(q.followUpQueues[agentSlug], message)
}

// DrainSteer removes and returns the front steer message for the agent.
// Returns ("", false) if the queue is empty.
func (q *MessageQueues) DrainSteer(agentSlug string) (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	msgs := q.steerQueues[agentSlug]
	if len(msgs) == 0 {
		return "", false
	}
	msg := msgs[0]
	q.steerQueues[agentSlug] = msgs[1:]
	return msg, true
}

// DrainFollowUp removes and returns the front follow-up message for the agent.
// Returns ("", false) if the queue is empty.
func (q *MessageQueues) DrainFollowUp(agentSlug string) (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	msgs := q.followUpQueues[agentSlug]
	if len(msgs) == 0 {
		return "", false
	}
	msg := msgs[0]
	q.followUpQueues[agentSlug] = msgs[1:]
	return msg, true
}

// HasSteer reports whether the agent has any steer messages queued.
func (q *MessageQueues) HasSteer(agentSlug string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steerQueues[agentSlug]) > 0
}

// HasFollowUp reports whether the agent has any follow-up messages queued.
func (q *MessageQueues) HasFollowUp(agentSlug string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.followUpQueues[agentSlug]) > 0
}

// HasMessages reports whether the agent has any queued messages (steer or follow-up).
func (q *MessageQueues) HasMessages(agentSlug string) bool {
	return q.HasSteer(agentSlug) || q.HasFollowUp(agentSlug)
}
