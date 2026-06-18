package team

// slack_stream.go delivers an agent's pane reply LIVE: one Slack message that
// builds in place (chat.startStream → appendStream → stopStream) as the reply is
// generated, instead of the several separate posts the normal relay path would
// produce (each up to a poll-interval apart). Scoped to the 1:1 Assistant pane —
// the only surface with the Assistant feature, and the one where watching the
// office think is wanted. Shared channels are never streamed.
//
// The relay (headless_live_chat_relay.go) owns one of these per turn, so
// correlation is exact: the sink IS that turn's stream. It is best-effort — if
// the stream cannot start (e.g. the app has not been reinstalled with the
// Assistant feature) the relay reverts to normal posting, so a reply is never
// lost.

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
)

// slackStreamCallTimeout bounds each streaming Web API call.
const slackStreamCallTimeout = 12 * time.Second

// turnStreamSink is a live, incremental delivery channel for one agent turn.
// Returned by the broker only for a channel that is an open streaming surface
// (a Slack pane today); nil otherwise, meaning the turn delivers via the normal
// message path. All methods are best-effort.
type turnStreamSink interface {
	// Append shows more of the reply, live. It returns an error only when the
	// stream could not be STARTED (the first append) — the caller then falls back
	// to normal posting for the whole turn. A failure to append after a
	// successful start is swallowed (the text still lands in the recorded reply).
	Append(text string) error
	// Started reports whether the live stream has actually opened.
	Started() bool
	// Close finalizes the streamed message. It is a no-op if the stream never
	// started. disclaimer, when non-empty, is appended as a closing line.
	Close(disclaimer string)
}

// openTurnStream returns a live stream sink for an agent's reply when channel is
// that agent's own open Slack pane, else nil (the caller posts normally). The
// agent (slug) must own the DM channel — the pane is the agent's 1:1 surface.
func (b *Broker) openTurnStream(slug, channel string) turnStreamSink {
	if b == nil {
		return nil
	}
	slug = normalizeActorSlug(slug)
	channel = normalizeChannelSlug(channel)
	if slug == "" || !IsDMSlug(channel) {
		return nil
	}
	// The streamed reply must be the agent talking in its OWN pane DM — the same
	// slug AssistantPaneRef / seedAssistantThread bind the pane to.
	if normalizeChannelSlug(DMSlugFor(slug)) != channel {
		return nil
	}
	b.mu.Lock()
	st := b.slackTransport
	b.mu.Unlock()
	if st == nil {
		return nil
	}
	return st.openPaneStream(slug)
}

// openPaneStream builds a live stream bound to the agent's open Assistant pane,
// or nil when the pane is not open / the transport cannot stream.
func (t *SlackTransport) openPaneStream(slug string) turnStreamSink {
	if t == nil || t.api == nil || t.Broker == nil {
		return nil
	}
	channelID, threadTS, ok := t.Broker.AssistantPaneRef(slug)
	if !ok {
		return nil
	}
	return &slackPaneStream{api: t.api, channelID: channelID, threadTS: threadTS}
}

// slackPaneStream is one turn's live message in a pane. Lazily starts on the
// first append, appends each subsequent chunk, and finalizes on close.
type slackPaneStream struct {
	api       slackAPI
	channelID string
	threadTS  string

	mu      sync.Mutex
	started bool
	failed  bool
	ts      string
}

func (s *slackPaneStream) Started() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *slackPaneStream) Append(text string) error {
	text = strings.TrimSpace(text)
	if s == nil || text == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		// Already fell back to normal posting; do nothing further.
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), slackStreamCallTimeout)
	defer cancel()
	chunk := slack.MsgOptionChunks(slack.NewMarkdownTextChunk(text))
	if !s.started {
		opts := []slack.MsgOption{chunk}
		if s.threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(s.threadTS))
		}
		_, ts, err := s.api.StartStreamContext(ctx, s.channelID, opts...)
		if err != nil {
			// The stream could not start (e.g. no Assistant feature). Signal the
			// caller to fall back to normal posting for the whole turn.
			s.failed = true
			return err
		}
		s.started = true
		s.ts = ts
		return nil
	}
	if _, _, err := s.api.AppendStreamContext(ctx, s.channelID, s.ts, chunk); err != nil {
		// A transient append failure does not abort the turn: the full reply is
		// still recorded to history by the relay. Drop this live delta.
		return nil
	}
	return nil
}

func (s *slackPaneStream) Close(disclaimer string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.failed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), slackStreamCallTimeout)
	defer cancel()
	if disclaimer = strings.TrimSpace(disclaimer); disclaimer != "" {
		// A closing line (the LLM disclaimer) appended just before finalizing.
		_, _, _ = s.api.AppendStreamContext(ctx, s.channelID, s.ts,
			slack.MsgOptionChunks(slack.NewMarkdownTextChunk("\n\n"+disclaimer)))
	}
	_, _, _ = s.api.StopStreamContext(ctx, s.channelID, s.ts)
	s.started = false
}
