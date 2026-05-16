package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedWorkspaceTree populates a fake .wuphf/ tree under runtimeHome with one
// representative file per category so categorized backup + restore can be
// exercised end-to-end. Returns the wuphfHome path.
func seedWorkspaceTree(t *testing.T, runtimeHome string) string {
	t.Helper()
	wuphfHome := filepath.Join(runtimeHome, ".wuphf")
	mustWrite := func(rel, body string) {
		t.Helper()
		full := filepath.Join(wuphfHome, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	// wiki tree, including a skill subtree that should also surface in skills/.
	mustWrite("wiki/team/index.md", "team root")
	mustWrite("wiki/team/skills/copyedit/SKILL.md", "copyedit skill body")
	mustWrite("wiki/team/skills/copyedit/templates/intro.md", "intro template")
	// chats live under sessions/
	mustWrite("sessions/agent-a/2026-05-13-conversation.jsonl", "{\"role\":\"user\"}\n")
	// context fan-out: a leaf in team/, root-level files, and a nested dir.
	mustWrite("team/broker-state.json", "{\"version\":1}")
	mustWrite("team/broker-state.json.last-good", "{\"version\":1,\"snapshot\":true}")
	mustWrite("onboarded.json", "{\"done\":true}")
	mustWrite("company.json", "{\"name\":\"Acme\"}")
	mustWrite("calendar.json", "[]")
	mustWrite("office/tasks/task-1/receipt.json", "{}")
	mustWrite("workflows/onboard.yaml", "steps: []")
	mustWrite("logs/channel-pane.log", "log line")
	mustWrite("providers/claude-sessions.json", "{}")
	mustWrite("codex-headless/cache.bin", "binary")
	mustWrite("wiki.bak/lightweight.md", "wiki backup")
	return wuphfHome
}

func TestShredCreatesCategorizedBackup(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()

	runtimeHome := filepath.Join(sd, "ws-cat")
	wuphfHome := seedWorkspaceTree(t, runtimeHome)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: filepath.Join(sd, "main"),
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "ws-cat", RuntimeHome: runtimeHome,
				BrokerPort: 7920, WebPort: 7921,
				State: StatePaused, CreatedAt: now, LastUsedAt: now,
				Blueprint: "founding-team", CompanyName: "Acme"},
		},
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	if err := Shred(context.Background(), "ws-cat", false); err != nil {
		t.Fatalf("Shred: %v", err)
	}

	// Runtime tree must be gone — Shred deletes it after backup.
	if _, err := os.Stat(wuphfHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wuphfHome should be removed after shred; stat err=%v", err)
	}
	if _, err := os.Stat(runtimeHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtimeHome should be removed after shred; stat err=%v", err)
	}

	// Exactly one categorized backup entry should exist.
	backupRoot := filepath.Join(sd, backupsDirName)
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 backup entry, got %d", len(entries))
	}
	backupDir := filepath.Join(backupRoot, entries[0].Name())

	// Categorized subfolders + manifest must be present with the right content.
	expectFile := func(rel, wantBody string) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(backupDir, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != wantBody {
			t.Fatalf("%s body: want %q, got %q", rel, wantBody, string(got))
		}
	}
	expectFile("wiki/team/index.md", "team root")
	expectFile("wiki/team/skills/copyedit/SKILL.md", "copyedit skill body")
	// skills/ is the duplicate view of wiki/team/skills/ — files preserved.
	expectFile("skills/copyedit/SKILL.md", "copyedit skill body")
	expectFile("skills/copyedit/templates/intro.md", "intro template")
	// chats/ corresponds to sessions/.
	expectFile("chats/agent-a/2026-05-13-conversation.jsonl", "{\"role\":\"user\"}\n")
	// context/ preserves wuphfHome-relative layout for everything else.
	expectFile("context/team/broker-state.json", "{\"version\":1}")
	expectFile("context/team/broker-state.json.last-good", "{\"version\":1,\"snapshot\":true}")
	expectFile("context/onboarded.json", "{\"done\":true}")
	expectFile("context/company.json", "{\"name\":\"Acme\"}")
	expectFile("context/calendar.json", "[]")
	expectFile("context/office/tasks/task-1/receipt.json", "{}")
	expectFile("context/workflows/onboard.yaml", "steps: []")
	expectFile("context/logs/channel-pane.log", "log line")
	expectFile("context/providers/claude-sessions.json", "{}")
	expectFile("context/codex-headless/cache.bin", "binary")
	expectFile("context/wiki.bak/lightweight.md", "wiki backup")

	// Manifest must round-trip the workspace identity.
	manifestBytes, err := os.ReadFile(filepath.Join(backupDir, backupManifestFile))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m backupManifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m.OriginalName != "ws-cat" {
		t.Errorf("manifest original_name: want ws-cat, got %q", m.OriginalName)
	}
	if m.OriginalRuntimeHome != runtimeHome {
		t.Errorf("manifest original_runtime_home: want %q, got %q", runtimeHome, m.OriginalRuntimeHome)
	}
	if m.Blueprint != "founding-team" {
		t.Errorf("manifest blueprint: want founding-team, got %q", m.Blueprint)
	}
	if m.CompanyName != "Acme" {
		t.Errorf("manifest company_name: want Acme, got %q", m.CompanyName)
	}
	if m.BrokerPort != 7920 || m.WebPort != 7921 {
		t.Errorf("manifest ports: want 7920/7921, got %d/%d", m.BrokerPort, m.WebPort)
	}
}

func TestShredPermanentSkipsBackup(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()

	runtimeHome := filepath.Join(sd, "perm-noback")
	seedWorkspaceTree(t, runtimeHome)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: filepath.Join(sd, "main"),
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "perm-noback", RuntimeHome: runtimeHome,
				BrokerPort: 7922, WebPort: 7923,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Shred(context.Background(), "perm-noback", true); err != nil {
		t.Fatalf("Shred permanent: %v", err)
	}

	if _, err := os.Stat(runtimeHome); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("runtime home should be deleted in permanent shred")
	}

	// No backup entry should exist for this workspace.
	backupRoot := filepath.Join(sd, backupsDirName)
	if entries, _ := os.ReadDir(backupRoot); len(entries) != 0 {
		t.Errorf("permanent shred should not create a backup, found %d entries", len(entries))
	}
}

func TestRestoreFromCategorizedBackup(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()

	// Shred a workspace first to produce a categorized backup.
	runtimeHome := filepath.Join(sd, "rt-restore")
	seedWorkspaceTree(t, runtimeHome)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: filepath.Join(sd, "main"),
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "rt-restore", RuntimeHome: runtimeHome,
				BrokerPort: 7924, WebPort: 7925,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Shred(context.Background(), "rt-restore", false); err != nil {
		t.Fatalf("Shred: %v", err)
	}

	backupRoot := filepath.Join(sd, backupsDirName)
	entries, err := os.ReadDir(backupRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup setup: entries=%d err=%v", len(entries), err)
	}
	backupID := entries[0].Name()

	if err := Restore(context.Background(), backupID); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// The backup directory should be cleaned up on success.
	if _, err := os.Stat(filepath.Join(backupRoot, backupID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup dir should be removed after restore; stat err=%v", err)
	}

	// Reconstructed wuphfHome should have the original layout back in place.
	restoredWuphf := filepath.Join(sd, "rt-restore", ".wuphf")
	expect := func(rel, wantBody string) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(restoredWuphf, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != wantBody {
			t.Fatalf("%s: want %q, got %q", rel, wantBody, string(got))
		}
	}
	expect("wiki/team/index.md", "team root")
	expect("wiki/team/skills/copyedit/SKILL.md", "copyedit skill body")
	expect("sessions/agent-a/2026-05-13-conversation.jsonl", "{\"role\":\"user\"}\n")
	expect("team/broker-state.json", "{\"version\":1}")
	expect("onboarded.json", "{\"done\":true}")
	expect("office/tasks/task-1/receipt.json", "{}")

	// And the restored workspace must be registered with state never_started.
	reg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var restored *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == "rt-restore" {
			restored = ws
			break
		}
	}
	if restored == nil {
		t.Fatal("restored workspace not in registry")
	}
	if restored.State != StateNeverStarted {
		t.Errorf("restored state: want never_started, got %s", restored.State)
	}
}

// TestShredRollbackOnPartialBackupFailure verifies the data-loss safeguard:
// if a rename inside writeCategorizedBackup fails after earlier renames have
// already succeeded, the function rolls every previous move back to its
// source so the runtime tree stays intact for a retry. We force the failure
// by pre-creating a non-empty destination inside the backup root before
// shred starts — os.Rename on Linux refuses to overwrite a non-empty target,
// so the second rename attempt fails deterministically.
func TestShredRollbackOnPartialBackupFailure(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()

	runtimeHome := filepath.Join(sd, "ws-rollback")
	wuphfHome := seedWorkspaceTree(t, runtimeHome)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "ws-rollback", RuntimeHome: runtimeHome,
				BrokerPort: 7940, WebPort: 7941,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Plant a non-empty directory at the exact path writeCategorizedBackup
	// will try to rename "wiki" into. The backup writer creates its backup
	// root with a unix-second-based ID, so we plant entries for the unix
	// second the test runs at and one second on either side to absorb timing
	// drift. os.Rename refuses to overwrite a non-empty target, forcing the
	// wiki move to fail.
	backupsRoot := filepath.Join(sd, backupsDirName)
	for delta := -1; delta <= 1; delta++ {
		blocker := filepath.Join(backupsRoot,
			fmt.Sprintf("ws-rollback-%d", time.Now().Unix()+int64(delta)),
			"wiki", "sentinel")
		if err := os.MkdirAll(blocker, 0o700); err != nil {
			t.Fatalf("plant blocker: %v", err)
		}
	}

	err := Shred(context.Background(), "ws-rollback", false)
	if err == nil {
		t.Fatal("expected Shred to fail when backup move collides")
	}

	// Runtime tree must be intact: the wiki, sessions, and team/ entries
	// should all still be at their original locations because rollback
	// reversed every successful move before returning.
	for _, rel := range []string{
		"wiki/team/index.md",
		"wiki/team/skills/copyedit/SKILL.md",
		"sessions/agent-a/2026-05-13-conversation.jsonl",
		"team/broker-state.json",
		"onboarded.json",
		"office/tasks/task-1/receipt.json",
	} {
		if _, err := os.Stat(filepath.Join(wuphfHome, rel)); err != nil {
			t.Errorf("source %s should be restored after rollback, got: %v", rel, err)
		}
	}

	// Workspace must still be registered — Shred only removes from registry
	// on success.
	reg, _ := Read()
	stillRegistered := false
	for _, ws := range reg.Workspaces {
		if ws.Name == "ws-rollback" {
			stillRegistered = true
			break
		}
	}
	if !stillRegistered {
		t.Error("ws-rollback should remain in registry after failed shred")
	}
}

func TestTrashListSurfaceManifestFields(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()

	runtimeHome := filepath.Join(sd, "ws-list")
	seedWorkspaceTree(t, runtimeHome)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "ws-list", RuntimeHome: runtimeHome,
				BrokerPort: 7930, WebPort: 7931,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Shred(context.Background(), "ws-list", false); err != nil {
		t.Fatalf("Shred: %v", err)
	}

	entries, err := Trash(context.Background())
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 trash entry, got %d", len(entries))
	}
	got := entries[0]
	if got.Name != "ws-list" {
		t.Errorf("Name: want ws-list, got %q", got.Name)
	}
	if got.OriginalRuntimeHome != runtimeHome {
		t.Errorf("OriginalRuntimeHome: want %q, got %q", runtimeHome, got.OriginalRuntimeHome)
	}
	if got.ShredAt.IsZero() {
		t.Errorf("ShredAt should be populated, got zero")
	}
}
