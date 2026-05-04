package team

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

var terminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
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

	for _, line := range stream.recentTask(taskID) {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(terminalLine(line))); err != nil {
			return
		}
	}

	lines, unsubscribe := stream.subscribeTask(taskID)
	defer unsubscribe()

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
			if err := conn.WriteMessage(websocket.TextMessage, []byte(terminalLine(line))); err != nil {
				return
			}
		}
	}
}

func terminalLine(line string) string {
	if line == "" {
		return ""
	}
	if strings.ContainsAny(line, "\r\n") {
		return line
	}
	return line + "\r\n"
}
