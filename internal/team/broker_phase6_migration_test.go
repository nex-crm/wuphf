package team

// broker_phase6_migration_test.go — production-fixture test for the Phase 6
// persisted-state migration. It hand-writes a PRE-CHANGE legacy
// broker-state.json (the shape a real user on `~/.wuphf` would have before the
// "Tasks as the primary primitive" restructure), loads it through the real
// constructor + migration entry points, and asserts the workspace comes up
// clean with no data loss:
//
//   - the built-in Librarian (Pam) is added to the existing roster;
//   - legacy lifecycle_state values are remapped (merged -> approved);
//   - in-flight legacy tasks survive with a sensible (non-archived) state;
//   - every orphaned legacy channel AND DM is folded into an archived owning
//     task so its history stays reachable (the user invariant: every chat
//     channel is a task channel);
//   - #general stays owned by the Backup & Migration system task;
//   - messages + incidents (agent_issues wire key, issue-N IDs) are preserved;
//   - additive task keys default cleanly (no coerced "auto" owner);
//   - the migration is idempotent and the result round-trips through save+reload.

import (
	"os"
	"path/filepath"
	"testing"
)

// legacyBrokerStateFixture is a pre-structural-changes broker-state.json:
// a roster WITHOUT the Librarian, a free-standing #product channel, a
// message-only DM slug (dwight__human, no channels[] record — the legacy DM
// shape), tasks with a legacy lifecycle_state and none of the new
// provider/model/effort keys, a removed-plan-mode task (lifecycle_state
// "planning" + plan_first key) that must load as Running with the key
// ignored, and an incident persisted under the historical `agent_issues`
// wire key with an `issue-N` ID.
const legacyBrokerStateFixture = `{
  "messages": [
    {"id":"m1","from":"human","channel":"general","content":"morning all","timestamp":"2026-01-02T10:00:00Z","tagged":[]},
    {"id":"m2","from":"angela","channel":"product","content":"roadmap draft is up","timestamp":"2026-01-03T10:00:00Z","tagged":[]},
    {"id":"m3","from":"human","channel":"product","content":"looks good","timestamp":"2026-01-03T11:00:00Z","tagged":[]},
    {"id":"m4","from":"human","channel":"dwight__human","content":"quick question","timestamp":"2026-01-04T09:00:00Z","tagged":[]},
    {"id":"m5","from":"dwight","channel":"dwight__human","content":"on it","timestamp":"2026-01-04T09:05:00Z","tagged":[]}
  ],
  "agent_issues": [
    {"id":"issue-1","agent":"dwight","channel":"general","detail":"tool timeout","normalized_key":"tool-timeout","count":2,"created_at":"2026-01-05T10:00:00Z","updated_at":"2026-01-05T10:30:00Z"}
  ],
  "members": [
    {"slug":"ceo","name":"CEO","role":"Chief Executive"},
    {"slug":"dwight","name":"Dwight","role":"Sales"},
    {"slug":"angela","name":"Angela","role":"Accounting"}
  ],
  "channels": [
    {"slug":"general","name":"general","members":["ceo","dwight","angela"]},
    {"slug":"product","name":"product","members":["ceo","angela"]}
  ],
  "tasks": [
    {"id":"task-1","channel":"general","title":"shipped before the rename","status":"done","lifecycle_state":"merged","owner":"dwight"},
    {"id":"task-2","channel":"general","title":"still in flight","status":"in_progress","owner":"angela"},
    {"id":"task-3","channel":"general","title":"was mid-plan when plan mode was removed","status":"in_progress","pipeline_stage":"plan","review_state":"pending_review","lifecycle_state":"planning","plan_first":true,"owner":"dwight"}
  ],
  "counter": 3
}`

// writeLegacyFixture writes the fixture to a temp broker-state.json and returns
// the path. NewBrokerAt(path) loads it at construction.
func writeLegacyFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "broker-state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(legacyBrokerStateFixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// withDiskLoad flips off the package test gate that normally suppresses
// NewBrokerAt's auto-load of disk state (set in test_support.go), so the
// constructor runs the real production boot sequence — loadState +
// ensureDefaultOfficeMembersLocked + ensureDefaultChannelsLocked (which seeds
// the Backup & Migration task) + normalizeLoadedStateLocked. Restored on test
// cleanup. Mirrors broker_state_path_test.go. The team test suite runs serially
// (no t.Parallel here), so toggling the global is safe.
func withDiskLoad(t *testing.T) {
	t.Helper()
	old := skipBrokerStateLoadOnConstruct
	skipBrokerStateLoadOnConstruct = false
	t.Cleanup(func() { skipBrokerStateLoadOnConstruct = old })
}

// runStartupMigrations mirrors the order Start() runs the once-migrations,
// without booting the full broker (wiki worker / HTTP / company seed).
func runStartupMigrations(b *Broker) {
	b.MigrateLifecycleStatesOnce()
	b.MigrateLegacyChannelsOnce()
}

// bootLegacyBroker writes the legacy fixture, boots a broker against it exactly
// as production would (auto-load on), runs the startup migrations, and returns
// the broker + its state path.
func bootLegacyBroker(t *testing.T) (*Broker, string) {
	t.Helper()
	withDiskLoad(t)
	path := writeLegacyFixture(t)
	b := NewBrokerAt(path)
	runStartupMigrations(b)
	return b, path
}

func memberBySlug(t *testing.T, b *Broker, slug string) (officeMember, bool) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.members {
		if m.Slug == slug {
			return m, true
		}
	}
	return officeMember{}, false
}

func taskByID(t *testing.T, b *Broker, id string) (teamTask, bool) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, tk := range b.tasks {
		if tk.ID == id {
			return tk, true
		}
	}
	return teamTask{}, false
}

// archivedOwnerOf returns the System+Archived task that owns channel slug, if any.
func archivedOwnerOf(t *testing.T, b *Broker, slug string) (teamTask, bool) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, tk := range b.tasks {
		if tk.Channel == slug && tk.System && tk.LifecycleState == LifecycleStateArchived {
			return tk, true
		}
	}
	return teamTask{}, false
}

func channelMessageCount(t *testing.T, b *Broker, slug string) int {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, m := range b.messages {
		if m.Channel == slug {
			n++
		}
	}
	return n
}

func TestPhase6MigrationLoadsLegacyWorkspaceClean(t *testing.T) {
	b, _ := bootLegacyBroker(t)

	// --- Librarian (Pam) added to the existing roster, built-in. ---
	pam, ok := memberBySlug(t, b, LibrarianSlug)
	if !ok {
		t.Fatalf("Librarian (%q) was not added to the legacy roster", LibrarianSlug)
	}
	if !pam.BuiltIn {
		t.Fatalf("Librarian should be built-in after migration, got BuiltIn=false")
	}
	if pam.Name != librarianName {
		t.Fatalf("Librarian name = %q, want %q", pam.Name, librarianName)
	}

	// --- App Builder back-filled onto the existing roster, built-in. The
	// roster was persisted before the Apps feature, so this is the same
	// load-time migration as the Librarian: the agent must appear in the
	// office (and therefore the sidebar) without re-onboarding. ---
	ab, ok := memberBySlug(t, b, appBuilderSlug)
	if !ok {
		t.Fatalf("App Builder (%q) was not added to the legacy roster", appBuilderSlug)
	}
	if !ab.BuiltIn {
		t.Fatalf("App Builder should be built-in after migration, got BuiltIn=false")
	}
	if ab.Name != appBuilderRole {
		t.Fatalf("App Builder name = %q, want %q", ab.Name, appBuilderRole)
	}
	// The pre-existing specialists must still be present (no roster clobber).
	for _, slug := range []string{"ceo", "dwight", "angela"} {
		if _, found := memberBySlug(t, b, slug); !found {
			t.Fatalf("legacy member %q was lost during migration", slug)
		}
	}

	// --- Legacy lifecycle_state remapped, in-flight task survives. ---
	t1, ok := taskByID(t, b, "task-1")
	if !ok {
		t.Fatalf("legacy task-1 was lost")
	}
	if t1.LifecycleState != LifecycleStateApproved {
		t.Fatalf("legacy lifecycle_state \"merged\" should remap to Approved, got %q", t1.LifecycleState)
	}
	t2, ok := taskByID(t, b, "task-2")
	if !ok {
		t.Fatalf("in-flight task-2 was lost")
	}
	// A legacy in-flight task (status=in_progress, no lifecycle_state) must
	// resume as Running — not land in the Unknown bucket — even though
	// normalizeTaskPlan stamped a current-scheme pipeline_stage the strict
	// migration map predates.
	if t2.LifecycleState != LifecycleStateRunning {
		t.Fatalf("in-flight task-2 should migrate to Running, got %q", t2.LifecycleState)
	}

	// --- Additive keys default cleanly; no coerced "auto" owner. ---
	if t2.Provider != "" || t2.Model != "" || t2.Effort != "" {
		t.Fatalf("legacy task-2 should have empty provider/model/effort, got %q/%q/%q",
			t2.Provider, t2.Model, t2.Effort)
	}
	if isAutoOwner(t2.Owner) {
		t.Fatalf("legacy task-2 owner must stay %q, not be coerced to auto", "angela")
	}

	// --- Removed plan mode (core-loop R3): persisted "planning" loads as
	// Running and the legacy plan_first key is ignored without error. ---
	t3, ok := taskByID(t, b, "task-3")
	if !ok {
		t.Fatalf("legacy planning task-3 was lost")
	}
	if t3.LifecycleState != LifecycleStateRunning {
		t.Fatalf("legacy planning task-3 should load as Running, got %q", t3.LifecycleState)
	}

	// --- #general stays owned by the Backup & Migration system task. ---
	bm, ok := taskByID(t, b, backupMigrationTaskID)
	if !ok {
		t.Fatalf("Backup & Migration task (%q) missing", backupMigrationTaskID)
	}
	if bm.Channel != "general" || !bm.System {
		t.Fatalf("Backup & Migration task should own #general as a system task, got channel=%q system=%v", bm.Channel, bm.System)
	}
	// #general must NOT be double-folded into a second archived task.
	if _, dup := archivedOwnerOf(t, b, "general"); dup {
		// archivedOwnerOf would also match the Backup & Migration task, so
		// instead assert exactly one task owns #general besides the work tasks.
		count := 0
		b.mu.Lock()
		for _, tk := range b.tasks {
			if tk.Channel == "general" && tk.System {
				count++
			}
		}
		b.mu.Unlock()
		if count != 1 {
			t.Fatalf("#general should be owned by exactly one system task, got %d", count)
		}
	}

	// --- Orphaned #product folded into an archived owning task. ---
	prod, ok := archivedOwnerOf(t, b, "product")
	if !ok {
		t.Fatalf("legacy #product channel was not folded into an archived task")
	}
	if prod.ID == "" || prod.Title == "" {
		t.Fatalf("folded #product task missing ID/Title: %+v", prod)
	}

	// --- DM (message-only slug) folded into an archived owning task. ---
	dm, ok := archivedOwnerOf(t, b, "dwight__human")
	if !ok {
		t.Fatalf("legacy DM dwight__human was not folded into an archived task")
	}
	if dm.Title == "" {
		t.Fatalf("folded DM task missing Title")
	}

	// --- Messages preserved per channel (no data loss, no rewrites). ---
	if got := channelMessageCount(t, b, "general"); got != 1 {
		t.Fatalf("#general message count = %d, want 1", got)
	}
	if got := channelMessageCount(t, b, "product"); got != 2 {
		t.Fatalf("#product message count = %d, want 2", got)
	}
	if got := channelMessageCount(t, b, "dwight__human"); got != 2 {
		t.Fatalf("DM message count = %d, want 2", got)
	}

	// --- Incident loaded from agent_issues with issue-N ID intact. ---
	b.mu.Lock()
	incCount := len(b.incidents)
	var incID string
	if incCount > 0 {
		incID = b.incidents[0].ID
	}
	b.mu.Unlock()
	if incCount != 1 {
		t.Fatalf("expected 1 incident from agent_issues, got %d", incCount)
	}
	if incID != "issue-1" {
		t.Fatalf("legacy incident ID should be preserved as %q, got %q", "issue-1", incID)
	}

	// --- Folded archived tasks surface via ListTasks(IncludeDone). ---
	resp, err := b.ListTasks(TaskListRequest{IncludeDone: true, AllChannels: true})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	surfaced := map[string]bool{}
	for _, tk := range resp.Tasks {
		surfaced[tk.Channel] = surfaced[tk.Channel] || (tk.System && tk.LifecycleState == LifecycleStateArchived)
	}
	for _, slug := range []string{"general", "product", "dwight__human"} {
		if !surfaced[slug] {
			t.Fatalf("archived owner of %q not surfaced by ListTasks(IncludeDone)", slug)
		}
	}
}

func TestPhase6ChannelFoldIsIdempotent(t *testing.T) {
	withDiskLoad(t)
	path := writeLegacyFixture(t)
	b := NewBrokerAt(path)
	b.MigrateLifecycleStatesOnce()

	// Run the locked fold directly twice (bypassing the per-broker sync.Once)
	// and confirm the second pass mints nothing new.
	b.mu.Lock()
	b.migrateLegacyChannelsIntoArchivedTasksLocked()
	firstCount := len(b.tasks)
	b.migrateLegacyChannelsIntoArchivedTasksLocked()
	secondCount := len(b.tasks)
	b.mu.Unlock()

	if firstCount != secondCount {
		t.Fatalf("channel fold is not idempotent: task count went %d -> %d on re-run", firstCount, secondCount)
	}
}

func TestPhase6MigrationRoundTripsThroughSave(t *testing.T) {
	b, path := bootLegacyBroker(t)

	// Persist the migrated state.
	b.mu.Lock()
	saveErr := b.saveLocked()
	b.mu.Unlock()
	if saveErr != nil {
		t.Fatalf("saveLocked: %v", saveErr)
	}

	// Reload from the same path: the Librarian and the folded archive tasks
	// must persist (they were written), and the second boot's migration must
	// not duplicate them.
	b2 := NewBrokerAt(path)
	runStartupMigrations(b2)

	if _, ok := memberBySlug(t, b2, LibrarianSlug); !ok {
		t.Fatalf("Librarian missing after save+reload")
	}
	for _, slug := range []string{"product", "dwight__human"} {
		if _, ok := archivedOwnerOf(t, b2, slug); !ok {
			t.Fatalf("archived owner of %q missing after save+reload", slug)
		}
	}
	// No duplicate archive tasks across the reload.
	b2.mu.Lock()
	owners := map[string]int{}
	for _, tk := range b2.tasks {
		if tk.System && tk.LifecycleState == LifecycleStateArchived {
			owners[tk.Channel]++
		}
	}
	b2.mu.Unlock()
	for slug, n := range owners {
		if n != 1 {
			t.Fatalf("channel %q owned by %d archived tasks after reload, want 1", slug, n)
		}
	}
}
