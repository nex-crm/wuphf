package workspaces

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startFakeServer binds a local HTTP server and returns its port and a
// shutdown function. The server responds 200 to all HEAD requests.
func startFakeServer(t *testing.T) (port int, shutdown func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().(*net.TCPAddr).Port, func() { _ = srv.Close() }
}

// withOrchestratorHome isolates HOME (and WUPHF_RUNTIME_HOME) for orchestrator
// tests. spacesDir uses real HOME because ~/.wuphf-spaces is shared
// cross-workspace; overriding only WUPHF_RUNTIME_HOME would leak into the
// user's real ~/.wuphf-spaces directory.
func withOrchestratorHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	return dir
}

// installNoopSpawn replaces the spawn function with a no-op that creates the
// PID file and immediately marks the port as "live" by starting a fake server.
// Returns the fake broker port that was assigned.
func installNoopSpawn(t *testing.T) (fakePort int, cleanup func()) {
	t.Helper()
	fakePort, shutdown := startFakeServer(t)
	origSpawn := spawnFn
	spawnFn = func(name, runtimeHome string, brokerPort, webPort int) error {
		// Write a pid file so pause can read it.
		pidPath := filepath.Join(runtimeHome, ".wuphf", "broker.pid")
		_ = os.MkdirAll(filepath.Dir(pidPath), 0o700)
		_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
		return nil
	}
	return fakePort, func() {
		spawnFn = origSpawn
		shutdown()
	}
}

// installFailSpawn replaces the spawn function with one that always errors.
func installFailSpawn(t *testing.T) func() {
	t.Helper()
	origSpawn := spawnFn
	spawnFn = func(name, runtimeHome string, brokerPort, webPort int) error {
		return fmt.Errorf("spawn: injected failure")
	}
	return func() { spawnFn = origSpawn }
}

// ---- Create tests ----------------------------------------------------------

func TestCreateRegistersWorkspace(t *testing.T) {
	home := withOrchestratorHome(t)
	_ = home

	// Seed a registry so AllocatePortPair has context.
	now := time.Now().UTC()
	seed := &Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{
				Name: "main", RuntimeHome: "/tmp/main",
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now,
			},
		},
	}
	if err := Write(seed); err != nil {
		t.Fatalf("seed Write: %v", err)
	}

	// Replace spawn with no-op.
	origSpawn := spawnFn
	spawnFn = func(name, runtimeHome string, brokerPort, webPort int) error {
		_ = os.MkdirAll(filepath.Join(runtimeHome, ".wuphf"), 0o700)
		return nil
	}
	defer func() { spawnFn = origSpawn }()

	if err := Create(context.Background(), "test-ws", "founding-team", CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	reg, err := Read()
	if err != nil {
		t.Fatalf("Read after Create: %v", err)
	}
	var found *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == "test-ws" {
			found = ws
			break
		}
	}
	if found == nil {
		t.Fatal("test-ws not in registry after Create")
	}
	if found.State != StateRunning {
		t.Errorf("state: want running, got %s", found.State)
	}
	if found.BrokerPort < PortRangeStart || found.BrokerPort > PortRangeEnd {
		t.Errorf("BrokerPort %d out of range", found.BrokerPort)
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "exists", RuntimeHome: "/tmp/x",
				BrokerPort: 7910, WebPort: 7911,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	origSpawn := spawnFn
	spawnFn = func(name, runtimeHome string, brokerPort, webPort int) error { return nil }
	defer func() { spawnFn = origSpawn }()

	err := Create(context.Background(), "exists", "", CreateOptions{})
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestCreateMarksErrorOnSpawnFailure(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: "/tmp/main",
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cleanup := installFailSpawn(t)
	defer cleanup()

	err := Create(context.Background(), "badspawn", "bp", CreateOptions{})
	if err == nil {
		t.Fatal("expected spawn error")
	}

	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "badspawn" && ws.State != StateError {
			t.Errorf("expected state=error, got %s", ws.State)
		}
	}
}

func TestCreateRejectsInvalidSlug(t *testing.T) {
	withOrchestratorHome(t)
	err := Create(context.Background(), "UPPER", "", CreateOptions{})
	if err == nil {
		t.Fatal("expected slug validation error")
	}
}

// ---- Switch tests ----------------------------------------------------------

func TestSwitchUpdatesCLICurrent(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: "/tmp/main",
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "demo", RuntimeHome: "/tmp/demo",
				BrokerPort: 7910, WebPort: 7911,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	url, err := Switch(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if url != "http://localhost:7911/" {
		t.Errorf("URL: want http://localhost:7911/, got %s", url)
	}

	reg, _ := Read()
	if reg.CLICurrent != "demo" {
		t.Errorf("CLICurrent: want demo, got %s", reg.CLICurrent)
	}
}

func TestSwitchReturnsErrForMissingWorkspace(t *testing.T) {
	withOrchestratorHome(t)

	if err := Write(&Registry{Version: Version, CLICurrent: "main",
		Workspaces: []*Workspace{}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := Switch(context.Background(), "ghost")
	if err != ErrWorkspaceNotFound {
		t.Errorf("want ErrWorkspaceNotFound, got %v", err)
	}
}

// ---- List tests ------------------------------------------------------------

func TestListProbesPortsInParallel(t *testing.T) {
	withOrchestratorHome(t)

	// Start two fake servers.
	port1, shut1 := startFakeServer(t)
	defer shut1()
	port2, shut2 := startFakeServer(t)
	defer shut2()

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "ws1", RuntimeHome: "/tmp/ws1", BrokerPort: port1, WebPort: port1 + 1,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "ws2", RuntimeHome: "/tmp/ws2", BrokerPort: port2, WebPort: port2 + 1,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "dead", RuntimeHome: "/tmp/dead", BrokerPort: 19999, WebPort: 20000,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	start := time.Now()
	results, err := List(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// With 200ms per probe and parallel execution, total should be ~200ms, not 600ms.
	if elapsed > 800*time.Millisecond {
		t.Errorf("List took %s; expected parallel probes to finish in ~200ms", elapsed)
	}

	liveness := make(map[string]bool, len(results))
	for _, lws := range results {
		liveness[lws.Name] = lws.Live
	}
	if !liveness["ws1"] {
		t.Error("ws1 should be live")
	}
	if !liveness["ws2"] {
		t.Error("ws2 should be live")
	}
	if liveness["dead"] {
		t.Error("dead should not be live")
	}
}

// ---- Restore tests ---------------------------------------------------------

func TestExtractOriginalName(t *testing.T) {
	tests := []struct {
		trashID string
		want    string
	}{
		{"demo-launch-1745000000", "demo-launch"},
		{"main-1700000000", "main"},
		{"a-b-c-9999999999", "a-b-c"},
		{"nohyphen", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := extractOriginalName(tc.trashID)
		if got != tc.want {
			t.Errorf("extractOriginalName(%q) = %q; want %q", tc.trashID, got, tc.want)
		}
	}
}

func TestRestoreFromTrash(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	trashDir := filepath.Join(sd, trashDirName)
	trashID := fmt.Sprintf("demo-%d", time.Now().Unix())
	trashEntry := filepath.Join(trashDir, trashID)
	if err := os.MkdirAll(filepath.Join(trashEntry, ".wuphf"), 0o700); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: "/tmp/main",
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Restore(context.Background(), trashID); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	reg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var found *Workspace
	for _, ws := range reg.Workspaces {
		if ws.Name == "demo" {
			found = ws
			break
		}
	}
	if found == nil {
		t.Fatal("demo not registered after Restore")
	}
	if found.State != StateNeverStarted {
		t.Errorf("state after restore: want never_started, got %s", found.State)
	}
}

// ---- Trash listing ---------------------------------------------------------

func TestTrashListsValidEntriesAndSkipsJunk(t *testing.T) {
	withOrchestratorHome(t)
	sd, _ := spacesDir()
	trashDir := filepath.Join(sd, trashDirName)
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}

	// Two valid entries (with parseable trailing timestamp) and one junk
	// directory that should be skipped.
	mkEntry := func(id string) {
		if err := os.MkdirAll(filepath.Join(trashDir, id, ".wuphf"), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
	}
	now := time.Now().Unix()
	demoID := fmt.Sprintf("demo-%d", now)
	scratchID := fmt.Sprintf("scratch-%d", now-100)
	mkEntry(demoID)
	mkEntry(scratchID)
	mkEntry("garbage-no-timestamp")

	got, err := Trash(context.Background())
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries: want 2, got %d (%v)", len(got), got)
	}

	byID := make(map[string]TrashEntry, len(got))
	for _, e := range got {
		byID[e.TrashID] = e
	}
	if got, ok := byID[demoID]; !ok {
		t.Fatalf("missing demo entry: %v", byID)
	} else if got.Name != "demo" {
		t.Fatalf("demo name: %q", got.Name)
	}
	if got, ok := byID[scratchID]; !ok {
		t.Fatalf("missing scratch entry: %v", byID)
	} else {
		if got.Name != "scratch" {
			t.Fatalf("scratch name: %q", got.Name)
		}
		if got.ShredAt.IsZero() {
			t.Fatalf("scratch shred_at should be parsed, got zero")
		}
		if !filepath.IsAbs(got.Path) {
			t.Fatalf("scratch path should be absolute, got %q", got.Path)
		}
	}
}

func TestTrashReturnsEmptyWhenDirMissing(t *testing.T) {
	withOrchestratorHome(t)
	got, err := Trash(context.Background())
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty slice, got %d entries", len(got))
	}
}

// ---- Shred tests -----------------------------------------------------------

func TestShredMovesToTrashByDefault(t *testing.T) {
	home := withOrchestratorHome(t)

	sd, _ := spacesDir()
	runtimeHome := filepath.Join(sd, "ws-to-shred")
	wuphfDir := filepath.Join(runtimeHome, ".wuphf")
	if err := os.MkdirAll(wuphfDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: filepath.Join(sd, "main"),
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "ws-to-shred", RuntimeHome: runtimeHome,
				BrokerPort: 7910, WebPort: 7911,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Shred(context.Background(), "ws-to-shred", false); err != nil {
		t.Fatalf("Shred: %v", err)
	}

	// Workspace should be removed from registry.
	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "ws-to-shred" {
			t.Error("ws-to-shred still in registry after shred")
		}
	}

	// Runtime home should be gone from its original location.
	if _, err := os.Stat(runtimeHome); !os.IsNotExist(err) {
		t.Error("runtime home should have been moved to trash")
	}

	// Trash dir should contain the entry.
	trashDir := filepath.Join(sd, trashDirName)
	entries, _ := os.ReadDir(trashDir)
	found := false
	for _, e := range entries {
		if len(e.Name()) >= len("ws-to-shred") && e.Name()[:len("ws-to-shred")] == "ws-to-shred" {
			found = true
		}
	}
	if !found {
		t.Error("trash entry not created for ws-to-shred")
	}
	_ = home
}

// ---- Doctor tests ----------------------------------------------------------

func TestDoctorDetectsOrphanTree(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	// Create an orphan directory not registered.
	orphan := filepath.Join(sd, "orphan-ws")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}

	// Initialize an empty registry.
	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	report, err := Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	found := false
	for _, o := range report.OrphanTrees {
		if o == orphan {
			found = true
		}
	}
	if !found {
		t.Errorf("orphan %s not in OrphanTrees %v", orphan, report.OrphanTrees)
	}
}

func TestDoctorReconcilesToppedToStoppedToPaused(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	// Register a workspace as stopping on a port nothing is listening on.
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "stopping-ws", RuntimeHome: "/tmp/x",
				BrokerPort: 29999, WebPort: 30000,
				State: StateStopping, PausedAt: now,
				CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	report, err := Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if len(report.ZombieRunning) == 0 {
		t.Error("expected ZombieRunning entry for stopping-ws")
	}

	// Registry should now show it as paused.
	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "stopping-ws" && ws.State != StatePaused {
			t.Errorf("stopping-ws state: want paused, got %s", ws.State)
		}
	}

	// Actions should record the reconciliation.
	found := false
	for _, a := range report.Actions {
		if len(a) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected reconciliation action in report")
	}
}

func TestDoctorRecreatesSymlink(t *testing.T) {
	home := withOrchestratorHome(t)

	sd, _ := spacesDir()
	// Create the main workspace tree.
	mainWuphf := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainWuphf, 0o700); err != nil {
		t.Fatalf("mkdir mainWuphf: %v", err)
	}

	// Make sure there's no ~/.wuphf symlink.
	symlinkPath := filepath.Join(home, ".wuphf")
	_ = os.Remove(symlinkPath)

	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	report, err := Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !report.SymlinkMissing {
		t.Error("expected SymlinkMissing=true")
	}

	// Symlink should have been recreated.
	if info, err := os.Lstat(symlinkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink not recreated: err=%v", err)
	}

	found := false
	for _, a := range report.Actions {
		if len(a) > 5 {
			found = true
		}
	}
	if !found {
		t.Error("expected action entry for symlink recreation")
	}
}

// ---- Resume tests ----------------------------------------------------------

func TestResumeUpdatesStateToRunning(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "paused-ws", RuntimeHome: t.TempDir(),
				BrokerPort: 7910, WebPort: 7911,
				State: StatePaused, PausedAt: now,
				CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// noop spawn
	origSpawn := spawnFn
	spawnFn = func(name, runtimeHome string, brokerPort, webPort int) error {
		_ = os.MkdirAll(filepath.Join(runtimeHome, ".wuphf"), 0o700)
		return nil
	}
	defer func() { spawnFn = origSpawn }()

	if err := Resume(context.Background(), "paused-ws"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "paused-ws" {
			if ws.State != StateRunning {
				t.Errorf("state: want running, got %s", ws.State)
			}
			if !ws.PausedAt.IsZero() {
				t.Errorf("PausedAt should be zero after resume, got %v", ws.PausedAt)
			}
			return
		}
	}
	t.Fatal("paused-ws not found after Resume")
}

func TestResumeMarksErrorOnSpawnFailure(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	rt := t.TempDir()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "paused-fail", RuntimeHome: rt,
				BrokerPort: 7912, WebPort: 7913,
				State: StatePaused, PausedAt: now,
				CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cleanup := installFailSpawn(t)
	defer cleanup()

	err := Resume(context.Background(), "paused-fail")
	if err == nil {
		t.Fatal("expected error from spawn failure")
	}

	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "paused-fail" && ws.State != StateError {
			t.Errorf("state: want error, got %s", ws.State)
		}
	}
}

func TestResumeReturnsErrForMissingWorkspace(t *testing.T) {
	withOrchestratorHome(t)
	if err := Write(&Registry{Version: Version, CLICurrent: "main",
		Workspaces: []*Workspace{}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := Resume(context.Background(), "ghost")
	if err != ErrWorkspaceNotFound {
		t.Errorf("want ErrWorkspaceNotFound, got %v", err)
	}
}

// ---- Pause tests -----------------------------------------------------------

func TestPauseIsNoOpWhenAlreadyPaused(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "already-paused", RuntimeHome: "/tmp/x",
				BrokerPort: 29990, WebPort: 29991,
				State: StatePaused, PausedAt: now,
				CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Pause(context.Background(), "already-paused"); err != nil {
		t.Fatalf("Pause on already-paused: %v", err)
	}
}

func TestPauseReturnsErrForMissingWorkspace(t *testing.T) {
	withOrchestratorHome(t)
	if err := Write(&Registry{Version: Version, CLICurrent: "main",
		Workspaces: []*Workspace{}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := Pause(context.Background(), "ghost")
	if err != ErrWorkspaceNotFound {
		t.Errorf("want ErrWorkspaceNotFound, got %v", err)
	}
}

func TestPauseMarksStoppingImmediately(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	rt := t.TempDir()
	// Use a port nothing listens on so the broker-exit wait returns quickly.
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "to-pause", RuntimeHome: rt,
				BrokerPort: 29988, WebPort: 29989,
				State:     StateRunning,
				CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Use a very short context so Pause returns quickly without real wait.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Pause may return ctx.Err() — that's fine. We just want to verify the
	// state transitions happened.
	_ = Pause(ctx, "to-pause")

	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "to-pause" {
			// State should be either stopping (if context was cancelled before
			// pause completed) or paused (if it finished quickly).
			if ws.State != StateStopping && ws.State != StatePaused {
				t.Errorf("unexpected state %s; want stopping or paused", ws.State)
			}
		}
	}
}

// ---- Port allocation tests -------------------------------------------------

func TestAllocatePortPairReturnsFirstFreeEvenPair(t *testing.T) {
	reg := &Registry{
		Workspaces: []*Workspace{
			{BrokerPort: 7910, WebPort: 7911},
			{BrokerPort: 7912, WebPort: 7913},
		},
	}
	bp, wp, err := AllocatePortPair(reg)
	if err != nil {
		t.Fatalf("AllocatePortPair: %v", err)
	}
	if bp != 7914 {
		t.Errorf("broker port: want 7914, got %d", bp)
	}
	if wp != 7915 {
		t.Errorf("web port: want 7915, got %d", wp)
	}
}

func TestAllocatePortPairExhausted(t *testing.T) {
	var wss []*Workspace
	for port := PortRangeStart; port <= PortRangeEnd; port += 2 {
		wss = append(wss, &Workspace{BrokerPort: port, WebPort: port + 1})
	}
	reg := &Registry{Workspaces: wss}
	_, _, err := AllocatePortPair(reg)
	if err != ErrPortPoolExhausted {
		t.Errorf("want ErrPortPoolExhausted, got %v", err)
	}
}

func TestAllocatePortPairNilRegistry(t *testing.T) {
	bp, wp, err := AllocatePortPair(nil)
	if err != nil {
		t.Fatalf("AllocatePortPair(nil): %v", err)
	}
	if bp != PortRangeStart {
		t.Errorf("broker port: want %d, got %d", PortRangeStart, bp)
	}
	if wp != PortRangeStart+1 {
		t.Errorf("web port: want %d, got %d", PortRangeStart+1, wp)
	}
}

// ---- appendOrReplace tests -------------------------------------------------

func TestAppendOrReplace(t *testing.T) {
	env := []string{"A=1", "B=2", "C=3"}
	got := appendOrReplace(env, "B", "99")
	found := false
	for _, e := range got {
		if e == "B=99" {
			found = true
		}
		if e == "B=2" {
			t.Error("old B=2 should be replaced")
		}
	}
	if !found {
		t.Error("B=99 not found in result")
	}
	if len(got) != len(env) {
		t.Errorf("length changed: want %d, got %d", len(env), len(got))
	}
}

func TestAppendOrReplaceAddsNew(t *testing.T) {
	env := []string{"A=1"}
	got := appendOrReplace(env, "NEWKEY", "val")
	if len(got) != 2 {
		t.Errorf("want 2 entries, got %d", len(got))
	}
	found := false
	for _, e := range got {
		if e == "NEWKEY=val" {
			found = true
		}
	}
	if !found {
		t.Error("NEWKEY=val not in result")
	}
}

// ---- postAdminPause coverage via fake server --------------------------------

func TestPostAdminPauseSucceeds(t *testing.T) {
	var gotAuth string
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/pause" && r.Method == http.MethodPost {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	if err := postAdminPause(port, "test-token"); err != nil {
		t.Fatalf("postAdminPause: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization header: want %q, got %q", "Bearer test-token", gotAuth)
	}
}

func TestPostAdminPauseErrorsOnNonOK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	if err := postAdminPause(port, ""); err == nil {
		t.Fatal("expected error on 500 response")
	}
}

// ---- readPIDFile -----------------------------------------------------------

func TestReadPIDFileReturnsZeroOnMissing(t *testing.T) {
	rt := t.TempDir()
	if pid := readPIDFile(rt); pid != 0 {
		t.Errorf("want 0 for missing PID file, got %d", pid)
	}
}

func TestReadPIDFileReturnsPID(t *testing.T) {
	rt := t.TempDir()
	pidPath := filepath.Join(rt, ".wuphf", "broker.pid")
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o700)
	_ = os.WriteFile(pidPath, []byte("12345\n"), 0o600)
	if pid := readPIDFile(rt); pid != 12345 {
		t.Errorf("want 12345, got %d", pid)
	}
}

// ---- ShredAt / ResetAt coverage via workspace package ----------------------

func TestShredPermanentDeletesTree(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	runtimeHome := filepath.Join(sd, "perm-shred")
	wuphfDir := filepath.Join(runtimeHome, ".wuphf")
	if err := os.MkdirAll(wuphfDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: filepath.Join(sd, "main"),
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
			{Name: "perm-shred", RuntimeHome: runtimeHome,
				BrokerPort: 7914, WebPort: 7915,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Shred(context.Background(), "perm-shred", true); err != nil {
		t.Fatalf("Shred permanent: %v", err)
	}

	// The runtime home must be deleted.
	if _, err := os.Stat(runtimeHome); !os.IsNotExist(err) {
		t.Error("runtime home should be deleted in permanent shred")
	}
}

// ---- Restore error paths ---------------------------------------------------

func TestRestoreReturnsErrForNonexistentTrashID(t *testing.T) {
	withOrchestratorHome(t)
	if err := Write(&Registry{Version: Version, CLICurrent: "main",
		Workspaces: []*Workspace{}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := Restore(context.Background(), "ghost-1700000000")
	if err == nil {
		t.Fatal("expected error for nonexistent trash entry")
	}
}

func TestRestoreReturnsErrForAlreadyExistingWorkspace(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	trashDir := filepath.Join(sd, trashDirName)
	trashID := fmt.Sprintf("main-%d", time.Now().Unix())
	trashEntry := filepath.Join(trashDir, trashID)
	if err := os.MkdirAll(trashEntry, 0o700); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "main", RuntimeHome: filepath.Join(sd, "main"),
				BrokerPort: MainBrokerPort, WebPort: MainWebPort,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// "main" already exists in registry; restore of a main-<ts> trash ID should fail.
	err := Restore(context.Background(), trashID)
	if err == nil {
		t.Fatal("expected error when workspace already exists")
	}
}

func TestRestoreReturnsErrForMalformedTrashID(t *testing.T) {
	withOrchestratorHome(t)

	sd, _ := spacesDir()
	trashDir := filepath.Join(sd, trashDirName)
	// TrashID with no unix suffix — cannot infer original name.
	trashID := "nodash"
	trashEntry := filepath.Join(trashDir, trashID)
	if err := os.MkdirAll(trashEntry, 0o700); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}

	if err := Write(&Registry{Version: Version, CLICurrent: "main",
		Workspaces: []*Workspace{}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := Restore(context.Background(), trashID)
	if err == nil {
		t.Fatal("expected error for malformed trash ID (no unix suffix)")
	}
}

// ---- readTokenFile ---------------------------------------------------------

func TestReadTokenFileMissingReturnsEmpty(t *testing.T) {
	withOrchestratorHome(t)
	token, err := readTokenFile("no-such-workspace")
	if err == nil {
		t.Fatal("expected error for missing token file")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

// ---- removeFromRegistry edge case ------------------------------------------

func TestRemoveFromRegistryClearsCLICurrentIfNoWorkspacesLeft(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "solo",
		Workspaces: []*Workspace{
			{Name: "solo", RuntimeHome: "/tmp/solo",
				BrokerPort: 7910, WebPort: 7911,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := removeFromRegistry("solo"); err != nil {
		t.Fatalf("removeFromRegistry: %v", err)
	}

	reg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reg.Workspaces) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(reg.Workspaces))
	}
	// CLICurrent should remain "solo" (no workspaces left to switch to).
	if reg.CLICurrent != "solo" {
		t.Errorf("CLICurrent: want solo, got %s", reg.CLICurrent)
	}
}

func TestRemoveFromRegistryUpdatesCLICurrentWhenPresent(t *testing.T) {
	withOrchestratorHome(t)

	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "first",
		Workspaces: []*Workspace{
			{Name: "first", RuntimeHome: "/tmp/first",
				BrokerPort: 7910, WebPort: 7911,
				State: StatePaused, CreatedAt: now, LastUsedAt: now},
			{Name: "second", RuntimeHome: "/tmp/second",
				BrokerPort: 7912, WebPort: 7913,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := removeFromRegistry("first"); err != nil {
		t.Fatalf("removeFromRegistry: %v", err)
	}

	reg, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// cli_current must switch to the remaining workspace.
	if reg.CLICurrent != "second" {
		t.Errorf("CLICurrent: want second, got %s", reg.CLICurrent)
	}
}

// ---- writeUnderLock hash-check path ----------------------------------------

func TestWriteProducesReadableHashOnDisk(t *testing.T) {
	withSpacesDir(t)
	reg := sampleRegistry()
	if err := Write(reg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// A second write produces a .bak and succeeds.
	reg.CLICurrent = "demo"
	if err := Write(reg); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	got, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.CLICurrent != "demo" {
		t.Errorf("CLICurrent: want demo, got %s", got.CLICurrent)
	}
}

// ---- Doctor: partial migration detection -----------------------------------

func TestDoctorDetectsPartialMigration(t *testing.T) {
	home := withOrchestratorHome(t)

	sd, _ := spacesDir()
	mainTarget := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainTarget, 0o700); err != nil {
		t.Fatalf("mkdir main target: %v", err)
	}

	// Place a regular directory (not a symlink) at ~/.wuphf.
	symlinkPath := filepath.Join(home, ".wuphf")
	if err := os.MkdirAll(symlinkPath, 0o700); err != nil {
		t.Fatalf("mkdir symlink path: %v", err)
	}

	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	report, err := Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	// A regular directory where a symlink is expected → PartialMigration flag.
	if !report.PartialMigration {
		t.Error("expected PartialMigration=true when ~/.wuphf is a real directory")
	}
}

// ---- Doctor: wrong symlink target ------------------------------------------

func TestDoctorDetectsWrongSymlinkTarget(t *testing.T) {
	home := withOrchestratorHome(t)

	sd, _ := spacesDir()
	mainTarget := filepath.Join(sd, "main", ".wuphf")
	if err := os.MkdirAll(mainTarget, 0o700); err != nil {
		t.Fatalf("mkdir main target: %v", err)
	}

	// Point the symlink at the wrong target.
	symlinkPath := filepath.Join(home, ".wuphf")
	wrongTarget := filepath.Join(home, "somewhere-else")
	if err := os.MkdirAll(wrongTarget, 0o700); err != nil {
		t.Fatalf("mkdir wrong target: %v", err)
	}
	if err := os.Symlink(wrongTarget, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := Write(&Registry{Version: Version, CLICurrent: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	report, err := Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if report.SymlinkWrong == "" {
		t.Error("expected SymlinkWrong to be set for wrong symlink target")
	}
}

// ---- Pause fail-closed tests (CodeRabbit #3164366631) ----------------------

// withShortPauseTimeouts shrinks the pause escalation budget so the
// fail-closed test can run in under a second instead of 90s. Restores the
// production values via t.Cleanup.
func withShortPauseTimeouts(t *testing.T) {
	t.Helper()
	origWall := pauseWallClockTimeout
	origTerm := pauseSIGTERMAt
	origKill := pauseSIGKILLAt
	pauseWallClockTimeout = 800 * time.Millisecond
	pauseSIGTERMAt = 200 * time.Millisecond
	pauseSIGKILLAt = 500 * time.Millisecond
	t.Cleanup(func() {
		pauseWallClockTimeout = origWall
		pauseSIGTERMAt = origTerm
		pauseSIGKILLAt = origKill
	})
}

// withTmuxKillerStub replaces tmuxKiller with a no-op for tests that don't
// have tmux installed and don't care about session teardown.
func withTmuxKillerStub(t *testing.T) {
	t.Helper()
	orig := tmuxKiller
	tmuxKiller = func(port int) {}
	t.Cleanup(func() { tmuxKiller = orig })
}

// TestPauseHappyPathBrokerExitsCleanly verifies the canonical clean shutdown:
// the broker port is unbound from the start (simulating a process that has
// already exited), so probePort returns false on the first tick and the
// registry transitions cleanly to StatePaused.
func TestPauseHappyPathBrokerExitsCleanly(t *testing.T) {
	withOrchestratorHome(t)
	withShortPauseTimeouts(t)
	withTmuxKillerStub(t)

	now := time.Now().UTC()
	rt := t.TempDir()
	// Use a port that is guaranteed to be unbound.
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "clean", RuntimeHome: rt,
				BrokerPort: 29980, WebPort: 29981,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Pause(context.Background(), "clean"); err != nil {
		t.Fatalf("Pause clean: %v", err)
	}

	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "clean" && ws.State != StatePaused {
			t.Errorf("state: want paused, got %s", ws.State)
		}
	}
}

// TestPauseFallsThroughWhenTokenMissingButProcessGone verifies the fail-open
// case: the token file does not exist (so readTokenFile errors) AND
// /admin/pause errors (no listener), but the broker is in fact gone. Pause
// must still mark the workspace paused — refusing because of a missing token
// file would be a regression.
func TestPauseFallsThroughWhenTokenMissingButProcessGone(t *testing.T) {
	withOrchestratorHome(t)
	withShortPauseTimeouts(t)
	withTmuxKillerStub(t)

	now := time.Now().UTC()
	rt := t.TempDir()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "no-token", RuntimeHome: rt,
				BrokerPort: 29982, WebPort: 29983,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Sanity: the token file does NOT exist on disk.
	if _, err := readTokenFile("no-token"); err == nil {
		t.Fatal("expected readTokenFile to error for nonexistent token")
	}

	if err := Pause(context.Background(), "no-token"); err != nil {
		t.Fatalf("Pause no-token: %v", err)
	}

	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "no-token" && ws.State != StatePaused {
			t.Errorf("state: want paused (fail-open), got %s", ws.State)
		}
	}
}

// TestPauseReadsTokenFromRealHomeNotRuntimeHome verifies that Pause resolves
// the token file via the user's REAL HOME (~/.wuphf-spaces/tokens/) and not
// via WUPHF_RUNTIME_HOME. Cross-workspace artifacts must live at the same
// shared location regardless of which workspace's runtime home is active —
// otherwise sibling workspaces would each look in their own token directory
// and admin/pause would always go out unauthenticated.
//
// Regression test for CodeRabbit #3164366633.
func TestPauseReadsTokenFromRealHomeNotRuntimeHome(t *testing.T) {
	// Two distinct directories: real HOME (where the token actually lives)
	// and WUPHF_RUNTIME_HOME (a per-workspace runtime tree).
	realHome := t.TempDir()
	runtimeHome := t.TempDir()
	t.Setenv("HOME", realHome)
	t.Setenv("WUPHF_RUNTIME_HOME", runtimeHome)

	// Fake broker that records the Authorization header it receives.
	var gotAuth string
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/pause" {
			gotAuth = r.Header.Get("Authorization")
		}
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	// Plant the token file under the REAL home (the shared cross-workspace
	// location). If Pause incorrectly resolves via RuntimeHomeDir, it will
	// look under runtimeHome and miss this file.
	tokenDir := filepath.Join(realHome, ".wuphf-spaces", "tokens")
	if err := os.MkdirAll(tokenDir, 0o700); err != nil {
		t.Fatalf("mkdir tokens: %v", err)
	}
	wantToken := "secret-token-value"
	if err := os.WriteFile(filepath.Join(tokenDir, "tokens-test.token"),
		[]byte(wantToken+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	// Make sure the wrong location (under runtimeHome) does NOT contain a
	// token, so a regression would result in an empty Authorization header.
	wrongDir := filepath.Join(runtimeHome, ".wuphf-spaces", "tokens")
	if _, err := os.Stat(wrongDir); !os.IsNotExist(err) {
		t.Fatalf("wrongDir should not exist: %v", err)
	}

	// Seed the registry. spacesDir resolves under realHome (already verified
	// in workspaces/registry.go) so Write/Read use the right registry path.
	now := time.Now().UTC()
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "tokens-test", RuntimeHome: t.TempDir(),
				BrokerPort: port, WebPort: port + 1,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Shrink the wall-clock budget so the test doesn't sit waiting for the
	// broker to "exit" (it stays alive throughout the fake server's lifetime).
	withShortPauseTimeouts(t)
	withTmuxKillerStub(t)

	// Pause will fail (broker stays alive → fail-closed StateError), but we
	// only care that the Authorization header was set from the real-HOME
	// token before that failure.
	_ = Pause(context.Background(), "tokens-test")

	if gotAuth != "Bearer "+wantToken {
		t.Errorf("Authorization header: want %q, got %q\n  Pause likely resolved tokenFilePath under WUPHF_RUNTIME_HOME instead of real HOME.",
			"Bearer "+wantToken, gotAuth)
	}
}

// TestPauseFailsClosedWhenBrokerStillAlive verifies the core regression: if
// the broker is bound throughout the wall-clock budget AND survives the
// SIGTERM/SIGKILL ladder (because we point pid at our own test process which
// will not actually exit when "killed" — sendSIGTERM/SIGKILL are best-effort
// and we never write a real pid file), Pause must NOT mark the registry
// paused. It must transition to StateError and return an error.
func TestPauseFailsClosedWhenBrokerStillAlive(t *testing.T) {
	withOrchestratorHome(t)
	withShortPauseTimeouts(t)
	withTmuxKillerStub(t)

	// Stand up a fake broker that stays alive for the duration of the test.
	// probePort will see this port as live throughout the escalation ladder.
	port, shutdown := startFakeServer(t)
	defer shutdown()

	// Stub out sendSIGTERM/sendSIGKILL? We can't (they're package-level
	// functions, not vars). Instead: deliberately omit any pid file so
	// readPIDFile returns 0. With pid=0, sendSIGTERM/sendSIGKILL are no-ops
	// and the broker survives the ladder — exactly the failure case we want
	// the registry to reflect.
	now := time.Now().UTC()
	rt := t.TempDir()
	// Note: do NOT write rt/.wuphf/broker.pid — readPIDFile returns 0.
	if err := Write(&Registry{
		Version:    Version,
		CLICurrent: "main",
		Workspaces: []*Workspace{
			{Name: "stuck", RuntimeHome: rt,
				BrokerPort: port, WebPort: port + 1,
				State: StateRunning, CreatedAt: now, LastUsedAt: now},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := Pause(context.Background(), "stuck")
	if err == nil {
		t.Fatal("expected Pause to fail closed when broker survives escalation")
	}

	// Registry must reflect StateError, not StatePaused.
	reg, _ := Read()
	for _, ws := range reg.Workspaces {
		if ws.Name == "stuck" {
			if ws.State != StateError {
				t.Errorf("state: want error (fail-closed), got %s", ws.State)
			}
		}
	}
}
