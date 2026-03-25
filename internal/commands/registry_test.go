package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSlashInput(t *testing.T) {
	tests := []struct {
		input     string
		wantName  string
		wantArgs  string
		wantSlash bool
	}{
		{"/ask hello world", "ask", "hello world", true},
		{"/quit", "quit", "", true},
		{"/search  spaces  ", "search", "spaces", true},
		{"hello world", "", "", false},
		{"", "", "", false},
		{"/", "", "", true},
	}
	for _, tt := range tests {
		name, args, isSlash := ParseSlashInput(tt.input)
		if name != tt.wantName || args != tt.wantArgs || isSlash != tt.wantSlash {
			t.Errorf("ParseSlashInput(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.input, name, args, isSlash,
				tt.wantName, tt.wantArgs, tt.wantSlash)
		}
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(SlashCommand{Name: "foo", Description: "Foo command"})

	cmd, ok := r.Get("foo")
	if !ok {
		t.Fatal("expected to find command 'foo'")
	}
	if cmd.Name != "foo" {
		t.Errorf("expected Name='foo', got %q", cmd.Name)
	}

	_, ok = r.Get("bar")
	if ok {
		t.Error("expected 'bar' to not be found")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(SlashCommand{Name: "zebra"})
	r.Register(SlashCommand{Name: "alpha"})
	r.Register(SlashCommand{Name: "middle"})

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "middle" || list[2].Name != "zebra" {
		t.Errorf("list not sorted: %v", list)
	}
}

func TestRegisterAllCommands(t *testing.T) {
	r := NewRegistry()
	RegisterAllCommands(r)

	expected := []string{
		"ask", "search", "remember",
		"object", "record", "note", "task", "list", "rel", "attribute",
		"agent", "graph", "insights", "calendar", "chat",
		"config", "detect", "init", "provider",
		"help", "clear", "quit",
	}
	for _, name := range expected {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected command %q to be registered", name)
		}
	}
}

func TestCmdQuitReturnsErrQuit(t *testing.T) {
	r := NewRegistry()
	RegisterAllCommands(r)

	cmd, ok := r.Get("quit")
	if !ok {
		t.Fatal("quit not registered")
	}
	if cmd.Execute == nil {
		t.Fatal("quit has no Execute")
	}

	var messages []string
	ctx := &SlashContext{
		AddMessage: func(role, content string) { messages = append(messages, content) },
		SetLoading: func(bool) {},
		SendResult: func(string, error) {},
	}
	err := cmd.Execute(ctx, "")
	if !errors.Is(err, ErrQuit) {
		t.Errorf("expected ErrQuit, got %v", err)
	}
}

func TestCmdHelp(t *testing.T) {
	r := NewRegistry()
	RegisterAllCommands(r)

	cmd, _ := r.Get("help")
	var output string
	ctx := &SlashContext{
		AddMessage: func(role, content string) { output += content },
		SetLoading: func(bool) {},
		SendResult: func(string, error) {},
	}
	if err := cmd.Execute(ctx, ""); err != nil {
		t.Fatalf("help returned error: %v", err)
	}
	if output == "" {
		t.Error("help produced no output")
	}
}

func TestCmdAgentsNoService(t *testing.T) {
	r := NewRegistry()
	RegisterAllCommands(r)

	cmd, _ := r.Get("agent")
	var output string
	ctx := &SlashContext{
		AgentService: nil,
		AddMessage:   func(role, content string) { output += content },
		SetLoading:   func(bool) {},
		SendResult:   func(string, error) {},
	}
	if err := cmd.Execute(ctx, ""); err != nil {
		t.Fatalf("agents returned error: %v", err)
	}
	if output == "" {
		t.Error("agents produced no output for nil service")
	}
}

func TestDispatchQuit(t *testing.T) {
	result := Dispatch("/quit", "", "text", 0)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestDispatchUnknown(t *testing.T) {
	result := Dispatch("/nonexistent", "", "text", 0)
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
}

func TestDispatchHelp(t *testing.T) {
	result := Dispatch("/help", "", "text", 0)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Output == "" {
		t.Error("expected non-empty help output")
	}
}

func TestDispatchJSON(t *testing.T) {
	result := Dispatch("/help", "", "json", 0)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if len(result.Output) == 0 || result.Output[0] != '{' {
		t.Errorf("expected JSON output, got: %s", result.Output)
	}
}

func TestDispatchInitInstallsLatestCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_API_KEY", "")
	t.Setenv("NEX_API_KEY", "")

	dir := t.TempDir()
	logFile := filepath.Join(dir, "args.log")
	npmPath := filepath.Join(dir, "npm")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + strings.ReplaceAll(logFile, "'", "'\"'\"'") + "'\n"
	if err := os.WriteFile(npmPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WUPHF_CLI_INSTALL_BIN", "npm")
	t.Setenv("WUPHF_CLI_PACKAGE", "@example/wuphf")

	result := Dispatch("/init", "", "text", 0)
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (%s)", result.ExitCode, result.Error)
	}
	if !strings.Contains(result.Output, "Latest @example/wuphf CLI installed from npm.") {
		t.Fatalf("expected install notice, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "Setup defaults saved.") {
		t.Fatalf("expected setup defaults notice, got %q", result.Output)
	}
}
