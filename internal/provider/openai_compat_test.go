package provider

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// helper: drain a StreamFn channel into a slice of chunks.
func drainChunks(t *testing.T, ch <-chan agent.StreamChunk) []agent.StreamChunk {
	t.Helper()
	var out []agent.StreamChunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

// helper: build an SSE response body from raw event payloads (each becomes
// one `data: ...` frame), terminated by `data: [DONE]`.
func sseBody(events ...string) string {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString("data: ")
		b.WriteString(ev)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// TestParseOpenAISSEStream_TextDeltas verifies plain content deltas produce
// ordered text chunks.
func TestParseOpenAISSEStream_TextDeltas(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"content":"Hello "}}]}`,
		`{"choices":[{"delta":{"content":"world"}}]}`,
		`{"choices":[{"delta":{"content":"!"}, "finish_reason":"stop"}]}`,
	)

	ch := make(chan agent.StreamChunk, 16)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "test", strings.NewReader(body))
	}()

	chunks := drainChunks(t, ch)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 text chunks, got %d: %+v", len(chunks), chunks)
	}
	want := []string{"Hello ", "world", "!"}
	for i, c := range chunks {
		if c.Type != "text" {
			t.Errorf("chunk[%d].Type = %q, want text", i, c.Type)
		}
		if c.Content != want[i] {
			t.Errorf("chunk[%d].Content = %q, want %q", i, c.Content, want[i])
		}
	}
}

// TestParseOpenAISSEStream_ToolCallAcrossDeltas verifies the accumulator
// concatenates argument fragments and emits one tool_use chunk on [DONE].
func TestParseOpenAISSEStream_ToolCallAcrossDeltas(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_files","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\".\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	ch := make(chan agent.StreamChunk, 16)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "test", strings.NewReader(body))
	}()

	chunks := drainChunks(t, ch)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 tool_use chunk, got %d: %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.Type != "tool_use" {
		t.Fatalf("chunk type = %q, want tool_use", c.Type)
	}
	if c.ToolName != "list_files" {
		t.Errorf("ToolName = %q, want list_files", c.ToolName)
	}
	if c.ToolUseID != "call_1" {
		t.Errorf("ToolUseID = %q, want call_1", c.ToolUseID)
	}
	if got, _ := c.ToolParams["path"].(string); got != "." {
		t.Errorf("ToolParams[path] = %v, want \".\"", c.ToolParams["path"])
	}
	if !strings.Contains(c.ToolInput, `"path":".`) {
		t.Errorf("ToolInput = %q, expected raw JSON with path", c.ToolInput)
	}
}

// TestParseOpenAISSEStream_MixedTextAndToolCall verifies a turn that emits
// some text before invoking a tool produces both chunk kinds in the right
// order.
func TestParseOpenAISSEStream_MixedTextAndToolCall(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"content":"Looking it up..."}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"search","arguments":"{\"q\":\"x\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	ch := make(chan agent.StreamChunk, 16)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "test", strings.NewReader(body))
	}()
	chunks := drainChunks(t, ch)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (text then tool_use), got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Type != "text" || chunks[0].Content != "Looking it up..." {
		t.Errorf("chunk[0] = %+v, expected text 'Looking it up...'", chunks[0])
	}
	if chunks[1].Type != "tool_use" || chunks[1].ToolName != "search" {
		t.Errorf("chunk[1] = %+v, expected tool_use search", chunks[1])
	}
}

// TestParseOpenAISSEStream_ParallelToolCalls verifies two tool calls
// streamed at separate indexes both surface as distinct tool_use chunks
// in first-seen order.
func TestParseOpenAISSEStream_ParallelToolCalls(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"a","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"b","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	ch := make(chan agent.StreamChunk, 16)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "test", strings.NewReader(body))
	}()
	chunks := drainChunks(t, ch)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 tool_use chunks, got %d", len(chunks))
	}
	if chunks[0].ToolName != "a" || chunks[1].ToolName != "b" {
		t.Errorf("tool order = [%s, %s], want [a, b]", chunks[0].ToolName, chunks[1].ToolName)
	}
}

// TestParseOpenAISSEStream_MalformedToolArgs verifies an unparseable JSON
// arg surfaces as an error chunk identifying the tool, not a silent
// no-op.
func TestParseOpenAISSEStream_MalformedToolArgs(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"oops","arguments":"{not-json"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	ch := make(chan agent.StreamChunk, 16)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "mlx-lm", strings.NewReader(body))
	}()
	chunks := drainChunks(t, ch)
	if len(chunks) != 1 || chunks[0].Type != "error" {
		t.Fatalf("expected one error chunk, got %+v", chunks)
	}
	if !strings.Contains(chunks[0].Content, `tool "oops"`) {
		t.Errorf("error did not name the tool: %q", chunks[0].Content)
	}
	if !strings.Contains(chunks[0].Content, "mlx-lm") {
		t.Errorf("error did not name the kind: %q", chunks[0].Content)
	}
}

// TestParseOpenAISSEStream_JSONInContentToolCallFallback verifies the
// Qwen-style tool-call dialect (entire content is a single
// {"name":...,"arguments":{...}} JSON object) surfaces as a tool_use chunk
// even when the server doesn't populate structured tool_calls deltas.
func TestParseOpenAISSEStream_JSONInContentToolCallFallback(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"content":"{\"name\": \"get_weather\", \"arguments\": {\"city\": \"Lisbon\"}}"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ch := make(chan agent.StreamChunk, 16)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "mlx-lm", strings.NewReader(body))
	}()
	chunks := drainChunks(t, ch)
	// Expect: one text chunk (the streamed content) and one tool_use chunk
	// synthesized from it on flush.
	var sawToolUse bool
	for _, c := range chunks {
		if c.Type == "tool_use" {
			sawToolUse = true
			if c.ToolName != "get_weather" {
				t.Errorf("ToolName = %q, want get_weather", c.ToolName)
			}
			if got, _ := c.ToolParams["city"].(string); got != "Lisbon" {
				t.Errorf("ToolParams[city] = %v, want Lisbon", c.ToolParams["city"])
			}
		}
	}
	if !sawToolUse {
		t.Fatalf("fallback did not emit tool_use chunk: %+v", chunks)
	}
}

// TestParseOpenAISSEStream_JSONInContentFallback_OnlyWhenWholeContentIsJSON
// guards against false-positive fallbacks: prose that mentions a tool name
// must not be misread as an invocation.
func TestParseOpenAISSEStream_JSONInContentFallback_OnlyWhenWholeContentIsJSON(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"content":"I would call get_weather but I won't right now."}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)
	ch := make(chan agent.StreamChunk, 8)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "mlx-lm", strings.NewReader(body))
	}()
	for _, c := range drainChunks(t, ch) {
		if c.Type == "tool_use" {
			t.Fatalf("prose mentioning a tool was misread as an invocation: %+v", c)
		}
	}
}

// TestParseOpenAISSEStream_QwenToolsTagFallback verifies the
// <tools>{...}</tools> dialect Qwen2.5-Coder uses surfaces as a tool_use
// chunk. This is what the live MLX-LM test caught: the model emits
//
//	<tools>
//	{"name": "echo_phrase", "arguments": {"phrase": "x"}}
//	</tools>
//	The function returned ...
//
// rather than bare JSON.
func TestParseOpenAISSEStream_QwenToolsTagFallback(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"content":"<tools>\n{\"name\": \"echo_phrase\", \"arguments\": {\"phrase\": \"unified-steele\"}}\n</tools>\n"}}]}`,
		`{"choices":[{"delta":{"content":"\nThe function returned the phrase 'unified-steele'."}, "finish_reason":"stop"}]}`,
	)
	ch := make(chan agent.StreamChunk, 16)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "mlx-lm", strings.NewReader(body))
	}()
	chunks := drainChunks(t, ch)
	var sawToolUse bool
	for _, c := range chunks {
		if c.Type == "tool_use" {
			sawToolUse = true
			if c.ToolName != "echo_phrase" {
				t.Errorf("ToolName = %q, want echo_phrase", c.ToolName)
			}
			if got, _ := c.ToolParams["phrase"].(string); got != "unified-steele" {
				t.Errorf("ToolParams[phrase] = %v, want unified-steele", c.ToolParams["phrase"])
			}
		}
	}
	if !sawToolUse {
		t.Fatalf("Qwen <tools> dialect did not emit tool_use; chunks: %+v", chunks)
	}
}

// TestParseOpenAISSEStream_QwenToolsTagFallback_OnlyOneBlock guards against
// a model that emits multiple <tools> blocks (sometimes happens when it's
// reasoning aloud). Conservative path: do not invoke either; surface as
// text only.
func TestParseOpenAISSEStream_QwenToolsTagFallback_OnlyOneBlock(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"content":"thinking...\n<tools>{\"name\":\"a\",\"arguments\":{}}</tools>\nor maybe\n<tools>{\"name\":\"b\",\"arguments\":{}}</tools>"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)
	ch := make(chan agent.StreamChunk, 8)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "mlx-lm", strings.NewReader(body))
	}()
	for _, c := range drainChunks(t, ch) {
		if c.Type == "tool_use" {
			t.Fatalf("ambiguous multi-<tools> content was misread as an invocation: %+v", c)
		}
	}
}

// TestParseOpenAISSEStream_StructuredToolCallsBeatFallback confirms that
// when the server DOES emit structured tool_calls, we use those and don't
// also synthesize a duplicate from any text content the model emitted.
func TestParseOpenAISSEStream_StructuredToolCallsBeatFallback(t *testing.T) {
	body := sseBody(
		`{"choices":[{"delta":{"content":"{\"name\": \"shadow\", \"arguments\": {}}"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"real_tool","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	ch := make(chan agent.StreamChunk, 8)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "mlx-lm", strings.NewReader(body))
	}()
	var toolUses []agent.StreamChunk
	for _, c := range drainChunks(t, ch) {
		if c.Type == "tool_use" {
			toolUses = append(toolUses, c)
		}
	}
	if len(toolUses) != 1 {
		t.Fatalf("expected exactly 1 tool_use, got %d (%+v)", len(toolUses), toolUses)
	}
	if toolUses[0].ToolName != "real_tool" {
		t.Errorf("structured path was overridden by fallback: %q", toolUses[0].ToolName)
	}
}

// TestParseOpenAISSEStream_IgnoresEmptyDataFrames verifies that empty
// `data:\n\n` frames — which ollama and llama.cpp's server emit as
// keep-alive heartbeats under load — don't abort the stream. Earlier
// versions hit this mid-stream and turned every keep-alive into a fatal
// "unexpected end of JSON input" error chunk; the live MLX tests didn't
// catch it because mlx_lm.server doesn't emit them.
func TestParseOpenAISSEStream_IgnoresEmptyDataFrames(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n" +
		"data:\n\n" +
		"data: \n\n" + // also exercise the trailing-space variant
		"data: {\"choices\":[{\"delta\":{\"content\":\"b\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	ch := make(chan agent.StreamChunk, 8)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "ollama", strings.NewReader(body))
	}()
	chunks := drainChunks(t, ch)
	for _, c := range chunks {
		if c.Type == "error" {
			t.Fatalf("empty data frame should not produce error chunk: %+v", c)
		}
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 text chunks (a, b), got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Content != "a" || chunks[1].Content != "b" {
		t.Errorf("ordered text wrong: %+v", chunks)
	}
}

// TestNormalizeOpenAICompatEndpoint_AppendsV1WhenMissing prevents the
// silent-404 footgun where a user pasting `http://127.0.0.1:8080` from
// mlx_lm.server's startup banner gets a confusing
// `HTTP 404 from http://127.0.0.1:8080/chat/completions` instead of a
// working request.
func TestNormalizeOpenAICompatEndpoint_AppendsV1WhenMissing(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/v1/chat/completions"},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080/v1/chat/completions"},
		{"http://127.0.0.1:8080/v1", "http://127.0.0.1:8080/v1/chat/completions"},
		{"http://127.0.0.1:8080/v1/", "http://127.0.0.1:8080/v1/chat/completions"},
		// Multi-tenant proxies sometimes nest /v1 under a path prefix.
		{"https://gateway.example.com/api/v1/proxy", "https://gateway.example.com/api/v1/proxy/chat/completions"},
	}
	for _, tc := range cases {
		got := normalizeOpenAICompatEndpoint(tc.in)
		if got != tc.want {
			t.Errorf("normalizeOpenAICompatEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestParseOpenAISSEStream_IgnoresCommentLines verifies SSE comment lines
// (servers sometimes send keep-alive pings as `: ping`) don't break parsing.
func TestParseOpenAISSEStream_IgnoresCommentLines(t *testing.T) {
	body := ": keep-alive\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
		": another\n\n" +
		"data: [DONE]\n\n"
	ch := make(chan agent.StreamChunk, 4)
	go func() {
		defer close(ch)
		parseOpenAISSEStream(ch, "test", strings.NewReader(body))
	}()
	chunks := drainChunks(t, ch)
	if len(chunks) != 1 || chunks[0].Type != "text" || chunks[0].Content != "ok" {
		t.Fatalf("expected single text 'ok', got %+v", chunks)
	}
}

// TestOpenAICompatStreamFn_HTTP500SurfacesAsError exercises the full HTTP
// path: a 500 from the server should produce one error chunk including the
// kind, status, and response body excerpt.
func TestOpenAICompatStreamFn_HTTP500SurfacesAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"out of memory"}`)
	}))
	defer srv.Close()

	t.Setenv("WUPHF_MLX_LM_BASE_URL", srv.URL+"/v1")

	factory := NewOpenAICompatStreamFn(KindMLXLM, "http://unused", "default-model")
	stream := factory("agent-1")
	chunks := drainChunks(t, stream(
		[]agent.Message{{Role: "user", Content: "hi"}},
		nil,
	))

	if len(chunks) != 1 || chunks[0].Type != "error" {
		t.Fatalf("expected one error chunk, got %+v", chunks)
	}
	body := chunks[0].Content
	if !strings.Contains(body, "HTTP 500") {
		t.Errorf("error did not mention HTTP 500: %q", body)
	}
	if !strings.Contains(body, "out of memory") {
		t.Errorf("error did not include server body: %q", body)
	}
	if !strings.Contains(body, "mlx-lm") {
		t.Errorf("error did not name the kind: %q", body)
	}
}

// TestOpenAICompatStreamFn_TextStream_EndToEnd exercises the full HTTP path
// with a streamed 200 response to make sure the SSE plumbing matches what
// httptest's ResponseWriter produces (Transfer-Encoding handling, flushing,
// reader bufferng, etc.).
func TestOpenAICompatStreamFn_TextStream_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, ev := range []string{
			`{"choices":[{"delta":{"content":"al"}}]}`,
			`{"choices":[{"delta":{"content":"pha"}, "finish_reason":"stop"}]}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	t.Setenv("WUPHF_OLLAMA_BASE_URL", srv.URL+"/v1")

	factory := NewOpenAICompatStreamFn(KindOllama, "http://unused", "default-model")
	chunks := drainChunks(t, factory("a")(
		[]agent.Message{{Role: "user", Content: "say alpha"}},
		nil,
	))

	if len(chunks) != 2 {
		t.Fatalf("expected 2 text chunks, got %d (%+v)", len(chunks), chunks)
	}
	got := chunks[0].Content + chunks[1].Content
	if got != "alpha" {
		t.Errorf("concatenated text = %q, want \"alpha\"", got)
	}
}

// TestOpenAICompatStreamFn_RequestBodyShape verifies the outgoing JSON
// payload uses the OpenAI shape (model, messages, stream:true, tools[]).
func TestOpenAICompatStreamFn_RequestBodyShape(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		got = string(buf)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	t.Setenv("WUPHF_EXO_BASE_URL", srv.URL+"/v1")
	t.Setenv("WUPHF_EXO_MODEL", "test-model")

	factory := NewOpenAICompatStreamFn(KindExo, "http://unused", "unused-default")
	stream := factory("a")
	for range stream(
		[]agent.Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "ping"}},
		[]agent.AgentTool{{Name: "t1", Description: "d", Schema: map[string]any{"type": "object"}}},
	) {
	}

	for _, want := range []string{
		`"model":"test-model"`,
		`"stream":true`,
		`"tool_choice":"auto"`,
		`"name":"t1"`,
		`"role":"system"`,
		`"role":"user"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("request body missing %q\nbody: %s", want, got)
		}
	}
}
