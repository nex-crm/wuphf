package main

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkOfficeViewportVirtualizedHot(b *testing.B) {
	benchmarkOfficeViewport(b, 2000, true, func(m channelModel, contentWidth, msgH int) {
		_ = m.currentMainViewportLines(contentWidth, msgH)
	})
}

func BenchmarkOfficeViewportVirtualizedCold(b *testing.B) {
	benchmarkOfficeViewport(b, 2000, false, func(m channelModel, contentWidth, msgH int) {
		resetViewportBenchmarkCaches()
		_ = m.currentMainViewportLines(contentWidth, msgH)
	})
}

func BenchmarkOfficeViewportFullRender(b *testing.B) {
	benchmarkOfficeViewport(b, 2000, false, func(m channelModel, contentWidth, msgH int) {
		full := append(buildOfficeMessageLines(m.messages, m.expandedThreads, contentWidth, m.threadsDefaultExpand, m.unreadAnchorID, m.unreadCount), buildLiveWorkLines(m.members, m.tasks, m.actions, contentWidth, "")...)
		_, _, _, _ = sliceRenderedLines(full, msgH, m.scroll)
	})
}

func benchmarkOfficeViewport(b *testing.B, messageCount int, warmCache bool, fn func(channelModel, int, int)) {
	b.Helper()
	resetViewportBenchmarkCaches()

	m := benchmarkViewportModel(messageCount)
	layout := computeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)
	_, msgH, _ := m.mainPanelGeometry(layout.MainW, layout.ContentH)
	contentWidth := layout.MainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}

	if warmCache {
		fn(m, contentWidth, msgH)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(m, contentWidth, msgH)
	}
}

func benchmarkViewportModel(messageCount int) channelModel {
	m := newChannelModel(false)
	m.width = 140
	m.height = 34
	m.activeApp = officeAppMessages
	m.members = []channelMember{
		{Slug: "fe", Name: "Frontend Engineer", LastMessage: "Landing the next slice"},
		{Slug: "pm", Name: "Product Manager", LastMessage: "Reviewing launch decisions"},
	}
	m.tasks = []channelTask{{
		ID:            "task-bench-1",
		Title:         "Ship onboarding",
		Status:        "in_progress",
		Owner:         "fe",
		ExecutionMode: "local_worktree",
		WorktreePath:  "/tmp/wuphf-task-bench-1",
		CreatedBy:     "ceo",
		CreatedAt:     time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
		UpdatedAt:     time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}}
	m.actions = []channelAction{{
		Kind:      "external_build",
		Actor:     "fe",
		Summary:   "Build the office UI",
		CreatedAt: time.Date(2026, 4, 8, 10, 5, 0, 0, time.UTC).Format(time.RFC3339),
	}}

	for i := 0; i < messageCount; i++ {
		from := []string{"ceo", "fe", "pm", "designer"}[i%4]
		msg := brokerMessage{
			ID:        fmt.Sprintf("msg-%04d", i),
			From:      from,
			Content:   fmt.Sprintf("Long benchmark message %04d keeps the transcript heavy enough that viewport virtualization and block caching matter during scroll and repaint work.", i),
			Timestamp: time.Date(2026, 4, 8, 8+(i/60), i%60, 0, 0, time.UTC).Format(time.RFC3339),
		}
		if i > 0 && i%5 == 0 {
			msg.ReplyTo = fmt.Sprintf("msg-%04d", i-1)
		}
		m.messages = append(m.messages, msg)
	}
	return m
}

func resetViewportBenchmarkCaches() {
	channelRenderCache.mu.Lock()
	defer channelRenderCache.mu.Unlock()
	channelRenderCache.threaded = make(map[uint64][]threadedMessage)
	channelRenderCache.blocks = make(map[uint64][]renderedLine)
	channelRenderCache.mainLines = make(map[uint64][]renderedLine)
}
