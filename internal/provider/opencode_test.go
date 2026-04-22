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

func TestBuildOpencodeArgsIncludesRunAndQuietAndStdin(t *testing.T) {
	args := buildOpencodeArgs("/tmp/work", "qwen3.6:35b-a3b")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "run") {
		t.Fatalf("expected `run` subcommand, got %q", joined)
	}
	if !strings.Contains(joined, "--cwd /tmp/work") {
		t.Fatalf("expected working directory, got %q", joined)
	}
	if !strings.Contains(joined, "--quiet") {
		t.Fatalf("expected quiet flag, got %q", joined)
	}
	if !strings.Contains(joined, "--model qwen3.6:35b-a3b") {
		t.Fatalf("expected explicit model flag, got %q", joined)
	}
	if args[len(args)-1] != "-" {
		t.Fatalf("expected prompt to be read from stdin (last arg `-`), got %q", args[len(args)-1])
	}
}

func TestBuildOpencodeArgsOmitsModelWhenUnset(t *testing.T) {
	args := buildOpencodeArgs("/tmp/work", "")
	for _, a := range args {
		if a == "--model" {
			t.Fatalf("did not expect --model flag when model empty, got %v", args)
		}
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
	if !containsArgPair(records[0].Args, "--cwd", cwd) {
		t.Fatalf("expected opencode cwd arg, got %#v", records[0].Args)
	}
	if !strings.Contains(records[0].Stdin, "<system>") {
		t.Fatalf("expected system prompt wrapper in stdin, got %q", records[0].Stdin)
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

	if !containsArg(opencodeArgs, "--quiet") {
		t.Fatalf("missing --quiet arg: %#v", opencodeArgs)
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
