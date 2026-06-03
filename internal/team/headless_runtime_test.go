package team

import (
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
	if got := l.headlessClaudeModel("eng"); got != "claude-opus-4-8" {
		t.Fatalf("headlessClaudeModel = %q, want task override claude-opus-4-8", got)
	}

	// codex task model wins for the codex runner.
	l2 := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "codex", Model: "gpt-5"},
		teamTask{ID: "task-2", Title: "y", Provider: "codex", Model: "gpt-5.5"})
	if got := l2.codexModelForAgent("eng"); got != "gpt-5.5" {
		t.Fatalf("codexModelForAgent = %q, want task override gpt-5.5", got)
	}

	// Cross-kind isolation: a codex task's model must NOT leak into the claude
	// runner — it falls back to the claude binding.
	l3 := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "claude-code", Model: "claude-opus-4-7"},
		teamTask{ID: "task-3", Title: "z", Provider: "codex", Model: "gpt-5.5"})
	if got := l3.headlessClaudeModel("eng"); got != "claude-opus-4-7" {
		t.Fatalf("headlessClaudeModel = %q, want binding claude-opus-4-7 (codex task model must not leak)", got)
	}
}

// TestEffectiveProviderKindPrefersTask: the dispatch switch routes by the
// active task's provider when set, falling back to the agent binding.
func TestEffectiveProviderKindPrefersTask(t *testing.T) {
	// Task provider wins over the agent binding kind.
	l := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "claude-code", Model: "claude-opus-4-8"},
		teamTask{ID: "task-1", Title: "x", Provider: "codex", Model: "gpt-5.5"})
	if got := l.effectiveProviderKindForAgent("eng"); got != provider.KindCodex {
		t.Fatalf("effectiveProviderKindForAgent = %q, want codex (task override)", got)
	}

	// No per-task provider → empty here means "use the binding/global default";
	// taskModelForKind returns "" so the runners use the binding.
	l2 := launcherWithActiveTask(t, "eng",
		provider.ProviderBinding{Kind: "claude-code", Model: "claude-opus-4-8"},
		teamTask{ID: "task-2", Title: "y"})
	if got := l2.taskModelForKind("eng", provider.KindClaudeCode); got != "" {
		t.Fatalf("taskModelForKind with no task model = %q, want empty", got)
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
