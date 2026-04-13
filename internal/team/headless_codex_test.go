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
	Env   []string `json:"env"`
	Stdin string   `json:"stdin"`
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
	if !containsEnv(record.Env, "WUPHF_AGENT_SLUG=ceo") {
		t.Fatalf("expected agent env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "WUPHF_HEADLESS_PROVIDER=codex") {
		t.Fatalf("expected headless provider env, got %#v", record.Env)
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

func containsArg(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func newHeadlessLauncherForTest() *Launcher {
	return &Launcher{
		headlessCtx:     context.Background(),
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
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
