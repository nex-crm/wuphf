package team

// playbook_events.go declares the SSE event payloads and publisher
// interface used by the v1.3 playbook compiler + execution log. Kept in a
// dedicated file so the wiki_worker stays focused on the write-queue
// plumbing and tests can reuse these types without dragging the worker in.

// PlaybookExecutionRecordedEvent is the SSE payload broadcast when an
// execution-log entry lands. The UI subscribes by named event
// `playbook:execution_recorded` — follow the pattern already established
// by the entity + notebook event streams.
type PlaybookExecutionRecordedEvent struct {
	// Slug is the playbook slug (team/playbooks/{slug}.md).
	Slug string `json:"slug"`
	// Path is the jsonl log path that received the append. Callers use it
	// to avoid a second HTTP round-trip when they want to refetch.
	Path string `json:"path"`
	// CommitSHA is the short sha produced by CommitPlaybookExecution.
	CommitSHA string `json:"commit_sha"`
	// RecordedBy is the author slug the commit is attributed to.
	RecordedBy string `json:"recorded_by"`
	// Timestamp is the RFC3339 wall-clock time when the commit completed.
	Timestamp string `json:"timestamp"`
}

// playbookEventPublisher is the extension the WikiWorker needs in order to
// emit playbook-scoped SSE events. Broker satisfies this interface.
type playbookEventPublisher interface {
	PublishPlaybookExecutionRecorded(evt PlaybookExecutionRecordedEvent)
}
