package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestAppEditSessionLazilyMintsChannel locks in the modify-wedge fix: an app
// with no edit thread (the state every app registered via POST /apps starts in,
// and the state every app minted before edit-channel stamping is stuck in) must
// become editable on demand. Opening an edit session mints a task-<id> channel,
// binds it to the app, and is idempotent. This is the regression for "the Edit
// option isn't there" — before the endpoint existed the FE had no channel to
// bind and hid Edit forever.
func TestAppEditSessionLazilyMintsChannel(t *testing.T) {
	// Isolate the app store under a temp runtime home (appStore() reads
	// CustomAppsRootDir lazily, so set it before the first /apps call).
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	// Register an app the way a legacy app exists: published, but with NO edit
	// channel — register_app never stamps one, only a task create does.
	body, _ := json.Marshal(map[string]any{"name": "Legacy Tool", "html": validAppHTML})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, body)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)
	if id == "" {
		t.Fatalf("no app id in register response: %v", created)
	}
	if ch, _ := app["editChannel"].(string); ch != "" {
		t.Fatalf("a fresh register should carry no edit channel, got %q", ch)
	}

	// Open an edit session → mints a task-<id> channel and returns it.
	session := postAppsAsAgent(t, base+"/apps/"+id+"/edit-session", b.Token(), appBuilderSlug, []byte("{}"))
	ch1, _ := session["channel"].(string)
	if ch1 == "" {
		t.Fatalf("edit-session returned no channel: %v", session)
	}
	if !strings.HasPrefix(ch1, "task-") {
		t.Fatalf("edit channel = %q, want a task-<id> slug", ch1)
	}

	// The app's manifest now carries that channel, so the FE can bind Edit.
	_, cur := getAppsJSON(t, base+"/apps/"+id, b.Token())
	curApp, _ := cur["app"].(map[string]any)
	if got, _ := curApp["editChannel"].(string); got != ch1 {
		t.Fatalf("app editChannel = %q, want %q", got, ch1)
	}

	// A real App Builder task backs that channel, so a human post there wakes
	// the agent through the task_followup path (the channel is not a dangling
	// slug pointing at nothing).
	b.mu.Lock()
	var backing *teamTask
	for i := range b.tasks {
		if b.tasks[i].Channel == ch1 && b.tasks[i].Owner == appBuilderSlug {
			backing = &b.tasks[i]
			break
		}
	}
	b.mu.Unlock()
	if backing == nil {
		t.Fatalf("no app-builder task backs edit channel %q", ch1)
	}
	if backing.Title != "Edit app: Legacy Tool" {
		t.Fatalf("backing task title = %q, want %q", backing.Title, "Edit app: Legacy Tool")
	}

	// Idempotent: a second open returns the SAME channel and spawns no new task.
	session2 := postAppsAsAgent(t, base+"/apps/"+id+"/edit-session", b.Token(), appBuilderSlug, []byte("{}"))
	if ch2, _ := session2["channel"].(string); ch2 != ch1 {
		t.Fatalf("second edit-session returned %q, want the same channel %q", ch2, ch1)
	}
	b.mu.Lock()
	count := 0
	for i := range b.tasks {
		if b.tasks[i].Channel == ch1 && b.tasks[i].Owner == appBuilderSlug {
			count++
		}
	}
	b.mu.Unlock()
	if count != 1 {
		t.Fatalf("idempotent open spawned %d backing tasks, want 1", count)
	}
}

// TestAppEditSessionRestrictedToWriters locks the gate: a random agent holding
// the broker token must not open an edit session (it spawns an App Builder
// task); only the App Builder or a human session may.
func TestAppEditSessionRestrictedToWriters(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{"name": "Gated Tool", "html": validAppHTML})
	created := postAppsAsAgent(t, base+"/apps", b.Token(), appBuilderSlug, body)
	app, _ := created["app"].(map[string]any)
	id, _ := app["id"].(string)

	if code := postAppsStatus(t, base+"/apps/"+id+"/edit-session", b.Token(), "ceo", []byte("{}")); code != http.StatusForbidden {
		t.Fatalf("non-writer edit-session: got %d, want 403", code)
	}
}
