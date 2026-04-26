// mlx-stub is a deterministic, scriptable stand-in for `mlx_lm.server`
// (and any other OpenAI-compatible local LLM). It runs the same
// /v1/models + /v1/chat/completions surface wuphf's openai-compat
// provider talks to, but the response stream is replayed from a
// fixture file so e2e tests don't depend on a real model.
//
// Usage:
//
//	mlx-stub --port 28080 --fixture web/e2e/fixtures/qwen-markdown-tool.txt
//
// Fixture format (one frame per line):
//
//	chunk: <text to emit as content delta>
//	tool: name=<tool> args=<json>           # structured tool_calls delta
//	tool_done                                # finish_reason=tool_calls
//	stop                                     # finish_reason=stop
//	delay-ms: <int>                          # sleep before next frame
//
// Frames stream out as SSE deltas, with a 5ms pacing between chunks
// by default so the channel-by-channel behaviour matches a slow real
// server. Multiple turns can be served from one fixture file via
// `---` separators (one turn per request).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	flagPort    = flag.Int("port", 28080, "TCP port to listen on")
	flagFixture = flag.String("fixture", "", "path to fixture script (required)")
	flagModel   = flag.String("model", "stub-model-v1", "model id to advertise on /v1/models")
)

type frame struct {
	kind     string // "chunk" | "tool" | "tool_done" | "stop" | "delay"
	content  string
	toolName string
	toolArgs string
	delayMs  int
}

type turn struct{ frames []frame }

type fixture struct {
	turns []turn
}

func parseFixture(path string) (*fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var (
		fx  fixture
		cur turn
	)
	flush := func() {
		if len(cur.frames) > 0 {
			fx.turns = append(fx.turns, cur)
			cur = turn{}
		}
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "---" {
			flush()
			continue
		}
		if line == "" {
			cur.frames = append(cur.frames, frame{kind: "chunk", content: "\n"})
			continue
		}
		if strings.HasPrefix(line, "chunk:") {
			cur.frames = append(cur.frames, frame{kind: "chunk", content: line[len("chunk:"):]})
			continue
		}
		if strings.HasPrefix(line, "raw:") {
			// raw:<exact bytes> — emit verbatim as a single chunk
			cur.frames = append(cur.frames, frame{kind: "chunk", content: line[len("raw:"):]})
			continue
		}
		if strings.HasPrefix(line, "tool:") {
			rest := strings.TrimSpace(line[len("tool:"):])
			name := ""
			args := ""
			for _, kv := range strings.SplitN(rest, " args=", 2) {
				if strings.HasPrefix(kv, "name=") {
					name = strings.TrimSpace(kv[len("name="):])
				} else if strings.HasPrefix(kv, "{") {
					args = strings.TrimSpace(kv)
				}
			}
			cur.frames = append(cur.frames, frame{kind: "tool", toolName: name, toolArgs: args})
			continue
		}
		if line == "tool_done" {
			cur.frames = append(cur.frames, frame{kind: "tool_done"})
			continue
		}
		if line == "stop" {
			cur.frames = append(cur.frames, frame{kind: "stop"})
			continue
		}
		if strings.HasPrefix(line, "delay-ms:") {
			// Surface parse errors loudly. A typo'd `delay-ms: 5o`
			// (letter o) used to silently become 0 — frame got
			// appended with no pause and the run continued, masking
			// fixture bugs. Same footgun shape as the unrecognised-
			// line case above; both should reach the author fast.
			val := strings.TrimSpace(line[len("delay-ms:"):])
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("fixture %s: invalid delay-ms %q: %w", path, val, err)
			}
			cur.frames = append(cur.frames, frame{kind: "delay", delayMs: n})
			continue
		}
		// Treat any unrecognised line as a literal content chunk so
		// fixture authors can paste raw model output verbatim.
		cur.frames = append(cur.frames, frame{kind: "chunk", content: line + "\n"})
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(fx.turns) == 0 {
		return nil, fmt.Errorf("fixture %s has no turns", path)
	}
	return &fx, nil
}

func main() {
	flag.Parse()
	if *flagFixture == "" {
		log.Fatal("--fixture is required")
	}
	fx, err := parseFixture(*flagFixture)
	if err != nil {
		log.Fatalf("load fixture: %v", err)
	}

	var (
		mu       sync.Mutex
		reqCount uint64
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": *flagModel}},
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddUint64(&reqCount, 1) - 1
		mu.Lock()
		t := fx.turns[int(idx)%len(fx.turns)]
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)

		emit := func(payload string) {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
		}

		toolIdx := 0
		for _, fr := range t.frames {
			switch fr.kind {
			case "delay":
				time.Sleep(time.Duration(fr.delayMs) * time.Millisecond)
			case "chunk":
				body, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta": map[string]any{"content": fr.content},
					}},
				})
				emit(string(body))
				time.Sleep(5 * time.Millisecond)
			case "tool":
				body, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta": map[string]any{
							"tool_calls": []map[string]any{{
								"index": toolIdx,
								"id":    fmt.Sprintf("call-%d", toolIdx),
								"type":  "function",
								"function": map[string]any{
									"name":      fr.toolName,
									"arguments": fr.toolArgs,
								},
							}},
						},
					}},
				})
				emit(string(body))
				toolIdx++
				time.Sleep(5 * time.Millisecond)
			case "tool_done":
				body, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta":         map[string]any{},
						"finish_reason": "tool_calls",
					}},
				})
				emit(string(body))
			case "stop":
				body, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta":         map[string]any{},
						"finish_reason": "stop",
					}},
				})
				emit(string(body))
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *flagPort)
	log.Printf("mlx-stub listening on %s; fixture=%s turns=%d", addr, *flagFixture, len(fx.turns))
	// Use http.Server (not ListenAndServe) so we can set
	// ReadHeaderTimeout — gosec / staff review minimum hardening
	// for any HTTP listener. ReadTimeout is generous because
	// Playwright sometimes pauses mid-request during debugger attach.
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
