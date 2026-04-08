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
	if !strings.Contains(joined, `WUPHF_AGENT_SLUG="pm"`) {
		t.Fatalf("expected agent slug in MCP env, got %q", joined)
	}
	if !strings.Contains(joined, `WUPHF_BROKER_TOKEN="`) {
		t.Fatalf("expected broker token in MCP env, got %q", joined)
	}
	if !strings.Contains(joined, `WUPHF_ONE_ON_ONE="1"`) || !strings.Contains(joined, `WUPHF_ONE_ON_ONE_AGENT="pm"`) {
		t.Fatalf("expected 1:1 env in MCP override, got %q", joined)
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
	t.Setenv("WUPHF_NO_NEX", "1")

	l := &Launcher{
		pack:        agent.GetPack("founding-team"),
		cwd:         t.TempDir(),
		broker:      NewBroker(),
		headlessCtx: context.Background(),
	}

	if err := l.runHeadlessCodexTurn("ceo", "You have new work in #launch."); err != nil {
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
	if !containsEnv(record.Env, "WUPHF_AGENT_SLUG=ceo") {
		t.Fatalf("expected agent env, got %#v", record.Env)
	}
	if !containsEnv(record.Env, "WUPHF_HEADLESS_PROVIDER=codex") {
		t.Fatalf("expected headless provider env, got %#v", record.Env)
	}
	if !containsEnvPrefix(record.Env, "WUPHF_BROKER_TOKEN=") {
		t.Fatalf("expected broker token env, got %#v", record.Env)
	}
	if !strings.Contains(record.Stdin, "<system>") || !strings.Contains(record.Stdin, "You have new work in #launch.") {
		t.Fatalf("expected notification prompt in stdin, got %q", record.Stdin)
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

	outputPath := ""
	for i := 0; i < len(codexArgs)-1; i++ {
		if codexArgs[i] == "--output-last-message" {
			outputPath = codexArgs[i+1]
			break
		}
	}
	if outputPath == "" {
		t.Fatalf("missing --output-last-message arg: %#v", codexArgs)
	}
	if err := os.WriteFile(outputPath, []byte("codex office reply"), 0o644); err != nil {
		t.Fatalf("write codex output: %v", err)
	}
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
