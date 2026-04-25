package team

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Pair HOME with WUPHF_RUNTIME_HOME so resetManifestToPack writes into
	// this test's tmpdir instead of the process-level leaked runtime home
	// installed by worktree_guard_test's init — that leak pollutes
	// downstream tests that read company.json via NewBroker.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
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

func TestNewLauncherAcceptsOperationBlueprintID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair HOME with WUPHF_RUNTIME_HOME so resetManifestToOperationBlueprint
	// writes into this test's tmpdir, not the process-level leaked runtime
	// home from worktree_guard_test's init.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_BROKER_TOKEN", "")
	if err := config.Save(config.Config{LLMProvider: "codex"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	l, err := NewLauncher("youtube-factory")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}
	if got, want := l.packSlug, "youtube-factory"; got != want {
		t.Fatalf("unexpected launcher blueprint id: got %q want %q", got, want)
	}
	if l.pack != nil {
		t.Fatalf("expected no static pack for operation blueprint launch, got %+v", l.pack)
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
	if !strings.Contains(joined, `mcp_servers.wuphf-office.env_vars=["WUPHF_AGENT_SLUG", "WUPHF_BROKER_TOKEN", "WUPHF_BROKER_BASE_URL", "WUPHF_NO_NEX", "WUPHF_ONE_ON_ONE", "WUPHF_ONE_ON_ONE_AGENT"]`) {
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
	t.Setenv("WUPHF_OPENAI_API_KEY", "openai-secret-key")
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
	if !strings.Contains(joinedArgs, "-a never") || !strings.Contains(joinedArgs, "-s workspace-write") {
		t.Fatalf("expected workspace-write sandbox for office turn, got %#v", record.Args)
	}
	if strings.Contains(joinedArgs, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("did not expect dangerous bypass for office turn, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, "--disable plugins") {
		t.Fatalf("expected plugins feature to be disabled, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, `mcp_servers.wuphf-office.command="/tmp/wuphf"`) {
		t.Fatalf("expected office MCP override, got %#v", record.Args)
	}
	if !strings.Contains(joinedArgs, `mcp_servers.wuphf-office.env_vars=["WUPHF_AGENT_SLUG", "WUPHF_BROKER_TOKEN", "WUPHF_BROKER_BASE_URL", "ONE_SECRET", "ONE_IDENTITY", "ONE_IDENTITY_TYPE"]`) {
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
	wantCodexHome := filepath.Join(os.Getenv("HOME"), ".wuphf", "codex-headless")
	if !containsEnv(record.Env, "HOME="+wantCodexHome) {
		t.Fatalf("expected isolated HOME env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "CODEX_HOME="+wantCodexHome) {
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
	if !containsEnv(record.Env, "WUPHF_OPENAI_API_KEY=openai-secret-key") || !containsEnv(record.Env, "OPENAI_API_KEY=openai-secret-key") {
		t.Fatalf("expected openai API env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "ONE_SECRET=one-secret-value") {
		t.Fatalf("expected one secret env, got %#v", record.Env)
	}
	if strings.Contains(joinedArgs, l.broker.Token()) || strings.Contains(joinedArgs, "nex-secret-key") || strings.Contains(joinedArgs, "openai-secret-key") || strings.Contains(joinedArgs, "one-secret-value") {
		t.Fatalf("expected secret values to stay out of args, got %#v", record.Args)
	}
	if !strings.Contains(record.Stdin, "<system>") || !strings.Contains(record.Stdin, "You have new work in #launch.") {
		t.Fatalf("expected notification prompt in stdin, got %q", record.Stdin)
	}
	if got := l.broker.usage.Agents["ceo"].TotalTokens; got != 174 {
		t.Fatalf("expected recorded codex usage total 174, got %d", got)
	}
	if got := l.broker.usage.Agents["ceo"].InputTokens; got != 123 {
		t.Fatalf("expected recorded input tokens 123, got %d", got)
	}
	if got := l.broker.usage.Agents["ceo"].CacheReadTokens; got != 45 {
		t.Fatalf("expected recorded cached input tokens 45, got %d", got)
	}
	if got := l.broker.usage.Agents["ceo"].OutputTokens; got != 6 {
		t.Fatalf("expected recorded output tokens 6, got %d", got)
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
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PWD", repoRoot)
	t.Setenv("OLDPWD", "/tmp/previous")
	t.Setenv("CODEX_THREAD_ID", "thread-from-controller")
	t.Setenv("CODEX_TUI_RECORD_SESSION", "1")
	t.Setenv("CODEX_TUI_SESSION_LOG_PATH", "/tmp/controller-session.jsonl")

	broker := NewBroker()
	ensureTestMemberAccess(broker, "general", "builder", "Builder")
	ensureTestMemberAccess(broker, "general", "operator", "Operator")
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
	if !strings.Contains(joinedArgs, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected dangerous bypass for local worktree turn, got %#v", record.Args)
	}
	if strings.Contains(joinedArgs, "-s workspace-write") {
		t.Fatalf("did not expect workspace-write sandbox for local worktree turn, got %#v", record.Args)
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
	wantCodexHome := filepath.Join(os.Getenv("HOME"), ".wuphf", "codex-headless")
	if !containsEnv(record.Env, "HOME="+wantCodexHome) {
		t.Fatalf("expected isolated HOME env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "CODEX_HOME="+wantCodexHome) {
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

func TestRunHeadlessCodexTurnUsesAssignedWorktreeForLocalWorktreeBuilder(t *testing.T) {
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
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PWD", repoRoot)

	broker := NewBroker()
	ensureTestMemberAccess(broker, "general", "builder", "Builder")
	task, _, err := broker.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Build the dry-run intake packet",
		Details:       "Implement in the assigned worktree.",
		Owner:         "builder",
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

	if err := l.runHeadlessCodexTurn(context.Background(), "builder", "Ship the intake packet."); err != nil {
		t.Fatalf("runHeadlessCodexTurn: %v", err)
	}

	record := readHeadlessCodexRecord(t, recordFile)
	joinedArgs := strings.Join(record.Args, " ")
	if got := argValue(record.Args, "-C"); !samePath(got, worktreeDir) {
		t.Fatalf("expected codex worktree %q, got %q", worktreeDir, got)
	}
	if !strings.Contains(joinedArgs, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected dangerous bypass for local worktree turn, got %#v", record.Args)
	}
	if !samePath(record.Dir, worktreeDir) {
		t.Fatalf("expected command dir %q, got %q", worktreeDir, record.Dir)
	}
	if got := envValue(record.Env, "WUPHF_WORKTREE_PATH"); !samePath(got, worktreeDir) {
		t.Fatalf("expected worktree env, got %#v", record.Env)
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
	t.Setenv("OPENAI_API_KEY", "test-key")
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
	sourceHome := t.TempDir()
	runtimeHome := t.TempDir()
	t.Setenv("HOME", runtimeHome)
	t.Setenv("WUPHF_GLOBAL_HOME", sourceHome)

	sourceCodexHome := filepath.Join(sourceHome, ".codex")
	if err := os.MkdirAll(sourceCodexHome, 0o755); err != nil {
		t.Fatalf("MkdirAll source home: %v", err)
	}
	wantAuth := []byte(`{"access_token":"test-token"}`)
	if err := os.WriteFile(filepath.Join(sourceCodexHome, "auth.json"), wantAuth, 0o600); err != nil {
		t.Fatalf("write source auth: %v", err)
	}
	oneDir := filepath.Join(sourceHome, ".one")
	if err := os.MkdirAll(oneDir, 0o755); err != nil {
		t.Fatalf("MkdirAll one dir: %v", err)
	}
	wantOneConfig := []byte(`{"session":"one-test"}`)
	if err := os.WriteFile(filepath.Join(oneDir, "config.json"), wantOneConfig, 0o600); err != nil {
		t.Fatalf("write source one config: %v", err)
	}
	wantOneUpdate := []byte(`{"last_check":"2026-04-15T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(oneDir, "update-check.json"), wantOneUpdate, 0o600); err != nil {
		t.Fatalf("write source one update check: %v", err)
	}

	got := prepareHeadlessCodexHome()
	want := filepath.Join(runtimeHome, ".wuphf", "codex-headless")
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
	oneConfigCopy, err := os.ReadFile(filepath.Join(want, ".one", "config.json"))
	if err != nil {
		t.Fatalf("read copied one config: %v", err)
	}
	if string(oneConfigCopy) != string(wantOneConfig) {
		t.Fatalf("expected copied one config %q, got %q", string(wantOneConfig), string(oneConfigCopy))
	}
	oneUpdateCopy, err := os.ReadFile(filepath.Join(want, ".one", "update-check.json"))
	if err != nil {
		t.Fatalf("read copied one update check: %v", err)
	}
	if string(oneUpdateCopy) != string(wantOneUpdate) {
		t.Fatalf("expected copied one update check %q, got %q", string(wantOneUpdate), string(oneUpdateCopy))
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

func TestPostHeadlessFinalMessageIfSilentPostsFinalOutput(t *testing.T) {
	// Isolate state from the user's real ~/.wuphf/team/broker-state.json.
	// Without isolation NewBroker could still pick up state from a shared
	// ~/.wuphf/team/broker-state.json, and agentPostedSubstantiveMessageToChannelSince
	// could observe an unrelated ceo message, making the "expected posted=true"
	// assertion fail non-deterministically depending on machine history.
	b := newTestBroker(t)
	channel := DMSlugFor("ceo")
	root, err := b.PostMessage("you", channel, "Ping the CEO.", nil, "")
	if err != nil {
		t.Fatalf("post human message: %v", err)
	}
	l := &Launcher{broker: b}
	startedAt := time.Now().UTC().Add(-1 * time.Second)

	msg, posted, err := l.postHeadlessFinalMessageIfSilent(
		"ceo",
		"dm-human-ceo",
		fmt.Sprintf(`Reply using team_broadcast with reply_to_id "%s".`, root.ID),
		"REAL_AGENT_TYPING_OK",
		startedAt,
	)
	if err != nil {
		t.Fatalf("fallback post: %v", err)
	}
	if !posted {
		t.Fatal("expected final output fallback to post")
	}
	if msg.From != "ceo" || msg.Channel != channel || msg.Content != "REAL_AGENT_TYPING_OK" || msg.ReplyTo != root.ID {
		t.Fatalf("unexpected fallback message: %+v", msg)
	}

	_, posted, err = l.postHeadlessFinalMessageIfSilent("ceo", channel, "", "duplicate", startedAt)
	if err != nil {
		t.Fatalf("second fallback post: %v", err)
	}
	if posted {
		t.Fatal("expected fallback to skip when the agent already posted to the target channel")
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
	oldMinAge := headlessCodexMinTurnAgeBeforeCancel
	headlessCodexTurnTimeout = 5 * time.Second
	headlessCodexStaleCancelAfter = 20 * time.Millisecond
	// The min-age floor exists to prevent cancel-storms in prod; for this
	// test it must be smaller than the stale threshold so the
	// "cancel-old, process-new" behaviour can be exercised in milliseconds.
	headlessCodexMinTurnAgeBeforeCancel = 10 * time.Millisecond
	defer func() {
		headlessCodexRunTurn = oldRunTurn
		headlessCodexTurnTimeout = oldTimeout
		headlessCodexStaleCancelAfter = oldStale
		headlessCodexMinTurnAgeBeforeCancel = oldMinAge
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
	_, _ = os.Stdout.WriteString("{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":123,\"cached_input_tokens\":45,\"output_tokens\":6}}\n")
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
	setHeadlessWakeLeadFn(t, func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	})

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
	setHeadlessWakeLeadFn(t, func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	})

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
	setHeadlessWakeLeadFn(t, func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	})

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
	setHeadlessWakeLeadFn(t, func(_ *Launcher, specialistSlug string) {
		woken <- specialistSlug
	})

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

func TestEnqueueHeadlessCodexTurnRecordDropsDuplicateLeadTaskWhileActive(t *testing.T) {
	l := newHeadlessLauncherForTest()
	l.pack = &agent.PackDefinition{LeadSlug: "ceo"}
	l.headlessActive["ceo"] = &headlessCodexActiveTurn{
		Turn: headlessCodexTurn{
			Prompt: "first prompt about #task-3",
			TaskID: "task-3",
		},
		StartedAt: time.Now(),
	}

	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{
		Prompt:     "second prompt about #task-3",
		TaskID:     "task-3",
		EnqueuedAt: time.Now(),
	})

	if got := len(l.headlessQueues["ceo"]); got != 0 {
		t.Fatalf("expected no queued duplicate lead turn for same task, got %d", got)
	}
}

func TestEnqueueHeadlessCodexTurnRecordQueuesUrgentLeadWakeForSameTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Review and advance the proof lane",
		Owner:         "builder",
		CreatedBy:     "ceo",
		TaskType:      "follow_up",
		ExecutionMode: "office",
		ReviewState:   "ready_for_review",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	for i := range b.tasks {
		if b.tasks[i].ID == task.ID {
			b.tasks[i].Status = "review"
			b.tasks[i].ReviewState = "ready_for_review"
			break
		}
	}

	cancelled := false
	l := newHeadlessLauncherForTest()
	l.pack = &agent.PackDefinition{LeadSlug: "ceo"}
	l.broker = b
	l.headlessWorkers["ceo"] = true
	l.headlessActive["ceo"] = &headlessCodexActiveTurn{
		Turn: headlessCodexTurn{
			Prompt: "first prompt about #" + task.ID,
			TaskID: task.ID,
		},
		StartedAt: time.Now().Add(-2 * time.Minute),
		Cancel: func() {
			cancelled = true
		},
	}

	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{
		Prompt:     "specialist handoff about #" + task.ID,
		TaskID:     task.ID,
		EnqueuedAt: time.Now(),
	})

	if got := len(l.headlessQueues["ceo"]); got != 1 {
		t.Fatalf("expected urgent lead wake to queue behind same task, got %d", got)
	}
	if !cancelled {
		t.Fatal("expected stale active lead turn to be cancelled for urgent same-task wake")
	}
}

func TestEnqueueHeadlessCodexTurnRecordDropsDuplicateAgentTaskWhileActive(t *testing.T) {
	l := newHeadlessLauncherForTest()
	l.pack = &agent.PackDefinition{LeadSlug: "ceo"}
	l.headlessActive["eng"] = &headlessCodexActiveTurn{
		Turn: headlessCodexTurn{
			Prompt: "first prompt about #task-11",
			TaskID: "task-11",
		},
		StartedAt: time.Now(),
	}

	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{
		Prompt:     "second prompt about #task-11",
		TaskID:     "task-11",
		EnqueuedAt: time.Now(),
	})

	if got := len(l.headlessQueues["eng"]); got != 0 {
		t.Fatalf("expected no queued duplicate agent turn for same task, got %d", got)
	}
}

func TestEnqueueHeadlessCodexTurnRecordReplacesPendingAgentTaskTurn(t *testing.T) {
	l := newHeadlessLauncherForTest()
	l.pack = &agent.PackDefinition{LeadSlug: "ceo"}
	l.headlessWorkers["eng"] = true
	l.headlessQueues["eng"] = []headlessCodexTurn{{
		Prompt:     "older prompt about #task-11",
		Channel:    "youtube-factory",
		TaskID:     "task-11",
		EnqueuedAt: time.Now().Add(-time.Minute),
	}}

	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{
		Prompt:     "newer prompt about #task-11",
		Channel:    "youtube-factory",
		TaskID:     "task-11",
		EnqueuedAt: time.Now(),
	})

	queue := l.headlessQueues["eng"]
	if got := len(queue); got != 1 {
		t.Fatalf("expected single queued agent turn for same task, got %d", got)
	}
	if got := queue[0].Prompt; got != "newer prompt about #task-11" {
		t.Fatalf("expected queued agent turn to be replaced, got %q", got)
	}
}

func TestEnqueueHeadlessCodexTurnRecordAllowsRetryBehindActiveAgentTask(t *testing.T) {
	l := newHeadlessLauncherForTest()
	l.pack = &agent.PackDefinition{LeadSlug: "ceo"}
	l.headlessWorkers["eng"] = true
	l.headlessActive["eng"] = &headlessCodexActiveTurn{
		Turn: headlessCodexTurn{
			Prompt:   "first prompt about #task-11",
			TaskID:   "task-11",
			Attempts: 0,
		},
		StartedAt: time.Now(),
	}

	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{
		Prompt:     "retry prompt about #task-11",
		Channel:    "youtube-factory",
		TaskID:     "task-11",
		Attempts:   1,
		EnqueuedAt: time.Now(),
	})

	queue := l.headlessQueues["eng"]
	if got := len(queue); got != 1 {
		t.Fatalf("expected single queued retry turn for same task, got %d", got)
	}
	if got := queue[0].Prompt; got != "retry prompt about #task-11" {
		t.Fatalf("expected retry turn to be queued, got %q", got)
	}
	if got := queue[0].Attempts; got != 1 {
		t.Fatalf("expected retry attempt to be preserved, got %d", got)
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
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	ensureTestMemberAccess(b, "general", "operator", "Operator")
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
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "cmo", Name: "Chief Marketing Officer"})
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "cmo")
			break
		}
	}
	b.mu.Unlock()
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
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "cmo", Name: "Chief Marketing Officer"})
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "cmo")
			break
		}
	}
	b.mu.Unlock()
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
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	ensureTestMemberAccess(b, "general", "operator", "Operator")
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

func TestRecoverFailedHeadlessTurnRetriesLocalWorktreeOnceBeforeBlocking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement queue mode for the YouTube factory",
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
	l.recoverFailedHeadlessTurn("eng", turn, time.Now().UTC().Add(-2*time.Second), "Selected model is at capacity. Please try a different model.")

	if len(l.headlessQueues["eng"]) != 1 {
		t.Fatalf("expected one queued retry, got %+v", l.headlessQueues["eng"])
	}
	retry := l.headlessQueues["eng"][0]
	if retry.Attempts != 1 {
		t.Fatalf("expected retry attempt 1, got %+v", retry)
	}
	if !strings.Contains(retry.Prompt, "Previous attempt by @eng failed") {
		t.Fatalf("expected retry prompt note, got %q", retry.Prompt)
	}
	if !strings.Contains(retry.Prompt, "Selected model is at capacity") {
		t.Fatalf("expected retry prompt to carry failure detail, got %q", retry.Prompt)
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

func TestRecoverFailedHeadlessTurnBlocksLocalWorktreeAfterRetryExhausted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement queue mode for the YouTube factory",
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

	l.recoverFailedHeadlessTurn("eng", headlessCodexTurn{
		Prompt:   "Ship #task-" + strings.TrimPrefix(task.ID, "task-"),
		Channel:  "general",
		TaskID:   task.ID,
		Attempts: headlessCodexLocalWorktreeRetryLimit,
	}, time.Now().UTC().Add(-2*time.Second), "Selected model is at capacity. Please try a different model.")

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
	if !strings.Contains(updated.Details, "Selected model is at capacity") {
		t.Fatalf("expected failure detail appended, got %+v", updated)
	}
}

func TestRecoverFailedHeadlessTurnRequeuesExternalActionBeforeBlocking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Send a live Slack kickoff update and pivot to Notion if needed",
		Details:       "Use the connected Slack target first. If it fails, pivot to the smallest useful live Notion action.",
		Owner:         "operator",
		CreatedBy:     "ceo",
		TaskType:      "follow_up",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	l.recoverFailedHeadlessTurn("operator", headlessCodexTurn{
		Prompt:   "Send #task-" + strings.TrimPrefix(task.ID, "task-"),
		Channel:  "general",
		TaskID:   task.ID,
		Attempts: 0,
	}, time.Now().UTC().Add(-2*time.Second), "channel_not_found")

	queue := l.headlessQueues["operator"]
	if len(queue) != 1 {
		t.Fatalf("expected one retry queued for external action, got %+v", queue)
	}
	if queue[0].Attempts != 1 {
		t.Fatalf("expected retry attempt count 1, got %+v", queue[0])
	}
	if !strings.Contains(queue[0].Prompt, "live external-action task") {
		t.Fatalf("expected external recovery prompt, got %q", queue[0].Prompt)
	}
	if !strings.Contains(queue[0].Prompt, "smallest useful live Notion or Drive action") {
		t.Fatalf("expected pivot guidance in retry prompt, got %q", queue[0].Prompt)
	}
}

func TestHeadlessTurnCompletedDurablyRejectsCodingTurnWithoutTaskStateOrEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldSnapshot := headlessCodexWorkspaceStatusSnapshot
	headlessCodexWorkspaceStatusSnapshot = func(string) string {
		return "after-change"
	}
	defer func() { headlessCodexWorkspaceStatusSnapshot = oldSnapshot }()

	// Build the task state directly instead of going through
	// EnsurePlannedTask so we never call saveLocked — the broker-state
	// save path can race with leaked goroutines from earlier tests
	// (rename .tmp -> final fails mid-flight). We don't need persistence
	// here; we only need the task fields that headlessTurnCompletedDurably
	// reads.
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{{
		ID:            "task-1",
		Channel:       "general",
		Title:         "Implement the durable turn guard",
		Owner:         "eng",
		Status:        "open",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	}}
	task := b.tasks[0]
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b

	ok, reason := l.headlessTurnCompletedDurably("eng", &headlessCodexActiveTurn{
		Turn: headlessCodexTurn{
			TaskID: task.ID,
		},
		StartedAt:         time.Now().UTC().Add(-2 * time.Second),
		WorkspaceDir:      t.TempDir(),
		WorkspaceSnapshot: "before-change",
	})
	if ok {
		t.Fatal("expected coding turn without task closure or evidence to be rejected")
	}
	if !strings.Contains(reason, "without durable task state or completion evidence") && !strings.Contains(reason, "changed workspace") {
		t.Fatalf("expected durable completion failure reason, got %q", reason)
	}
}

func TestHeadlessTurnCompletedDurablyAcceptsReviewReadyTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldSnapshot := headlessCodexWorkspaceStatusSnapshot
	headlessCodexWorkspaceStatusSnapshot = func(string) string {
		return "after-change"
	}
	defer func() { headlessCodexWorkspaceStatusSnapshot = oldSnapshot }()

	// See TestHeadlessTurnCompletedDurablyRejectsCodingTurn... for why
	// we build the task state directly rather than via EnsurePlannedTask.
	b := NewBroker()
	b.mu.Lock()
	b.tasks = []teamTask{{
		ID:            "task-1",
		Channel:       "general",
		Title:         "Implement the durable turn guard",
		Owner:         "eng",
		Status:        "review",
		ReviewState:   "ready_for_review",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	}}
	task := b.tasks[0]
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b

	ok, reason := l.headlessTurnCompletedDurably("eng", &headlessCodexActiveTurn{
		Turn: headlessCodexTurn{
			TaskID: task.ID,
		},
		StartedAt:         time.Now().UTC().Add(-2 * time.Second),
		WorkspaceDir:      t.TempDir(),
		WorkspaceSnapshot: "before-change",
	})
	if !ok {
		t.Fatalf("expected review-ready task to satisfy durable completion, got %q", reason)
	}
}

func TestHeadlessTurnCompletedDurablyRejectsLocalWorktreeBuilderWithoutTaskStateOrEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldSnapshot := headlessCodexWorkspaceStatusSnapshot
	headlessCodexWorkspaceStatusSnapshot = func(string) string {
		return "after-change"
	}
	defer func() { headlessCodexWorkspaceStatusSnapshot = oldSnapshot }()

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Build the dry-run intake packet",
		Owner:         "builder",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	ok, reason := l.headlessTurnCompletedDurably("builder", &headlessCodexActiveTurn{
		Turn: headlessCodexTurn{
			TaskID: task.ID,
		},
		StartedAt:         time.Now().UTC().Add(-2 * time.Second),
		WorkspaceDir:      t.TempDir(),
		WorkspaceSnapshot: "before-change",
	})
	if ok {
		t.Fatal("expected local_worktree builder turn without task closure or evidence to be rejected")
	}
	if !strings.Contains(reason, "without durable task state or completion evidence") && !strings.Contains(reason, "changed workspace") {
		t.Fatalf("expected durable completion failure reason, got %q", reason)
	}
}

func TestHeadlessTurnCompletedDurablyRejectsExternalCompletionWithoutWorkflowEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:     "general",
		Title:       "Create one new Notion client workspace page for the consulting engagement",
		Details:     "Use the connected Notion workspace and leave the new client-facing page link in channel.",
		Owner:       "builder",
		CreatedBy:   "ceo",
		TaskType:    "follow_up",
		ReviewState: "ready_for_review",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	for i := range b.tasks {
		if b.tasks[i].ID == task.ID {
			b.tasks[i].Status = "review"
			b.tasks[i].ReviewState = "ready_for_review"
			break
		}
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	ok, reason := l.headlessTurnCompletedDurably("builder", &headlessCodexActiveTurn{
		Turn:      headlessCodexTurn{TaskID: task.ID},
		StartedAt: time.Now().UTC().Add(-2 * time.Second),
	})
	if ok {
		t.Fatal("expected external completion without workflow evidence to be rejected")
	}
	if !strings.Contains(reason, "without recorded external execution evidence") {
		t.Fatalf("expected external evidence failure reason, got %q", reason)
	}
}

func TestHeadlessTurnCompletedDurablyAcceptsExternalCompletionWithWorkflowEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:     "general",
		Title:       "Create one new Notion client workspace page for the consulting engagement",
		Details:     "Use the connected Notion workspace and leave the new client-facing page link in channel.",
		Owner:       "builder",
		CreatedBy:   "ceo",
		TaskType:    "follow_up",
		ReviewState: "ready_for_review",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	for i := range b.tasks {
		if b.tasks[i].ID == task.ID {
			b.tasks[i].Status = "review"
			b.tasks[i].ReviewState = "ready_for_review"
			break
		}
	}
	if err := b.RecordAction("external_workflow_executed", "notion", "general", "builder", "Created client workspace page in Notion", "workflow-notion-client-page", nil, ""); err != nil {
		t.Fatalf("record action: %v", err)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	ok, reason := l.headlessTurnCompletedDurably("builder", &headlessCodexActiveTurn{
		Turn:      headlessCodexTurn{TaskID: task.ID},
		StartedAt: time.Now().UTC().Add(-2 * time.Second),
	})
	if !ok {
		t.Fatalf("expected external completion with workflow evidence to be accepted, got %q", reason)
	}
}

func TestHeadlessTurnCompletedDurablyAcceptsExternalCompletionWithActionEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:     "general",
		Title:       "Verify the new Notion client workspace page for the consulting engagement",
		Details:     "Use the connected Notion workspace and confirm the client-facing page is live.",
		Owner:       "reviewer",
		CreatedBy:   "ceo",
		TaskType:    "follow_up",
		ReviewState: "not_required",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	for i := range b.tasks {
		if b.tasks[i].ID == task.ID {
			b.tasks[i].Status = "done"
			b.tasks[i].ReviewState = "not_required"
			break
		}
	}
	if err := b.RecordAction("external_action_executed", "one", "general", "reviewer", "Verified client workspace page in Notion", "notion-client-page", nil, ""); err != nil {
		t.Fatalf("record action: %v", err)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b

	ok, reason := l.headlessTurnCompletedDurably("reviewer", &headlessCodexActiveTurn{
		Turn:      headlessCodexTurn{TaskID: task.ID},
		StartedAt: time.Now().UTC().Add(-2 * time.Second),
	})
	if !ok {
		t.Fatalf("expected external completion with action evidence to be accepted, got %q", reason)
	}
}

func TestBeginHeadlessCodexTurnCapturesWorktreeForLocalWorktreeBuilder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	worktreeDir := t.TempDir()
	oldPrepareTaskWorktree := prepareTaskWorktree
	oldSnapshot := headlessCodexWorkspaceStatusSnapshot
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return worktreeDir, worktreeBranchName(taskID), nil
	}
	headlessCodexWorkspaceStatusSnapshot = func(path string) string {
		if !samePath(path, worktreeDir) {
			t.Fatalf("expected workspace snapshot to target %q, got %q", worktreeDir, path)
		}
		return "snapshot"
	}
	defer func() {
		prepareTaskWorktree = oldPrepareTaskWorktree
		headlessCodexWorkspaceStatusSnapshot = oldSnapshot
	}()

	b := NewBroker()
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Build the dry-run intake packet",
		Owner:         "builder",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.headlessQueues["builder"] = []headlessCodexTurn{{TaskID: task.ID}}

	_, _, _, _, ok := l.beginHeadlessCodexTurn("builder")
	if !ok {
		t.Fatal("expected queued builder turn to begin")
	}
	active := l.headlessActive["builder"]
	if active == nil {
		t.Fatal("expected active builder turn")
	}
	if !samePath(active.WorkspaceDir, worktreeDir) {
		t.Fatalf("expected builder workspace %q, got %q", worktreeDir, active.WorkspaceDir)
	}
	if active.WorkspaceSnapshot != "snapshot" {
		t.Fatalf("expected workspace snapshot to be recorded, got %q", active.WorkspaceSnapshot)
	}
}

func TestRunHeadlessCodexQueueRetriesLocalWorktreeAfterGenericError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()

	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return filepath.Join(tmpDir, "wuphf-task-"+taskID), "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(string, string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	setHeadlessWakeLeadFn(t, func(_ *Launcher, _ string) {})

	b := NewBrokerAt(filepath.Join(tmpDir, "broker-state.json"))
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement queue mode for the YouTube factory",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	oldRunTurn := headlessCodexRunTurn
	defer func() { headlessCodexRunTurn = oldRunTurn }()

	processed := make(chan string, 2)
	attempt := 0
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, _ string, notification string, channel ...string) error {
		attempt++
		processed <- notification
		if attempt == 1 {
			return fmt.Errorf("Selected model is at capacity. Please try a different model.")
		}
		return nil
	}

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.headlessWorkers["eng"] = true
	l.headlessQueues["eng"] = []headlessCodexTurn{{
		Prompt:     "Build #task-" + strings.TrimPrefix(task.ID, "task-"),
		Channel:    "general",
		TaskID:     task.ID,
		Attempts:   0,
		EnqueuedAt: time.Now(),
	}}

	done := make(chan struct{})
	go func() {
		l.runHeadlessCodexQueue("eng")
		close(done)
	}()

	first := waitForString(t, processed)
	second := waitForString(t, processed)
	if first == second {
		t.Fatalf("expected retry prompt to differ from the original prompt, got %q", first)
	}
	if !strings.Contains(second, "Previous attempt by @eng failed") {
		t.Fatalf("expected retry prompt note, got %q", second)
	}
	if !strings.Contains(second, "Selected model is at capacity") {
		t.Fatalf("expected retry prompt to include provider failure, got %q", second)
	}
	waitForSignal(t, done)
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

	stateDir := t.TempDir()

	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return filepath.Join(stateDir, "wuphf-task-"+taskID), "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(string, string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	b := NewBrokerAt(filepath.Join(stateDir, "broker-state.json"))
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
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
		Channel:   "general",
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

func TestPreflightHeadlessCodexAuthFailsAndPostsSystemMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if err := config.Save(config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	broker := NewBroker()
	l := &Launcher{broker: broker}

	err := l.preflightHeadlessCodexAuth("operator", "general")
	if err == nil {
		t.Fatal("expected preflight to fail with no auth available")
	}
	if !strings.Contains(err.Error(), "codex auth missing") {
		t.Fatalf("expected 'codex auth missing' in error, got %v", err)
	}

	// The channel should now contain a system message naming the agent and
	// the remediation the user needs to take. Without this the user sees
	// nothing but "Routing..." forever.
	messages := broker.ChannelMessages("general")
	if len(messages) == 0 {
		t.Fatal("expected a system message in general, got none")
	}
	found := false
	for _, m := range messages {
		if m.From == "system" && strings.Contains(m.Content, "@operator") && strings.Contains(m.Content, "codex login") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a system message mentioning @operator and 'codex login'; got %#v", messages)
	}
}

func TestPreflightHeadlessCodexAuthPassesWhenOpenAIKeySet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "sk-test-key")
	if err := config.Save(config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	l := &Launcher{broker: NewBroker()}
	if err := l.preflightHeadlessCodexAuth("operator", "general"); err != nil {
		t.Fatalf("expected preflight to pass with OPENAI_API_KEY set, got %v", err)
	}
}

func TestPreflightHeadlessCodexAuthPassesWhenAuthJSONPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if err := config.Save(config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	// Seed auth.json at the source path prepareHeadlessCodexHome reads from
	// (~/.codex/auth.json). It will copy it into the isolated runtime home.
	srcDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "auth.json"), []byte(`{"auth_mode":"chatgpt"}`), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}

	l := &Launcher{broker: NewBroker()}
	if err := l.preflightHeadlessCodexAuth("operator", "general"); err != nil {
		t.Fatalf("expected preflight to pass when auth.json exists, got %v", err)
	}
}

func TestIsCodexAuthError(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"exit status 1", false},
		{"unexpected status 401 Unauthorized", true},
		{"401 Unauthorized", true},
		{"Missing bearer or basic authentication", true},
		{"random network error", false},
	}
	for _, c := range cases {
		if got := isCodexAuthError(c.in); got != c.want {
			t.Errorf("isCodexAuthError(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
