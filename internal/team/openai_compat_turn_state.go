package team

import (
	"fmt"
	"strings"
	"time"
)

// runtimeTurnSinks adapts the production Launcher / agentStream /
// log paths to the openAICompatTurnSinks interface. Tests inject a
// fake sink instead.
type runtimeTurnSinks struct {
	l       *Launcher
	slug    string
	stream  *agentStreamBuffer
	metrics *headlessProgressMetrics
}

func (s *runtimeTurnSinks) pushAgentStream(line string) {
	if s.stream == nil || line == "" {
		return
	}
	s.stream.Push(line)
}

func (s *runtimeTurnSinks) updateProgressLabel(label string) {
	if s.l == nil || s.metrics == nil {
		return
	}
	s.l.updateHeadlessProgress(s.slug, "active", "text", label, *s.metrics)
}

func (s *runtimeTurnSinks) appendLog(line string) {
	appendHeadlessCodexLog(s.slug, line)
}

// openAICompatTurnSinks is the surface runHeadlessOpenAICompatTurn
// needs to observe + drive a single turn — agent stream pushes, the
// per-agent progress label, and the on-disk log. Pulled out as an
// interface so the turn-state machine can be unit-tested without a
// Launcher / broker fixture.
type openAICompatTurnSinks interface {
	// pushAgentStream emits a raw text chunk (or a tool-use marker)
	// into the live-output panel via the broker SSE.
	pushAgentStream(s string)
	// updateProgressLabel flips the agent's sidebar label (e.g.
	// "drafting response · ~12 tok/s"). The state machine throttles
	// these so the broker SSE isn't pummelled on fast streams.
	updateProgressLabel(label string)
	// appendLog records a line in the per-agent log (used for
	// post-mortem of unparsed tool calls etc).
	appendLog(line string)
}

// openAICompatTurnState owns the closure-captured variables the
// runner used to keep inline. Pulled out so:
//   - the live-output suppression decision (looksJSON gate)
//   - the broadcasted-this-turn gate (post-tool double-post fix)
//   - the rolling tokens-per-second readout
//
// each have a single named field on a single struct, can be mutated
// from a small set of methods, and can be exercised by tests that
// drive scripted text/tool callbacks without spinning up a broker.
//
// All fields are intentionally unexported — callers go through the
// methods. Concurrency: the loop calls these from a single goroutine
// (the per-turn driver), so no internal locking is needed; if we
// ever fan-out we'll revisit.
type openAICompatTurnState struct {
	sinks openAICompatTurnSinks

	// Live-output suppression. Open models stream JSON tool calls
	// chunk-by-chunk; without buffering the user watches raw JSON
	// type itself out before the loop swaps it for a tool_use chunk.
	liveBuf   strings.Builder
	looksJSON bool
	decided   bool

	// Broadcasted-this-turn gate. Flips true the first time a tool
	// dispatch posts user-visible content; the runner suppresses the
	// post-loop final-text post when set so the channel sees one
	// reply per user message instead of two (tool + summary).
	broadcastedThisTurn bool

	// Rolling tokens/sec window. Anchor + char-count snapshot,
	// re-anchored every ~750ms so the label doesn't flicker. We
	// estimate 4 chars/token for English; close enough for a vibe
	// readout and far cheaper than tokenizing.
	streamedChars int
	tpsAnchorAt   time.Time
	tpsAnchorChrs int
}

func newOpenAICompatTurnState(sinks openAICompatTurnSinks) *openAICompatTurnState {
	return &openAICompatTurnState{sinks: sinks}
}

// onText is the per-text-chunk handler. Updates the tps readout and
// gates the live-output push so JSON tool calls don't leak as text
// chunks while the model is still streaming the JSON.
func (s *openAICompatTurnState) onText(chunk string) {
	if chunk == "" {
		return
	}
	s.streamedChars += len(chunk)
	s.maybeUpdateTpsLabel(time.Now())
	s.maybeFlushLive(chunk)
}

// maybeUpdateTpsLabel flips the progress label every >=750ms while
// the stream is producing chars. Pulled out for direct testing of the
// throttle semantics.
func (s *openAICompatTurnState) maybeUpdateTpsLabel(now time.Time) {
	if s.tpsAnchorAt.IsZero() {
		s.tpsAnchorAt = now
		s.tpsAnchorChrs = s.streamedChars
		return
	}
	if now.Sub(s.tpsAnchorAt) < 750*time.Millisecond {
		return
	}
	delta := s.streamedChars - s.tpsAnchorChrs
	elapsed := now.Sub(s.tpsAnchorAt).Seconds()
	if delta > 0 && elapsed > 0 {
		tps := float64(delta) / 4.0 / elapsed
		s.sinks.updateProgressLabel(fmt.Sprintf("drafting response · ~%.0f tok/s", tps))
	}
	s.tpsAnchorAt = now
	s.tpsAnchorChrs = s.streamedChars
}

// maybeFlushLive owns the looksJSON-gated buffer. Until ~8 non-
// whitespace chars have arrived, the chunk is buffered and we
// classify the stream as JSON-shaped or not. Once classified:
// JSON-shaped streams stay buffered (we want the parser's tool_use
// to replace it); non-JSON streams flush + push every chunk live.
func (s *openAICompatTurnState) maybeFlushLive(chunk string) {
	if !s.decided {
		s.liveBuf.WriteString(chunk)
		trimmed := strings.TrimLeft(s.liveBuf.String(), " \t\r\n")
		if len(trimmed) >= 8 || strings.ContainsAny(trimmed, "\n") {
			s.looksJSON = strings.HasPrefix(trimmed, "{")
			s.decided = true
			if !s.looksJSON {
				if s.liveBuf.Len() > 0 {
					s.sinks.pushAgentStream(s.liveBuf.String())
				}
				s.liveBuf.Reset()
			}
		}
		return
	}
	if s.looksJSON {
		s.liveBuf.WriteString(chunk)
		return
	}
	s.sinks.pushAgentStream(chunk)
}

// onToolUseChunk handles a tool_use stream chunk: discards any
// buffered JSON (it was the tool's invocation, not a user-visible
// reply), resets the JSON-detection + tps anchor for the next
// iteration, and pushes a [tool_use ...] marker into the live-output
// panel so the user sees "the model is doing something".
func (s *openAICompatTurnState) onToolUseChunk(toolName, rawInput string) {
	s.liveBuf.Reset()
	s.looksJSON = false
	s.decided = false
	s.tpsAnchorAt = time.Time{}
	s.tpsAnchorChrs = 0
	s.streamedChars = 0
	s.sinks.pushAgentStream(fmt.Sprintf("[tool_use %s] %s", toolName, rawInput))
}

// onToolResult handles the result callback from openAICompatToolLoop:
// records the success/failure in the per-agent log and flips the
// broadcasted-this-turn gate when the tool was a user-visible post
// (so the runner suppresses the post-loop final-text post).
func (s *openAICompatTurnState) onToolResult(name, result string, err error) {
	if err != nil {
		s.sinks.appendLog(fmt.Sprintf("openai_compat_tool_error: %s -> %v", name, err))
		return
	}
	s.sinks.appendLog(fmt.Sprintf("openai_compat_tool_ok: %s -> %s", name, truncate(result, 240)))
	if isUserVisiblePostTool(name) {
		s.broadcastedThisTurn = true
	}
}

// onError handles a model-side error chunk. Surfaces it in the live
// panel so the user sees the failure inline rather than a silent
// stall.
func (s *openAICompatTurnState) onError(msg string) {
	s.sinks.pushAgentStream("[error] " + msg)
}

// shouldPostFinalText is the post-loop predicate for "should the
// runner post finalText to the channel?". Returns false when a
// user-visible posting tool already ran in this turn — the runner
// uses this to avoid double-posting (and to break the broker
// fan-out loop where the agent's own post re-fires it).
func (s *openAICompatTurnState) shouldPostFinalText() bool {
	return !s.broadcastedThisTurn
}
