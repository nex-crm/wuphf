package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

type opencodeHelperRecord struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

func TestBuildOpencodeArgsPassesPromptAsFinalArgAndModel(t *testing.T) {
	args := buildOpencodeArgs("anthropic/claude-sonnet-4", "ship the thing")
	if len(args) == 0 || args[0] != "run" {
		t.Fatalf("expected `run` as first arg, got %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model anthropic/claude-sonnet-4") {
		t.Fatalf("expected explicit model flag, got %q", joined)
	}
	// Opencode's CLI has no --cwd/--quiet/- flags; working dir comes from cmd.Dir.
	for _, a := range args {
		if a == "--cwd" || a == "--quiet" || a == "-" {
			t.Fatalf("unexpected flag %q: Opencode CLI does not accept it; got %v", a, args)
		}
	}
	if args[len(args)-1] != "ship the thing" {
		t.Fatalf("expected prompt as final variadic arg, got %q", args[len(args)-1])
	}
}

func TestBuildOpencodeArgsOmitsModelWhenUnset(t *testing.T) {
	args := buildOpencodeArgs("", "do the thing")
	for _, a := range args {
		if a == "--model" {
			t.Fatalf("did not expect --model flag when model empty, got %v", args)
		}
	}
}

func TestBuildOpencodeArgsOmitsPromptWhenEmpty(t *testing.T) {
	args := buildOpencodeArgs("anthropic/claude-sonnet-4", "")
	// With no prompt, only `run` + `--model X` should be present.
	if len(args) != 3 {
		t.Fatalf("expected [run --model X], got %v", args)
	}
}

func TestBuildOpencodePromptWrapsSystem(t *testing.T) {
	got := buildOpencodePrompt("sys instructions", "do the thing")
	if !strings.Contains(got, "<system>") {
		t.Fatalf("expected system wrapper, got %q", got)
	}
	if !strings.Contains(got, "do the thing") {
		t.Fatalf("expected user prompt, got %q", got)
	}
}

func TestReadOpencodeStreamConcatenatesLines(t *testing.T) {
	var received []string
	out, err := readOpencodeStream(bytes.NewBufferString("line one\nline two\n"), func(s string) {
		received = append(received, s)
	})
	if err != nil {
		t.Fatalf("readOpencodeStream: %v", err)
	}
	if out != "line one\nline two" {
		t.Fatalf("unexpected concatenated output: %q", out)
	}
	if len(received) != 2 || received[0] != "line one" || received[1] != "line two" {
		t.Fatalf("expected onLine called per line, got %v", received)
	}
}

func TestCreateOpencodeCLIStreamFnStreamsPlainText(t *testing.T) {
	recordFile := t.TempDir() + "/opencode-record.jsonl"
	cwd := t.TempDir()

	restore := stubOpencodeRuntime(t, recordFile, "success", cwd)
	defer restore()

	fn := CreateOpencodeCLIStreamFn("ceo")
	chunks := collectStreamChunks(fn([]agent.Message{
		{Role: "system", Content: "You are the CEO."},
		{Role: "user", Content: "Ship it."},
	}, nil))

	text := joinedChunkText(chunks)
	if !strings.Contains(text, "shipped") {
		t.Fatalf("expected streamed plaintext reply, got %q", text)
	}

	records := readOpencodeHelperRecords(t, recordFile)
	if len(records) != 1 {
		t.Fatalf("expected 1 opencode invocation, got %d", len(records))
	}
	// Prompt is passed as the final variadic positional arg (not stdin).
	lastArg := records[0].Args[len(records[0].Args)-1]
	if !strings.Contains(lastArg, "<system>") || !strings.Contains(lastArg, "Ship it.") {
		t.Fatalf("expected composed prompt as final argv arg, got %q", lastArg)
	}
	// cwd is conveyed via cmd.Dir, not a CLI flag — confirm no stale --cwd.
	for _, a := range records[0].Args {
		if a == "--cwd" {
			t.Fatalf("Opencode CLI does not accept --cwd; got %#v", records[0].Args)
		}
	}
}

func TestCreateOpencodeCLIStreamFnSurfacesMissingBinaryError(t *testing.T) {
	oldLookPath := opencodeLookPath
	opencodeLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	defer func() { opencodeLookPath = oldLookPath }()

	fn := CreateOpencodeCLIStreamFn("ceo")
	chunks := collectStreamChunks(fn([]agent.Message{{Role: "user", Content: "hi"}}, nil))
	if !hasErrorChunkContaining(chunks, "Opencode CLI not found") {
		t.Fatalf("expected missing binary error, got %#v", chunks)
	}
}

func readOpencodeHelperRecords(t *testing.T, path string) []opencodeHelperRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}
	var records []opencodeHelperRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record opencodeHelperRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal helper record: %v", err)
		}
		records = append(records, record)
	}
	return records
}

func stubOpencodeRuntime(t *testing.T, recordFile string, scenario string, cwd string) func() {
	t.Helper()

	oldLookPath := opencodeLookPath
	oldCommand := opencodeCommand
	oldGetwd := opencodeGetwd
	t.Setenv("GO_WANT_OPENCODE_HELPER_PROCESS", "1")
	t.Setenv("OPENCODE_TEST_RECORD_FILE", recordFile)
	t.Setenv("OPENCODE_TEST_SCENARIO", scenario)
	t.Setenv("HOME", t.TempDir())

	opencodeLookPath = func(file string) (string, error) {
		return "/usr/bin/opencode", nil
	}
	opencodeGetwd = func() (string, error) {
		return cwd, nil
	}
	opencodeCommand = func(name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestOpencodeHelperProcess", "--"}
		cmdArgs = append(cmdArgs, args...)
		return exec.Command(os.Args[0], cmdArgs...)
	}

	return func() {
		opencodeLookPath = oldLookPath
		opencodeCommand = oldCommand
		opencodeGetwd = oldGetwd
	}
}

func TestOpencodeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_OPENCODE_HELPER_PROCESS") != "1" {
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
	opencodeArgs := append([]string(nil), args[doubleDash+1:]...)
	stdin, _ := io.ReadAll(os.Stdin)

	recordPath := os.Getenv("OPENCODE_TEST_RECORD_FILE")
	record, _ := json.Marshal(opencodeHelperRecord{Args: opencodeArgs, Stdin: string(stdin)})
	file, err := os.OpenFile(recordPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open helper record file: %v", err)
	}
	if _, err := file.Write(append(record, '\n')); err != nil {
		t.Fatalf("write helper record: %v", err)
	}
	file.Close()

	if !containsArg(opencodeArgs, "run") {
		t.Fatalf("missing `run` subcommand: %#v", opencodeArgs)
	}

	switch os.Getenv("OPENCODE_TEST_SCENARIO") {
	case "success":
		_, _ = os.Stdout.WriteString("shipped the update\n")
		os.Exit(0)
	case "auth-error":
		_, _ = os.Stderr.WriteString("error: unauthorized\n")
		os.Exit(1)
	default:
		t.Fatalf("unknown helper scenario: %s", os.Getenv("OPENCODE_TEST_SCENARIO"))
	}
}
