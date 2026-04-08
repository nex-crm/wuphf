package provider

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

type codexHelperRecord struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

func TestBuildCodexArgsIncludesOutputFile(t *testing.T) {
	args := buildCodexArgs("/tmp/work", "/tmp/out.txt")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "exec") {
		t.Fatalf("expected exec command, got %q", joined)
	}
	if !strings.Contains(joined, "-C /tmp/work") {
		t.Fatalf("expected working directory, got %q", joined)
	}
	if !strings.Contains(joined, "--output-last-message /tmp/out.txt") {
		t.Fatalf("expected output file flag, got %q", joined)
	}
	if !strings.Contains(joined, "--ephemeral") {
		t.Fatalf("expected ephemeral execution, got %q", joined)
	}
}

func TestCreateCodexCLIStreamFnStreamsFinalMessage(t *testing.T) {
	recordFile := t.TempDir() + "/codex-record.jsonl"
	cwd := t.TempDir()

	restore := stubCodexRuntime(t, recordFile, "success", cwd)
	defer restore()

	fn := CreateCodexCLIStreamFn("ceo")
	chunks := collectStreamChunks(fn([]agent.Message{
		{Role: "system", Content: "You are the CEO."},
		{Role: "user", Content: "Ship it."},
	}, nil))

	if joinedChunkText(chunks) != "codex final answer" {
		t.Fatalf("unexpected codex response: %#v", chunks)
	}

	records := readCodexHelperRecords(t, recordFile)
	if len(records) != 1 {
		t.Fatalf("expected 1 codex invocation, got %d", len(records))
	}
	if !containsArgPair(records[0].Args, "-C", cwd) {
		t.Fatalf("expected codex cwd arg, got %#v", records[0].Args)
	}
	if !strings.Contains(records[0].Stdin, "<system>") {
		t.Fatalf("expected system prompt wrapper in stdin, got %q", records[0].Stdin)
	}
}

func TestCreateCodexCLIStreamFnShowsLoginError(t *testing.T) {
	recordFile := t.TempDir() + "/codex-login-record.jsonl"
	cwd := t.TempDir()

	restore := stubCodexRuntime(t, recordFile, "login-required", cwd)
	defer restore()

	fn := CreateCodexCLIStreamFn("ceo")
	chunks := collectStreamChunks(fn([]agent.Message{{Role: "user", Content: "hello"}}, nil))
	if !hasErrorChunkContaining(chunks, "Codex CLI requires login") {
		t.Fatalf("expected login guidance error, got %#v", chunks)
	}
}

func readCodexHelperRecords(t *testing.T, path string) []codexHelperRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}

	var records []codexHelperRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record codexHelperRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal helper record: %v", err)
		}
		records = append(records, record)
	}
	return records
}

func stubCodexRuntime(t *testing.T, recordFile string, scenario string, cwd string) func() {
	t.Helper()

	oldLookPath := codexLookPath
	oldCommand := codexCommand
	oldGetwd := codexGetwd
	t.Setenv("GO_WANT_CODEX_HELPER_PROCESS", "1")
	t.Setenv("CODEX_TEST_RECORD_FILE", recordFile)
	t.Setenv("CODEX_TEST_SCENARIO", scenario)

	codexLookPath = func(file string) (string, error) {
		return "/usr/bin/codex", nil
	}
	codexGetwd = func() (string, error) {
		return cwd, nil
	}
	codexCommand = func(name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestCodexHelperProcess", "--"}
		cmdArgs = append(cmdArgs, args...)
		return exec.Command(os.Args[0], cmdArgs...)
	}

	return func() {
		codexLookPath = oldLookPath
		codexCommand = oldCommand
		codexGetwd = oldGetwd
	}
}

func TestCodexHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CODEX_HELPER_PROCESS") != "1" {
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

	recordPath := os.Getenv("CODEX_TEST_RECORD_FILE")
	record, _ := json.Marshal(codexHelperRecord{Args: codexArgs, Stdin: string(stdin)})
	file, err := os.OpenFile(recordPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open helper record file: %v", err)
	}
	if _, err := file.Write(append(record, '\n')); err != nil {
		t.Fatalf("write helper record: %v", err)
	}
	file.Close()

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

	switch os.Getenv("CODEX_TEST_SCENARIO") {
	case "success":
		if err := os.WriteFile(outputPath, []byte("codex final answer"), 0o644); err != nil {
			t.Fatalf("write codex output: %v", err)
		}
		os.Exit(0)
	case "login-required":
		_ = os.WriteFile(outputPath, []byte(""), 0o644)
		_, _ = os.Stderr.WriteString("authentication required\n")
		os.Exit(1)
	default:
		t.Fatalf("unknown helper scenario: %s", os.Getenv("CODEX_TEST_SCENARIO"))
	}
}
