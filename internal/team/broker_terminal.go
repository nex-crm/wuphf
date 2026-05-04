package team

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const terminalWriteDeadline = 10 * time.Second

var terminalUpgrader = websocket.Upgrader{
	CheckOrigin: terminalOriginAllowed,
}

func terminalOriginAllowed(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

// handleAgentTerminal upgrades to a websocket terminal feed.
//
// Path: /terminal/agents/{slug}?task={taskID}
//
// The websocket carries terminal bytes from the broker to xterm. Client
// messages are accepted so xterm resize events have a protocol home; today
// resize is advisory because headless exec streams and tmux capture replay
// cannot yet be resized per viewer.
func (b *Broker) handleAgentTerminal(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/terminal/agents/"))
	if slug == "" {
		http.Error(w, "missing agent slug", http.StatusBadRequest)
		return
	}
	taskID := strings.TrimSpace(r.URL.Query().Get("task"))

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	stream := b.AgentStream(slug)
	if stream == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	history, lines, unsubscribe := stream.subscribeTaskWithRecent(taskID)
	defer unsubscribe()

	for _, line := range history {
		_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteDeadline))
		if err := conn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
			return
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteDeadline))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
				return
			}
		}
	}
}
