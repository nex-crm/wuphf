package provider

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

func TestBuildClaudePromptsSeparatesSystemMessages(t *testing.T) {
	msgs := []agent.Message{
		{Role: "system", Content: "You are the CEO."},
		{Role: "system", Content: "Only delegate to @fe and @be."},
		{Role: "user", Content: "Build the product."},
		{Role: "assistant", Content: "I'll coordinate this."},
	}

	systemPrompt, prompt := buildClaudePrompts(msgs)

	if systemPrompt != "You are the CEO.\n\nOnly delegate to @fe and @be." {
		t.Fatalf("unexpected system prompt: %q", systemPrompt)
	}
	expectedPrompt := "user: Build the product.\nassistant: I'll coordinate this."
	if prompt != expectedPrompt {
		t.Fatalf("unexpected conversation prompt: %q", prompt)
	}
}

func TestBuildClaudeArgsIncludesResume(t *testing.T) {
	args := buildClaudeArgs("system instructions", "sess-123")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--print -") {
		t.Fatalf("expected stdin print mode, got %q", joined)
	}
	if !strings.Contains(joined, "--disable-slash-commands") {
		t.Fatalf("expected slash commands disabled, got %q", joined)
	}
	if !strings.Contains(joined, "--strict-mcp-config") {
		t.Fatalf("expected strict mcp config, got %q", joined)
	}
	if !strings.Contains(joined, "--setting-sources user") {
		t.Fatalf("expected user-only setting sources, got %q", joined)
	}
	if !strings.Contains(joined, "--resume sess-123") {
		t.Fatalf("expected resume args, got %q", joined)
	}
	if strings.Contains(joined, "--no-session-persistence") {
		t.Fatalf("unexpected no-session-persistence flag: %q", joined)
	}
}

func TestStreamTextChunksSplitsLargeBlocks(t *testing.T) {
	ch := make(chan agent.StreamChunk, 16)
	streamTextChunks(ch, "one two three four five six seven eight nine ten eleven twelve")
	close(ch)

	var chunks []string
	for chunk := range ch {
		chunks = append(chunks, chunk.Content)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if chunks[0] != "one two three four five" {
		t.Fatalf("unexpected first chunk: %q", chunks[0])
	}
	if chunks[1] != "six seven eight nine ten" {
		t.Fatalf("unexpected second chunk: %q", chunks[1])
	}
}

func TestCreateClaudeCodeStreamFnRetriesUnknownSession(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "claude-record.jsonl")
	cwd := t.TempDir()

	restore := stubClaudeRuntime(t, recordFile, "resume-retry", cwd)
	defer restore()

	fn := CreateClaudeCodeStreamFn("ceo")

	first := collectStreamChunks(fn([]agent.Message{{Role: "user", Content: "first turn"}}, nil))
	if joinedChunkText(first) != "fresh run one" {
		t.Fatalf("unexpected first response: %q", joinedChunkText(first))
	}

	second := collectStreamChunks(fn([]agent.Message{{Role: "user", Content: "second turn"}}, nil))
	secondText := joinedChunkText(second)
	if !strings.Contains(secondText, "fresh run two") {
		t.Fatalf("expected retry response text, got %q", secondText)
	}
	if !hasThinkingChunk(second, "retrying with a fresh Claude session") {
		t.Fatalf("expected retry thinking chunk, got %#v", second)
	}
	if hasErrorChunk(second) {
		t.Fatalf("did not expect error chunk after successful retry: %#v", second)
	}

	records := readClaudeHelperRecords(t, recordFile)
	if len(records) != 3 {
		t.Fatalf("expected 3 claude invocations, got %d", len(records))
	}
	if containsArg(records[0].Args, "--resume") {
		t.Fatalf("did not expect resume on first invocation: %#v", records[0].Args)
	}
	if !containsArgPair(records[1].Args, "--resume", "sess-one") {
		t.Fatalf("expected resume of sess-one on second invocation: %#v", records[1].Args)
	}
	if containsArg(records[2].Args, "--resume") {
		t.Fatalf("did not expect resume on retry invocation: %#v", records[2].Args)
	}
}

func TestCreateClaudeCodeStreamFnPersistsSessionAcrossFactoryRecreation(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "claude-persist-record.jsonl")
	cwd := t.TempDir()

	restore := stubClaudeRuntime(t, recordFile, "persist-across-restart", cwd)
	defer restore()

	resetStore := stubClaudeSessionStore(t, filepath.Join(t.TempDir(), "claude-sessions.json"))
	defer resetStore()

	fn := CreateClaudeCodeStreamFn("ceo")
	first := collectStreamChunks(fn([]agent.Message{{Role: "user", Content: "first turn"}}, nil))
	if joinedChunkText(first) != "first persisted run" {
		t.Fatalf("unexpected first response: %q", joinedChunkText(first))
	}

	claudeSessionStoreMu.Lock()
	claudeSessionStoreInstance = nil
	claudeSessionStoreMu.Unlock()

	fn = CreateClaudeCodeStreamFn("ceo")
	second := collectStreamChunks(fn([]agent.Message{{Role: "user", Content: "second turn"}}, nil))
	if joinedChunkText(second) != "resumed after restart" {
		t.Fatalf("unexpected second response: %q", joinedChunkText(second))
	}

	records := readClaudeHelperRecords(t, recordFile)
	if len(records) != 2 {
		t.Fatalf("expected 2 claude invocations, got %d", len(records))
	}
	if containsArg(records[0].Args, "--resume") {
		t.Fatalf("did not expect resume on first invocation: %#v", records[0].Args)
	}
	if !containsArgPair(records[1].Args, "--resume", "persisted-sess") {
		t.Fatalf("expected resumed session after factory recreation: %#v", records[1].Args)
	}
}

func TestCreateClaudeCodeStreamFnShowsLoginError(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "claude-login-record.jsonl")
	cwd := t.TempDir()

	restore := stubClaudeRuntime(t, recordFile, "login-required", cwd)
	defer restore()

	fn := CreateClaudeCodeStreamFn("ceo")
	chunks := collectStreamChunks(fn([]agent.Message{{Role: "user", Content: "hello"}}, nil))
	if !hasErrorChunkContaining(chunks, "Claude CLI requires login") {
		t.Fatalf("expected login guidance error, got %#v", chunks)
	}
}

type claudeHelperRecord struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

func collectStreamChunks(ch <-chan agent.StreamChunk) []agent.StreamChunk {
	var chunks []agent.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func joinedChunkText(chunks []agent.StreamChunk) string {
	var parts []string
	for _, chunk := range chunks {
		if chunk.Type == "text" {
			parts = append(parts, chunk.Content)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func hasThinkingChunk(chunks []agent.StreamChunk, needle string) bool {
	for _, chunk := range chunks {
		if chunk.Type == "thinking" && strings.Contains(chunk.Content, needle) {
			return true
		}
	}
	return false
}

func hasErrorChunk(chunks []agent.StreamChunk) bool {
	for _, chunk := range chunks {
		if chunk.Type == "error" {
			return true
		}
	}
	return false
}

func hasErrorChunkContaining(chunks []agent.StreamChunk, needle string) bool {
	for _, chunk := range chunks {
		if chunk.Type == "error" && strings.Contains(chunk.Content, needle) {
			return true
		}
	}
	return false
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, key string, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func readClaudeHelperRecords(t *testing.T, path string) []claudeHelperRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record file: %v", err)
	}

	var records []claudeHelperRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record claudeHelperRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal helper record: %v", err)
		}
		records = append(records, record)
	}
	return records
}

func stubClaudeRuntime(t *testing.T, recordFile string, scenario string, cwd string) func() {
	t.Helper()

	oldLookPath := claudeLookPath
	oldCommand := claudeCommand
	oldGetwd := claudeGetwd
	oldConfigure := claudeConfigureProcess
	t.Setenv("GO_WANT_CLAUDE_HELPER_PROCESS", "1")
	t.Setenv("CLAUDE_TEST_RECORD_FILE", recordFile)
	t.Setenv("CLAUDE_TEST_SCENARIO", scenario)

	claudeLookPath = func(file string) (string, error) {
		return "/usr/bin/claude", nil
	}
	claudeGetwd = func() (string, error) {
		return cwd, nil
	}
	claudeConfigureProcess = func(cmd *exec.Cmd) {}
	claudeCommand = func(name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestClaudeHelperProcess", "--"}
		cmdArgs = append(cmdArgs, args...)
		return exec.Command(os.Args[0], cmdArgs...)
	}

	return func() {
		claudeLookPath = oldLookPath
		claudeCommand = oldCommand
		claudeGetwd = oldGetwd
		claudeConfigureProcess = oldConfigure
	}
}

func stubClaudeSessionStore(t *testing.T, path string) func() {
	t.Helper()

	oldFactory := claudeSessionStoreFactory

	claudeSessionStoreMu.Lock()
	claudeSessionStoreFactory = func() *claudeSessionStore {
		return newClaudeSessionStoreAt(path)
	}
	claudeSessionStoreInstance = nil
	claudeSessionStoreMu.Unlock()

	return func() {
		claudeSessionStoreMu.Lock()
		claudeSessionStoreFactory = oldFactory
		claudeSessionStoreInstance = nil
		claudeSessionStoreMu.Unlock()
	}
}

func TestClaudeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CLAUDE_HELPER_PROCESS") != "1" {
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
	claudeArgs := append([]string(nil), args[doubleDash+1:]...)
	stdin, _ := io.ReadAll(os.Stdin)

	recordPath := os.Getenv("CLAUDE_TEST_RECORD_FILE")
	record, _ := json.Marshal(claudeHelperRecord{Args: claudeArgs, Stdin: string(stdin)})
	file, err := os.OpenFile(recordPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open helper record file: %v", err)
	}
	if _, err := file.Write(append(record, '\n')); err != nil {
		t.Fatalf("write helper record: %v", err)
	}
	file.Close()

	linesRaw, _ := os.ReadFile(recordPath)
	callCount := 0
	for _, line := range strings.Split(strings.TrimSpace(string(linesRaw)), "\n") {
		if strings.TrimSpace(line) != "" {
			callCount++
		}
	}

	switch os.Getenv("CLAUDE_TEST_SCENARIO") {
	case "resume-retry":
		switch callCount {
		case 1:
			if containsArg(claudeArgs, "--resume") {
				t.Fatalf("did not expect resume on first call: %#v", claudeArgs)
			}
			_, _ = os.Stdout.WriteString("{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"sess-one\",\"model\":\"claude-opus\"}\n")
			_, _ = os.Stdout.WriteString("{\"type\":\"assistant\",\"session_id\":\"sess-one\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"fresh run one\"}]}}\n")
			_, _ = os.Stdout.WriteString("{\"type\":\"result\",\"session_id\":\"sess-one\",\"result\":\"fresh run one\"}\n")
			os.Exit(0)
		case 2:
			if !containsArgPair(claudeArgs, "--resume", "sess-one") {
				t.Fatalf("expected resume on second call: %#v", claudeArgs)
			}
			_, _ = os.Stdout.WriteString("{\"type\":\"result\",\"subtype\":\"error\",\"session_id\":\"sess-one\",\"result\":\"No conversation found with session id sess-one\",\"errors\":[\"No conversation found with session id sess-one\"]}\n")
			_, _ = os.Stderr.WriteString("no conversation found\n")
			os.Exit(1)
		case 3:
			if containsArg(claudeArgs, "--resume") {
				t.Fatalf("did not expect resume on retry call: %#v", claudeArgs)
			}
			_, _ = os.Stdout.WriteString("{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"sess-two\",\"model\":\"claude-opus\"}\n")
			_, _ = os.Stdout.WriteString("{\"type\":\"assistant\",\"session_id\":\"sess-two\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"fresh run two\"}]}}\n")
			_, _ = os.Stdout.WriteString("{\"type\":\"result\",\"session_id\":\"sess-two\",\"result\":\"fresh run two\"}\n")
			os.Exit(0)
		default:
			t.Fatalf("unexpected call count: %d", callCount)
		}
	case "login-required":
		_, _ = os.Stdout.WriteString("{\"type\":\"result\",\"subtype\":\"error\",\"result\":\"Please run claude login\",\"errors\":[\"Please run claude login\"]}\n")
		_, _ = os.Stderr.WriteString("authentication required\n")
		os.Exit(1)
	case "persist-across-restart":
		switch callCount {
		case 1:
			_, _ = os.Stdout.WriteString("{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"persisted-sess\",\"model\":\"claude-opus\"}\n")
			_, _ = os.Stdout.WriteString("{\"type\":\"assistant\",\"session_id\":\"persisted-sess\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"first persisted run\"}]}}\n")
			_, _ = os.Stdout.WriteString("{\"type\":\"result\",\"session_id\":\"persisted-sess\",\"result\":\"first persisted run\"}\n")
			os.Exit(0)
		case 2:
			if !containsArgPair(claudeArgs, "--resume", "persisted-sess") {
				t.Fatalf("expected resume after restart: %#v", claudeArgs)
			}
			_, _ = os.Stdout.WriteString("{\"type\":\"assistant\",\"session_id\":\"persisted-sess\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"resumed after restart\"}]}}\n")
			_, _ = os.Stdout.WriteString("{\"type\":\"result\",\"session_id\":\"persisted-sess\",\"result\":\"resumed after restart\"}\n")
			os.Exit(0)
		default:
			t.Fatalf("unexpected call count: %d", callCount)
		}
	default:
		t.Fatalf("unknown helper scenario: %s", os.Getenv("CLAUDE_TEST_SCENARIO"))
	}
}
