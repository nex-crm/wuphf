package team

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
)

type headlessCodexRecord struct {
	Args  []string `json:"args"`
	Dir   string   `json:"dir"`
	Env   []string `json:"env"`
	Stdin string   `json:"stdin"`
}

type processedTurn struct {
	notification string
	channel      string
}

func TestNewLauncherUsesCodexProviderFromConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_BROKER_TOKEN", "")
	if err := config.Save(config.Config{LLMProvider: "codex"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	l, err := NewLauncher("founding-team")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}
	if l.provider != "codex" {
		t.Fatalf("expected codex provider, got %q", l.provider)
	}
	if l.UsesTmuxRuntime() {
		t.Fatal("expected codex launcher to use headless runtime")
	}
}

func TestBuildCodexOfficeConfigOverridesIncludesOfficeMCPEnv(t *testing.T) {
	oldExecutablePath := headlessCodexExecutablePath
	oldLookPath := headlessCodexLookPath
	headlessCodexExecutablePath = func() (string, error) { return "/tmp/wuphf", nil }
	headlessCodexLookPath = func(file string) (string, error) {
		return "", exec.ErrNotFound
	}
	defer func() {
		headlessCodexExecutablePath = oldExecutablePath
		headlessCodexLookPath = oldLookPath
	}()

	t.Setenv("WUPHF_NO_NEX", "1")

	broker := NewBroker()
	if err := broker.SetSessionMode(SessionModeOneOnOne, "pm"); err != nil {
		t.Fatalf("SetSessionMode: %v", err)
	}
	l := &Launcher{
		broker:      broker,
		pack:        agent.GetPack("founding-team"),
		sessionMode: SessionModeOneOnOne,
		oneOnOne:    "pm",
	}

	overrides, err := l.buildCodexOfficeConfigOverrides("pm")
	if err != nil {
		t.Fatalf("buildCodexOfficeConfigOverrides: %v", err)
	}
	joined := strings.Join(overrides, "\n")
	if !strings.Contains(joined, `mcp_servers.wuphf-office.command="/tmp/wuphf"`) {
		t.Fatalf("expected WUPHF MCP command override, got %q", joined)
	}
	if !strings.Contains(joined, `mcp_servers.wuphf-office.args=["mcp-team"]`) {
		t.Fatalf("expected WUPHF MCP args override, got %q", joined)
	}
	if !strings.Contains(joined, `mcp_servers.wuphf-office.env_vars=["WUPHF_AGENT_SLUG", "WUPHF_BROKER_TOKEN", "WUPHF_NO_NEX", "WUPHF_ONE_ON_ONE", "WUPHF_ONE_ON_ONE_AGENT"]`) {
		t.Fatalf("expected office env var forwarding, got %q", joined)
	}
	if strings.Contains(joined, broker.Token()) {
		t.Fatalf("expected broker token value to stay out of args, got %q", joined)
	}
	if strings.Contains(joined, `mcp_servers.nex.command=`) {
		t.Fatalf("expected Nex MCP to stay disabled with WUPHF_NO_NEX, got %q", joined)
	}
}

func TestRunHeadlessCodexTurnUsesHeadlessOfficeRuntime(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "headless-codex-record.jsonl")
	oldLookPath := headlessCodexLookPath
	oldExecutablePath := headlessCodexExecutablePath
	oldCommandContext := headlessCodexCommandContext
	headlessCodexLookPath = func(file string) (string, error) {
		switch file {
		case "codex":
			return "/usr/bin/codex", nil
		case "nex-mcp":
			return "/usr/bin/nex-mcp", nil
		default:
			return "", exec.ErrNotFound
		}
	}
	headlessCodexExecutablePath = func() (string, error) { return "/tmp/wuphf", nil }
	headlessCodexCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestHeadlessCodexHelperProcess", "--"}
		cmdArgs = append(cmdArgs, args...)
		return exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	}
	defer func() {
		headlessCodexLookPath = oldLookPath
		headlessCodexExecutablePath = oldExecutablePath
		headlessCodexCommandContext = oldCommandContext
	}()

	t.Setenv("GO_WANT_HEADLESS_CODEX_HELPER_PROCESS", "1")
	t.Setenv("HEADLESS_CODEX_RECORD_FILE", recordFile)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_API_KEY", "nex-secret-key")
	t.Setenv("WUPHF_ONE_SECRET", "one-secret-value")
	t.Setenv("WUPHF_ONE_IDENTITY", "founder@example.com")
	t.Setenv("WUPHF_ONE_IDENTITY_TYPE", "user")

	l := &Launcher{
		pack:        agent.GetPack("founding-team"),
		cwd:         t.TempDir(),
		broker:      NewBroker(),
		headlessCtx: context.Background(),
	}

	if err := l.runHeadlessCodexTurn(context.Background(), "ceo", "You have new work in #launch."); err != nil {
		t.Fatalf("runHeadlessCodexTurn: %v", err)
	}

	record := readHeadlessCodexRecord(t, recordFile)
	joinedArgs := strings.Join(record.Args, " ")
	if !strings.Contains(joinedArgs, "exec") || !strings.Contains(joinedArgs, "--ephemeral") {
		t.Fatalf("expected codex exec args, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, "--disable plugins") {
		t.Fatalf("expected plugins feature to be disabled, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, `mcp_servers.wuphf-office.command="/tmp/wuphf"`) {
		t.Fatalf("expected office MCP override, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, `mcp_servers.wuphf-office.env_vars=["WUPHF_AGENT_SLUG", "WUPHF_BROKER_TOKEN", "ONE_SECRET", "ONE_IDENTITY", "ONE_IDENTITY_TYPE"]`) {
		t.Fatalf("expected office env var forwarding, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, `mcp_servers.nex.command="/usr/bin/nex-mcp"`) {
		t.Fatalf("expected nex MCP override, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, `mcp_servers.nex.env_vars=["WUPHF_API_KEY", "NEX_API_KEY"]`) {
		t.Fatalf("expected nex env var forwarding, got %#v", record.Args)
	}
	if got := argValue(record.Args, "-C"); !samePath(got, l.cwd) {
		t.Fatalf("expected codex workspace root %q, got %q", l.cwd, got)
	}
	if !samePath(record.Dir, l.cwd) {
		t.Fatalf("expected command dir %q, got %q", l.cwd, record.Dir)
	}
	if !containsEnv(record.Env, "WUPHF_AGENT_SLUG=ceo") {
		t.Fatalf("expected agent env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "HOME="+os.Getenv("HOME")) {
		t.Fatalf("expected HOME env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "CODEX_HOME="+filepath.Join(os.Getenv("HOME"), ".wuphf", "codex-headless")) {
		t.Fatalf("expected absolute CODEX_HOME env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "WUPHF_HEADLESS_PROVIDER=codex") {
		t.Fatalf("expected headless provider env, got %#v", record.Env)
	}
	if got := envValue(record.Env, "GOCACHE"); !samePath(got, filepath.Join(l.cwd, ".wuphf", "cache", "go-build", "ceo")) {
		t.Fatalf("expected repo-local GOCACHE, got %#v", record.Env)
	}
	if got := envValue(record.Env, "GOTMPDIR"); !samePath(got, filepath.Join(l.cwd, ".wuphf", "cache", "go-tmp", "ceo")) {
		t.Fatalf("expected repo-local GOTMPDIR, got %#v", record.Env)
	}
	if !containsEnvPrefix(record.Env, "WUPHF_BROKER_TOKEN=") {
		t.Fatalf("expected broker token env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "WUPHF_API_KEY=nex-secret-key") || !containsEnv(record.Env, "NEX_API_KEY=nex-secret-key") {
		t.Fatalf("expected nex API env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "ONE_SECRET=one-secret-value") {
		t.Fatalf("expected one secret env, got %#v", record.Env)
	}
	if strings.Contains(joinedArgs, l.broker.Token()) || strings.Contains(joinedArgs, "nex-secret-key") || strings.Contains(joinedArgs, "one-secret-value") {
		t.Fatalf("expected secret values to stay out of args, got %#v", record.Args)
	}
	if !strings.Contains(record.Stdin, "<system>") || !strings.Contains(record.Stdin, "You have new work in #launch.") {
		t.Fatalf("expected notification prompt in stdin, got %q", record.Stdin)
	}
}

func TestRunHeadlessCodexTurnUsesAssignedWorktreeForCodingAgents(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "headless-codex-record.jsonl")
	worktreeDir := t.TempDir()
	repoRoot := t.TempDir()

	oldLookPath := headlessCodexLookPath
	oldExecutablePath := headlessCodexExecutablePath
	oldCommandContext := headlessCodexCommandContext
	oldPrepareTaskWorktree := prepareTaskWorktree
	headlessCodexLookPath = func(file string) (string, error) {
		switch file {
		case "codex":
			return "/usr/bin/codex", nil
		default:
			return "", exec.ErrNotFound
		}
	}
	headlessCodexExecutablePath = func() (string, error) { return "/tmp/wuphf", nil }
	headlessCodexCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestHeadlessCodexHelperProcess", "--"}
		cmdArgs = append(cmdArgs, args...)
		return exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	}
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return worktreeDir, worktreeBranchName(taskID), nil
	}
	defer func() {
		headlessCodexLookPath = oldLookPath
		headlessCodexExecutablePath = oldExecutablePath
		headlessCodexCommandContext = oldCommandContext
		prepareTaskWorktree = oldPrepareTaskWorktree
	}()

	t.Setenv("GO_WANT_HEADLESS_CODEX_HELPER_PROCESS", "1")
	t.Setenv("HEADLESS_CODEX_RECORD_FILE", recordFile)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PWD", repoRoot)
	t.Setenv("OLDPWD", "/tmp/previous")
	t.Setenv("CODEX_THREAD_ID", "thread-from-controller")
	t.Setenv("CODEX_TUI_RECORD_SESSION", "1")
	t.Setenv("CODEX_TUI_SESSION_LOG_PATH", "/tmp/controller-session.jsonl")

	broker := NewBroker()
	task, _, err := broker.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Build the automation runtime",
		Details:       "Implement in the assigned worktree.",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		PipelineID:    "feature",
		ExecutionMode: "local_worktree",
		ReviewState:   "pending_review",
	})
	if err != nil {
		t.Fatalf("EnsurePlannedTask: %v", err)
	}
	if task.WorktreePath != worktreeDir {
		t.Fatalf("expected assigned worktree %q, got %q", worktreeDir, task.WorktreePath)
	}

	l := &Launcher{
		pack:        agent.GetPack("founding-team"),
		cwd:         repoRoot,
		broker:      broker,
		headlessCtx: context.Background(),
	}

	if err := l.runHeadlessCodexTurn(context.Background(), "eng", "Ship the automation runtime."); err != nil {
		t.Fatalf("runHeadlessCodexTurn: %v", err)
	}

	record := readHeadlessCodexRecord(t, recordFile)
	joinedArgs := strings.Join(record.Args, " ")
	if got := argValue(record.Args, "-C"); !samePath(got, worktreeDir) {
		t.Fatalf("expected codex worktree %q, got %q", worktreeDir, got)
	}
	if !strings.Contains(joinedArgs, "--disable plugins") {
		t.Fatalf("expected plugins feature to be disabled, got %#v", record.Args)
	}
	if !samePath(record.Dir, worktreeDir) {
		t.Fatalf("expected command dir %q, got %q", worktreeDir, record.Dir)
	}
	if got := envValue(record.Env, "WUPHF_WORKTREE_PATH"); !samePath(got, worktreeDir) {
		t.Fatalf("expected worktree env, got %#v", record.Env)
	}
	if got := envValue(record.Env, "PWD"); !samePath(got, worktreeDir) {
		t.Fatalf("expected PWD to match worktree, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "CODEX_HOME="+filepath.Join(os.Getenv("HOME"), ".wuphf", "codex-headless")) {
		t.Fatalf("expected absolute CODEX_HOME env, got %#v", record.Env)
	}
	if got := envValue(record.Env, "GOCACHE"); !samePath(got, filepath.Join(worktreeDir, ".wuphf", "cache", "go-build", "eng")) {
		t.Fatalf("expected worktree-local GOCACHE, got %#v", record.Env)
	}
	if got := envValue(record.Env, "GOTMPDIR"); !samePath(got, filepath.Join(worktreeDir, ".wuphf", "cache", "go-tmp", "eng")) {
		t.Fatalf("expected worktree-local GOTMPDIR, got %#v", record.Env)
	}
	for _, forbiddenPrefix := range []string{
		"OLDPWD=",
		"CODEX_THREAD_ID=",
		"CODEX_TUI_RECORD_SESSION=",
		"CODEX_TUI_SESSION_LOG_PATH=",
	} {
		if containsEnvPrefix(record.Env, forbiddenPrefix) {
			t.Fatalf("expected %s to be stripped, got %#v", forbiddenPrefix, record.Env)
		}
	}
}

func TestRunHeadlessCodexTurnPassesScopedChannelEnv(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "headless-codex-record.jsonl")
	oldLookPath := headlessCodexLookPath
	oldExecutablePath := headlessCodexExecutablePath
	oldCommandContext := headlessCodexCommandContext
	headlessCodexLookPath = func(file string) (string, error) {
		switch file {
		case "codex":
			return "/usr/bin/codex", nil
		default:
			return "", exec.ErrNotFound
		}
	}
	headlessCodexExecutablePath = func() (string, error) { return "/tmp/wuphf", nil }
	headlessCodexCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestHeadlessCodexHelperProcess", "--"}
		cmdArgs = append(cmdArgs, args...)
		return exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	}
	defer func() {
		headlessCodexLookPath = oldLookPath
		headlessCodexExecutablePath = oldExecutablePath
		headlessCodexCommandContext = oldCommandContext
	}()

	t.Setenv("GO_WANT_HEADLESS_CODEX_HELPER_PROCESS", "1")
	t.Setenv("HEADLESS_CODEX_RECORD_FILE", recordFile)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_CHANNEL", "general")

	l := &Launcher{
		pack:        agent.GetPack("founding-team"),
		cwd:         t.TempDir(),
		broker:      NewBroker(),
		headlessCtx: context.Background(),
	}

	if err := l.runHeadlessCodexTurn(context.Background(), "eng", "Work the owned task.", "youtube-factory"); err != nil {
		t.Fatalf("runHeadlessCodexTurn: %v", err)
	}

	record := readHeadlessCodexRecord(t, recordFile)
	if !containsEnv(record.Env, "WUPHF_CHANNEL=youtube-factory") {
		t.Fatalf("expected scoped channel env, got %#v", record.Env)
	}
}

func TestHeadlessCodexHomeDirNormalizesRelativeEnv(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	t.Setenv("CODEX_HOME", ".codex-relative")

	got := headlessCodexHomeDir()
	want := filepath.Join(wd, ".codex-relative")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if !samePath(got, want) {
		t.Fatalf("expected absolute CODEX_HOME %q, got %q", want, got)
	}
}

func TestPrepareHeadlessCodexHomeUsesDedicatedRuntimeHomeAndCopiesAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sourceHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(sourceHome, 0o755); err != nil {
		t.Fatalf("MkdirAll source home: %v", err)
	}
	wantAuth := []byte(`{"access_token":"test-token"}`)
	if err := os.WriteFile(filepath.Join(sourceHome, "auth.json"), wantAuth, 0o600); err != nil {
		t.Fatalf("write source auth: %v", err)
	}

	got := prepareHeadlessCodexHome()
	want := filepath.Join(home, ".wuphf", "codex-headless")
	if !samePath(got, want) {
		t.Fatalf("expected runtime headless home %q, got %q", want, got)
	}
	authCopy, err := os.ReadFile(filepath.Join(want, "auth.json"))
	if err != nil {
		t.Fatalf("read copied auth: %v", err)
	}
	if string(authCopy) != string(wantAuth) {
		t.Fatalf("expected copied auth %q, got %q", string(wantAuth), string(authCopy))
	}
}

func TestEnqueueHeadlessCodexTurnProcessesFIFO(t *testing.T) {
	oldRunTurn := headlessCodexRunTurn
	processed := make(chan string, 4)
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, _ string, notification string, channel ...string) error {
		processed <- notification
		return nil
	}
	defer func() { headlessCodexRunTurn = oldRunTurn }()

	l := newHeadlessLauncherForTest()

	// Use a specialist slug (not the lead/ceo) so the cap-at-1 and queue-hold
	// logic for the lead agent does not interfere with this FIFO test.
	l.enqueueHeadlessCodexTurn("fe", "first")
	l.enqueueHeadlessCodexTurn("fe", "second")

	first := waitForString(t, processed)
	second := waitForString(t, processed)
	if first != "first" || second != "second" {
		t.Fatalf("expected FIFO order, got %q then %q", first, second)
	}
}

func TestSendTaskUpdatePassesTaskChannelToHeadlessTurn(t *testing.T) {
	oldRunTurn := headlessCodexRunTurn
	processed := make(chan processedTurn, 1)
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, _ string, notification string, channel ...string) error {
		processed <- processedTurn{
			notification: notification,
			channel:      firstNonEmpty(channel...),
		}
		return nil
	}
	defer func() { headlessCodexRunTurn = oldRunTurn }()

	l := newHeadlessLauncherForTest()
	l.provider = "codex"
	l.pack = agent.GetPack("founding-team")

	l.sendTaskUpdate(notificationTarget{Slug: "eng"}, officeActionLog{
		Kind:    "task_updated",
		Actor:   "ceo",
		Channel: "youtube-factory",
	}, teamTask{
		ID:      "task-3",
		Channel: "youtube-factory",
		Title:   "Build the faceless YouTube factory MVP in-repo",
		Owner:   "eng",
		Status:  "in_progress",
	}, "Continue shipping the owned build.")

	got := waitForProcessedTurn(t, processed)
	if got.channel != "youtube-factory" {
		t.Fatalf("expected task update to preserve channel, got %+v", got)
	}
	if !strings.Contains(got.notification, "#youtube-factory") {
		t.Fatalf("expected notification to reference youtube-factory, got %+v", got)
	}
}

func TestEnqueueHeadlessCodexTurnCancelsStaleTurn(t *testing.T) {
	oldRunTurn := headlessCodexRunTurn
	oldTimeout := headlessCodexTurnTimeout
	oldStale := headlessCodexStaleCancelAfter
	headlessCodexTurnTimeout = 5 * time.Second
	headlessCodexStaleCancelAfter = 20 * time.Millisecond
	defer func() {
		headlessCodexRunTurn = oldRunTurn
		headlessCodexTurnTimeout = oldTimeout
		headlessCodexStaleCancelAfter = oldStale
	}()

	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	processed := make(chan string, 4)
	headlessCodexRunTurn = func(_ *Launcher, ctx context.Context, _ string, notification string, channel ...string) error {
		if notification == "first" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			select {
			case cancelled <- struct{}{}:
			default:
			}
			return ctx.Err()
		}
		processed <- notification
		return nil
	}

	l := newHeadlessLauncherForTest()
	l.enqueueHeadlessCodexTurn("ceo", "first")
	waitForSignal(t, started)
	time.Sleep(35 * time.Millisecond)
	l.enqueueHeadlessCodexTurn("ceo", "second")

	waitForSignal(t, cancelled)
	if got := waitForString(t, processed); got != "second" {
		t.Fatalf("expected queued turn to run after cancellation, got %q", got)
	}
}

func TestHeadlessCodexHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HEADLESS_CODEX_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	doubleDash := 0
	for i, arg := range args {
		if arg == "--" {
			doubleDash = i
			break
		}
	}
	codexArgs := append([]string(nil), args[doubleDash+1:]...)
	stdin, _ := io.ReadAll(os.Stdin)

	record := headlessCodexRecord{
		Args:  codexArgs,
		Dir:   mustGetwd(t),
		Env:   os.Environ(),
		Stdin: string(stdin),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal helper record: %v", err)
	}
	recordPath := os.Getenv("HEADLESS_CODEX_RECORD_FILE")
	if err := os.WriteFile(recordPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write helper record: %v", err)
	}

	if !containsArg(codexArgs, "--json") {
		t.Fatalf("missing --json arg: %#v", codexArgs)
	}
	_, _ = os.Stdout.WriteString("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"codex office reply\"}}\n")
	os.Exit(0)
}

func readHeadlessCodexRecord(t *testing.T, path string) headlessCodexRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}
	var record headlessCodexRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	return record
}

func containsEnv(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsEnvPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func envValue(values []string, key string) string {
	prefix := strings.TrimSpace(key) + "="
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return ""
}

func containsArg(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func argValue(values []string, key string) string {
	for i := 0; i < len(values)-1; i++ {
		if values[i] == key {
			return values[i+1]
		}
	}
	return ""
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

func samePath(a, b string) bool {
	return canonicalPath(a) == canonicalPath(b)
}

func canonicalPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

func newHeadlessLauncherForTest() *Launcher {
	return &Launcher{
		headlessCtx:     context.Background(),
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
		pack:            &agent.PackDefinition{LeadSlug: "ceo"}, // deterministic lead; avoids reading global broker state
	}
}

func TestFinishHeadlessTurnWakesLeadWhenAllSpecialistsDone(t *testing.T) {
	woken := make(chan string, 4)
	oldWakeLead := headlessWakeLeadFn
	headlessWakeLeadFn = func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	}
	defer func() { headlessWakeLeadFn = oldWakeLead }()

	l := newHeadlessLauncherForTest()

	// Simulate "fe" finishing with no other specialists active.
	l.finishHeadlessTurn("fe")

	got := waitForString(t, woken)
	if got != "fe" {
		t.Fatalf("expected lead woken after fe finished, got %q", got)
	}
}

func TestFinishHeadlessTurnDoesNotWakeLeadWhenOtherSpecialistsActive(t *testing.T) {
	woken := make(chan string, 4)
	oldWakeLead := headlessWakeLeadFn
	headlessWakeLeadFn = func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	}
	defer func() { headlessWakeLeadFn = oldWakeLead }()

	l := newHeadlessLauncherForTest()
	// "be" is still active while "fe" finishes.
	l.headlessActive["be"] = &headlessCodexActiveTurn{}

	l.finishHeadlessTurn("fe")

	select {
	case got := <-woken:
		t.Fatalf("expected NO lead wake when other specialist still active, but got %q", got)
	case <-time.After(100 * time.Millisecond):
		// correct: lead not woken
	}
}

func TestFinishHeadlessTurnDoesNotWakeLeadWhenLeadFinishes(t *testing.T) {
	woken := make(chan string, 4)
	oldWakeLead := headlessWakeLeadFn
	headlessWakeLeadFn = func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	}
	defer func() { headlessWakeLeadFn = oldWakeLead }()

	l := newHeadlessLauncherForTest()
	// CEO finishes — should not self-wake.
	l.finishHeadlessTurn("ceo")

	select {
	case got := <-woken:
		t.Fatalf("expected NO lead wake when lead itself finishes, got %q", got)
	case <-time.After(100 * time.Millisecond):
		// correct: lead not self-woken
	}
}

func TestFinishHeadlessTurnDoesNotWakeLeadWhenLeadAlreadyQueued(t *testing.T) {
	woken := make(chan string, 4)
	oldWakeLead := headlessWakeLeadFn
	headlessWakeLeadFn = func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	}
	defer func() { headlessWakeLeadFn = oldWakeLead }()

	l := newHeadlessLauncherForTest()
	// CEO already has a pending turn.
	l.headlessQueues["ceo"] = []headlessCodexTurn{{Prompt: "pending work"}}

	l.finishHeadlessTurn("fe")

	select {
	case got := <-woken:
		t.Fatalf("expected NO lead wake when lead already has queued work, got %q", got)
	case <-time.After(100 * time.Millisecond):
		// correct: lead not woken again
	}
}

func TestWakeLeadAfterSpecialistFallsBackToCompletedTaskUpdateWhenNoBroadcast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldRunTurn := headlessCodexRunTurn
	notifications := make(chan string, 1)
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, slug, notification string, channel ...string) error {
		if slug == "ceo" {
			notifications <- notification
		}
		return nil
	}
	defer func() { headlessCodexRunTurn = oldRunTurn }()

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Lock the faceless YouTube niche",
		Owner:         "gtm",
		CreatedBy:     "ceo",
		TaskType:      "launch",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID != task.ID {
			continue
		}
		b.tasks[i].Status = "done"
		b.tasks[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.appendActionLocked("task_updated", "office", "general", "gtm", truncateSummary(b.tasks[i].Title+" ["+b.tasks[i].Status+"]", 140), task.ID)
		break
	}
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.provider = "codex"
	l.sessionName = "test"

	l.wakeLeadAfterSpecialist("gtm")

	got := waitForString(t, notifications)
	if !strings.Contains(got, "[Task updated #"+task.ID+" on #general]") {
		t.Fatalf("expected CEO notification for completed task handoff, got %q", got)
	}
	if !strings.Contains(got, "status done") {
		t.Fatalf("expected completed task status in CEO notification, got %q", got)
	}
}

func TestRecoverTimedOutHeadlessTurnBlocksTaskWithoutSubstantiveReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Research the best faceless wedge",
		Owner:         "cmo",
		CreatedBy:     "ceo",
		TaskType:      "research",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	if _, err := b.PostMessage("cmo", "general", "[STATUS] still researching", nil, task.ThreadID); err != nil {
		t.Fatalf("post status: %v", err)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.recoverTimedOutHeadlessTurn("cmo", headlessCodexTurn{TaskID: task.ID}, time.Now().UTC().Add(-2*time.Second), headlessCodexTurnTimeout)

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "blocked" || !updated.Blocked {
		t.Fatalf("expected task to be blocked after empty timeout, got %+v", updated)
	}
	if !strings.Contains(updated.Details, "timed out") {
		t.Fatalf("expected timeout detail appended, got %+v", updated)
	}
}

func TestRecoverTimedOutHeadlessTurnLeavesTaskRunningAfterSubstantiveReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Research the best faceless wedge",
		Owner:         "cmo",
		CreatedBy:     "ceo",
		TaskType:      "research",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	startedAt := time.Now().UTC().Add(-2 * time.Second)
	if _, err := b.PostMessage("cmo", "general", "Best wedge is a high-volume historical facts channel with sponsor ladder.", nil, task.ThreadID); err != nil {
		t.Fatalf("post substantive message: %v", err)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.recoverTimedOutHeadlessTurn("cmo", headlessCodexTurn{TaskID: task.ID}, startedAt, headlessCodexTurnTimeout)

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "in_progress" || updated.Blocked {
		t.Fatalf("expected task to remain active after substantive reply, got %+v", updated)
	}
}

func TestRecoverTimedOutHeadlessTurnRetriesLocalWorktreeOnceBeforeBlocking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the studio build",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.headlessWorkers["eng"] = true

	turn := headlessCodexTurn{
		Prompt:   "Build #task-" + strings.TrimPrefix(task.ID, "task-"),
		Channel:  "general",
		TaskID:   task.ID,
		Attempts: 0,
	}
	l.recoverTimedOutHeadlessTurn("eng", turn, time.Now().UTC().Add(-2*time.Second), headlessCodexLocalWorktreeTurnTimeout)

	if len(l.headlessQueues["eng"]) != 1 {
		t.Fatalf("expected one queued retry, got %+v", l.headlessQueues["eng"])
	}
	retry := l.headlessQueues["eng"][0]
	if retry.Attempts != 1 {
		t.Fatalf("expected retry attempt 1, got %+v", retry)
	}
	if !strings.Contains(retry.Prompt, "Previous attempt by @eng timed out") {
		t.Fatalf("expected retry prompt note, got %q", retry.Prompt)
	}

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "in_progress" || updated.Blocked {
		t.Fatalf("expected task to remain active during retry, got %+v", updated)
	}
}

func TestRecoverTimedOutLocalWorktreeRetriesEvenAfterSubstantiveReplyIfTaskStillActive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the studio build",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	startedAt := time.Now().UTC().Add(-2 * time.Second)
	b.messages = append(b.messages, channelMessage{
		ID:        "msg-test-eng-timeout",
		From:      "eng",
		Channel:   "general",
		Content:   "I found the right files and I am wiring the generator now.",
		ReplyTo:   task.ThreadID,
		Timestamp: startedAt.Add(time.Second).Format(time.RFC3339),
	})

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.headlessWorkers["eng"] = true

	l.recoverTimedOutHeadlessTurn("eng", headlessCodexTurn{
		Prompt:   "Build #task-" + strings.TrimPrefix(task.ID, "task-"),
		Channel:  "general",
		TaskID:   task.ID,
		Attempts: 0,
	}, startedAt, headlessCodexLocalWorktreeTurnTimeout)

	if len(l.headlessQueues["eng"]) != 1 {
		t.Fatalf("expected one queued retry, got %+v", l.headlessQueues["eng"])
	}

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "in_progress" || updated.Blocked {
		t.Fatalf("expected task to remain active during retry, got %+v", updated)
	}
}

func TestRecoverTimedOutLocalWorktreeLeavesReviewReadyTaskUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the studio build",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	for i := range b.tasks {
		if b.tasks[i].ID != task.ID {
			continue
		}
		b.tasks[i].Status = "review"
		b.tasks[i].ReviewState = "ready_for_review"
		b.tasks[i].Details = "Artifact shipped and awaiting review."
		b.tasks[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		break
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	l.recoverTimedOutHeadlessTurn("eng", headlessCodexTurn{
		Prompt:   "Build #task-" + strings.TrimPrefix(task.ID, "task-"),
		Channel:  "general",
		TaskID:   task.ID,
		Attempts: 0,
	}, time.Now().UTC().Add(-2*time.Second), headlessCodexLocalWorktreeTurnTimeout)

	if len(l.headlessQueues["eng"]) != 0 {
		t.Fatalf("expected no retry queue for review-ready task, got %+v", l.headlessQueues["eng"])
	}

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "review" || updated.ReviewState != "ready_for_review" {
		t.Fatalf("expected task to remain review-ready, got %+v", updated)
	}
}

func TestRecoverTimedOutHeadlessTurnBlocksLocalWorktreeAfterRetryExhausted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the studio build",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	l.recoverTimedOutHeadlessTurn("eng", headlessCodexTurn{
		Prompt:   "Ship #task-" + strings.TrimPrefix(task.ID, "task-"),
		Channel:  "general",
		TaskID:   task.ID,
		Attempts: headlessCodexLocalWorktreeRetryLimit,
	}, time.Now().UTC().Add(-2*time.Second), headlessCodexLocalWorktreeTurnTimeout)

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "blocked" || !updated.Blocked {
		t.Fatalf("expected task to be blocked after retry budget exhausted, got %+v", updated)
	}
}

func TestHeadlessCodexTurnTimeoutForLocalWorktreeTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the studio build",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	if got := l.headlessCodexTurnTimeoutForTurn(headlessCodexTurn{TaskID: task.ID}); got != headlessCodexLocalWorktreeTurnTimeout {
		t.Fatalf("expected local worktree timeout %s, got %s", headlessCodexLocalWorktreeTurnTimeout, got)
	}
	if got := l.headlessCodexStaleCancelAfterForTurn(headlessCodexTurn{TaskID: task.ID}); got != headlessCodexLocalWorktreeTurnTimeout {
		t.Fatalf("expected local worktree stale cancel threshold %s, got %s", headlessCodexLocalWorktreeTurnTimeout, got)
	}
}

func TestHeadlessCodexTurnTimeoutForOfficeLaunchTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Produce the launch assets and operating pack",
		Owner:         "gtm",
		CreatedBy:     "ceo",
		TaskType:      "launch",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	if got := l.headlessCodexTurnTimeoutForTurn(headlessCodexTurn{TaskID: task.ID}); got != headlessCodexOfficeLaunchTurnTimeout {
		t.Fatalf("expected office launch timeout %s, got %s", headlessCodexOfficeLaunchTurnTimeout, got)
	}
	if got := l.headlessCodexStaleCancelAfterForTurn(headlessCodexTurn{TaskID: task.ID}); got != headlessCodexOfficeLaunchTurnTimeout {
		t.Fatalf("expected office launch stale cancel threshold %s, got %s", headlessCodexOfficeLaunchTurnTimeout, got)
	}
}

func TestEnqueueHeadlessCodexTurnDefersLeadUntilSpecialistFinishes(t *testing.T) {
	oldRunTurn := headlessCodexRunTurn
	processed := make(chan string, 2)
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, _ string, notification string, channel ...string) error {
		processed <- notification
		return nil
	}
	defer func() { headlessCodexRunTurn = oldRunTurn }()

	l := newHeadlessLauncherForTest()
	l.headlessActive["eng"] = &headlessCodexActiveTurn{}

	l.enqueueHeadlessCodexTurn("ceo", "task-5 blocked after timeout")
	if l.headlessDeferredLead == nil {
		t.Fatal("expected lead work to be deferred while specialist is active")
	}

	l.finishHeadlessTurn("eng")

	if got := waitForString(t, processed); got != "task-5 blocked after timeout" {
		t.Fatalf("expected deferred lead notification to replay after specialist finished, got %q", got)
	}
}

func TestEnqueueHeadlessCodexTurnBypassesLeadHoldForReviewReadyTask(t *testing.T) {
	oldRunTurn := headlessCodexRunTurn
	processed := make(chan string, 1)
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, _ string, notification string, channel ...string) error {
		processed <- notification
		return nil
	}
	defer func() { headlessCodexRunTurn = oldRunTurn }()

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "youtube-factory",
		Title:         "Define channel thesis and monetization system",
		Owner:         "gtm",
		CreatedBy:     "ceo",
		TaskType:      "launch",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	b.mu.Lock()
	for i := range b.tasks {
		if b.tasks[i].ID != task.ID {
			continue
		}
		b.tasks[i].Status = "review"
		b.tasks[i].ReviewState = "ready_for_review"
		task = b.tasks[i]
		break
	}
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.headlessActive["eng"] = &headlessCodexActiveTurn{}

	action := officeActionLog{
		Kind:      "task_updated",
		Actor:     "gtm",
		Channel:   "youtube-factory",
		RelatedID: task.ID,
	}
	content := l.taskNotificationContent(action, task)
	packet := l.buildTaskExecutionPacket("ceo", action, task, content)

	l.enqueueHeadlessCodexTurn("ceo", packet)

	if l.headlessDeferredLead != nil {
		t.Fatal("expected review-ready task notification to bypass lead deferral")
	}
	got := waitForString(t, processed)
	if !strings.Contains(got, "#"+task.ID) {
		t.Fatalf("expected immediate lead packet for %s, got %q", task.ID, got)
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func waitForString(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for string")
		return ""
	}
}

func waitForProcessedTurn(t *testing.T, ch <-chan processedTurn) processedTurn {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for processed turn")
		return processedTurn{}
	}
}
