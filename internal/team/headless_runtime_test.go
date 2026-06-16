package team

import (
	"context"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// launcherWithActiveTask builds a Launcher whose broker has member `slug` with
// `binding` and one in_progress task it owns, so agentActiveTask(slug) resolves
// to `task`. Used to exercise per-task runtime override.
func launcherWithActiveTask(t *testing.T, slug string, binding provider.ProviderBinding, task teamTask) *Launcher {
	t.Helper()
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: slug, Name: slug, Provider: binding})
	b.memberIndex = nil
	task.Owner = slug
	task.status = "in_progress"
	b.tasks = append(b.tasks, task)
	b.mu.Unlock()
	l := minimalLauncher(false)
	l.broker = b
	return l
}

// TestPerTaskModelOverridesBinding: the model lives on the task, so a task's
// Model wins over the owner agent's binding — but only when the task's provider
// matches the runtime (no cross-kind leak).
func TestPerTaskModelOverridesBinding(t *testing.T) {
	// claude task model wins over a claude binding.
	l := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "claude-code", Model: "claude-sonnet-4-6"},
		teamTask{ID: "task-1", Title: "x", Provider: "claude-code", Model: "claude-opus-4-8"})
	if got := l.headlessClaudeModel(context.Background(), "eng"); got != "claude-opus-4-8" {
		t.Fatalf("headlessClaudeModel = %q, want task override claude-opus-4-8", got)
	}

	// codex task model wins for the codex runner.
	l2 := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "codex", Model: "gpt-5"},
		teamTask{ID: "task-2", Title: "y", Provider: "codex", Model: "gpt-5.5"})
	if got := l2.codexModelForAgent(context.Background(), "eng"); got != "gpt-5.5" {
		t.Fatalf("codexModelForAgent = %q, want task override gpt-5.5", got)
	}

	// Cross-kind isolation: a codex task's model must NOT leak into the claude
	// runner — it falls back to the claude binding.
	l3 := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "claude-code", Model: "claude-opus-4-7"},
		teamTask{ID: "task-3", Title: "z", Provider: "codex", Model: "gpt-5.5"})
	if got := l3.headlessClaudeModel(context.Background(), "eng"); got != "claude-opus-4-7" {
		t.Fatalf("headlessClaudeModel = %q, want binding claude-opus-4-7 (codex task model must not leak)", got)
	}
}

// TestTurnTaskResolvedFromCtx: when an agent owns more than one in_progress
// task at once (parallel instances), the runtime helpers must resolve the
// SPECIFIC task the turn is for — carried on ctx — not "the first in_progress
// task" that agentActiveTask returns. This is the core invariant the
// parallel-instances change depends on.
func TestTurnTaskResolvedFromCtx(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members, officeMember{
		Slug: "eng", Name: "eng",
		Provider: provider.ProviderBinding{Kind: "claude-code", Model: "claude-sonnet-4-6"},
	})
	b.memberIndex = nil
	// Two in_progress tasks owned by the same agent, each on its own model.
	b.tasks = append(b.tasks,
		teamTask{ID: "task-a", Title: "a", Owner: "eng", status: "in_progress", Provider: "claude-code", Model: "claude-opus-4-8", Effort: "high"},
		teamTask{ID: "task-b", Title: "b", Owner: "eng", status: "in_progress", Provider: "claude-code", Model: "claude-haiku-4-5", Effort: "low"},
	)
	b.mu.Unlock()
	l := minimalLauncher(false)
	l.broker = b

	ctxA := withHeadlessTurnTaskID(context.Background(), "task-a")
	ctxB := withHeadlessTurnTaskID(context.Background(), "task-b")

	if got := l.headlessClaudeModel(ctxA, "eng"); got != "claude-opus-4-8" {
		t.Errorf("turn task-a model = %q, want claude-opus-4-8", got)
	}
	if got := l.headlessClaudeModel(ctxB, "eng"); got != "claude-haiku-4-5" {
		t.Errorf("turn task-b model = %q, want claude-haiku-4-5", got)
	}
	if got := l.activeTaskEffort(ctxA, "eng"); got != "high" {
		t.Errorf("turn task-a effort = %q, want high", got)
	}
	if got := l.activeTaskEffort(ctxB, "eng"); got != "low" {
		t.Errorf("turn task-b effort = %q, want low", got)
	}
}

// TestEffectiveProviderKindPrefersTask: the dispatch switch routes by the
// active task's provider when set, falling back to the agent binding.
func TestEffectiveProviderKindPrefersTask(t *testing.T) {
	// Task provider wins over the agent binding kind.
	l := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "claude-code", Model: "claude-opus-4-8"},
		teamTask{ID: "task-1", Title: "x", Provider: "codex", Model: "gpt-5.5"})
	if got := l.effectiveProviderKindForAgent(context.Background(), "eng"); got != provider.KindCodex {
		t.Fatalf("effectiveProviderKindForAgent = %q, want codex (task override)", got)
	}

	// No per-task provider → empty here means "use the binding/global default";
	// taskModelForKind returns "" so the runners use the binding.
	l2 := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "claude-code", Model: "claude-opus-4-8"},
		teamTask{ID: "task-2", Title: "y"})
	if got := l2.taskModelForKind(context.Background(), "eng", provider.KindClaudeCode); got != "" {
		t.Fatalf("taskModelForKind with no task model = %q, want empty", got)
	}
}

// TestGatewayAgentResolvesToGatewayKind locks the predicate the durability-guard
// skip depends on. A foreign Slack/Openclaw agent has no local runtime: its turn
// is a no-op and its real work happens in Slack, unobservable in WUPHF's action
// log. The queue worker therefore skips headlessTurnCompletedDurably for gateway
// kinds. Without that skip, every foreign-agent no-op turn fails the
// external-evidence guard and mints a bogus "[@hermes] Repeated errors blocked"
// self-heal task on each (re)assignment. This guards the resolution that gates
// the skip: a Slack-bound owner must read as a gateway kind, a local coding agent
// must not.
func TestGatewayAgentResolvesToGatewayKind(t *testing.T) {
	lSlack := launcherWithActiveTask(t, "hermes",
		provider.ProviderBinding{Kind: provider.KindSlack},
		teamTask{ID: "OFFICE-9", Title: "Build HubSpot CRM card"})
	if got := lSlack.effectiveProviderKindForAgent(context.Background(), "hermes"); !provider.IsGatewayKind(got) {
		t.Fatalf("foreign Slack agent must resolve to a gateway kind so durability is skipped; got %q", got)
	}

	lLocal := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: provider.KindClaudeCode},
		teamTask{ID: "OFFICE-10", Title: "Build the feature"})
	if got := lLocal.effectiveProviderKindForAgent(context.Background(), "eng"); provider.IsGatewayKind(got) {
		t.Fatalf("local coding agent must NOT be a gateway kind (durability still applies); got %q", got)
	}
}

// TestPerTaskRuntimeWireRoundTrip: provider/model/effort survive a
// marshal/unmarshal cycle with their stable wire keys.
func TestPerTaskRuntimeWireRoundTrip(t *testing.T) {
	original := teamTask{
		ID:       "task-1",
		Title:    "demo",
		Provider: "codex",
		Model:    "gpt-5.5",
		Effort:   "high",
	}
	data, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	for _, want := range []string{`"provider":"codex"`, `"model":"gpt-5.5"`, `"effort":"high"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("marshalled task missing %s; got %s", want, string(data))
		}
	}
	var decoded teamTask
	if err := decoded.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if decoded.Provider != "codex" || decoded.Model != "gpt-5.5" || decoded.Effort != "high" {
		t.Errorf("round-trip = {provider:%q model:%q effort:%q}, want {codex gpt-5.5 high}",
			decoded.Provider, decoded.Model, decoded.Effort)
	}
}
