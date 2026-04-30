package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/setup"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

// HTTP commands targeting the broker (and the launcher reconfigure path
// for session/provider switches). Each function returns a tea.Cmd that
// performs one POST and emits the corresponding tea.Msg. Co-locating
// them here keeps the wire shape and timeout/error handling visible in
// one file; the call sites in channel_commands.go just say "post X."
//
// The channelMentionAgents helper joins the static @mention defaults
// with dynamic office members; it lives here because @mention rendering
// happens in the same composer message-shape pipeline as broker posts.

func postToChannel(text string, replyTo string, channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"channel":  channel,
			"from":     "you",
			"content":  text,
			"tagged":   channelui.ExtractTagsFromText(text),
			"reply_to": strings.TrimSpace(replyTo),
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/messages", bytes.NewReader(body))
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				return channelPostDoneMsg{err: fmt.Errorf("broker returned %s", resp.Status)}
			}
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		return channelPostDoneMsg{}
	}
}

func channelMentionAgents(members []channelui.Member) []tui.AgentMention {
	defaults := []tui.AgentMention{
		{Slug: "all", Name: "All agents"},
		{Slug: "ceo", Name: "CEO"},
		{Slug: "pm", Name: "Product Manager"},
		{Slug: "fe", Name: "Frontend Engineer"},
		{Slug: "be", Name: "Backend Engineer"},
		{Slug: "ai", Name: "AI Engineer"},
		{Slug: "designer", Name: "Designer"},
		{Slug: "cmo", Name: "CMO"},
		{Slug: "cro", Name: "CRO"},
	}
	seen := make(map[string]bool, len(defaults))
	mentions := make([]tui.AgentMention, 0, len(defaults)+len(members))
	for _, ag := range defaults {
		seen[ag.Slug] = true
		mentions = append(mentions, ag)
	}
	for _, member := range members {
		if seen[member.Slug] {
			continue
		}
		seen[member.Slug] = true
		mentions = append(mentions, tui.AgentMention{Slug: member.Slug, Name: channelui.DisplayName(member.Slug)})
	}
	return mentions
}

func createSkill(description, channel string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{
			"action":      "create",
			"description": description,
			"channel":     channel,
		}
		body, _ := json.Marshal(payload)
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/skills", bytes.NewReader(body))
		if err != nil {
			return channelSkillsMsg{}
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSkillsMsg{}
		}
		defer resp.Body.Close()
		return channelSkillsMsg{}
	}
}

func invokeSkill(name string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/skills/"+name+"/invoke", nil)
		if err != nil {
			return channelSkillsMsg{}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSkillsMsg{}
		}
		defer resp.Body.Close()
		return channelSkillsMsg{}
	}
}

func resetDMSession(agent string, channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"agent":   agent,
			"channel": channel,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/reset-dm", bytes.NewReader(body))
		if err != nil {
			return channelResetDMDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelResetDMDoneMsg{err: err}
		}
		defer resp.Body.Close()
		var result struct {
			Removed int `json:"removed"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		return channelResetDMDoneMsg{removed: result.Removed}
	}
}

func resetTeamSession(oneOnOne bool) tea.Cmd {
	return func() tea.Msg {
		// Clear broker + Claude resume state and then rebuild the visible
		// team panes in place so reset does not leave dead panes behind.
		l, err := team.NewLauncher("")
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ResetSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		mode := team.SessionModeOffice
		agent := ""
		if oneOnOne {
			mode = team.SessionModeOneOnOne
		}
		if oneOnOne {
			return channelResetDoneMsg{notice: "Direct session reset. Agent pane reloaded in place.", sessionMode: mode, oneOnOneAgent: agent}
		}
		return channelResetDoneMsg{notice: "Office reset. Team panes reloaded in place.", sessionMode: mode, oneOnOneAgent: agent}
	}
}

func switchSessionMode(mode, agent string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"mode":  mode,
			"agent": agent,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/session-mode", bytes.NewReader(body))
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(resp.Body)
			return channelResetDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(raw)))}
		}
		var result struct {
			SessionMode   string `json:"session_mode"`
			OneOnOneAgent string `json:"one_on_one_agent"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			result.SessionMode = mode
			result.OneOnOneAgent = agent
		}

		l, err := team.NewLauncher("")
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ResetSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		switch team.NormalizeSessionMode(result.SessionMode) {
		case team.SessionModeOneOnOne:
			return channelResetDoneMsg{
				notice:        "Direct 1:1 with " + channelui.DisplayName(team.NormalizeOneOnOneAgent(result.OneOnOneAgent)) + " is ready.",
				sessionMode:   result.SessionMode,
				oneOnOneAgent: result.OneOnOneAgent,
			}
		default:
			return channelResetDoneMsg{
				notice:        "Office mode is ready.",
				sessionMode:   result.SessionMode,
				oneOnOneAgent: result.OneOnOneAgent,
			}
		}
	}
}

func switchFocusMode(enabled bool) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"focus_mode": enabled,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/focus-mode", bytes.NewReader(body))
		if err != nil {
			return nil
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		resp.Body.Close()
		return nil
	}
}

func applyTeamSetup() tea.Cmd {
	return func() tea.Msg {
		notice, err := setup.InstallLatestCLI(context.Background())
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		cfg, _ := config.Load()
		if current := strings.TrimSpace(os.Getenv("WUPHF_HEADLESS_PROVIDER")); current != "" {
			return channelInitDoneMsg{notice: notice + " Setup saved. Restart WUPHF to reload the " + current + " office runtime with the new configuration."}
		}
		if config.ResolveLLMProvider("") == "codex" || strings.TrimSpace(cfg.LLMProvider) == "codex" {
			return channelInitDoneMsg{notice: notice + " Codex was saved as the LLM provider. Restart WUPHF to launch the headless Codex office runtime."}
		}
		if config.ResolveLLMProvider("") == "opencode" || strings.TrimSpace(cfg.LLMProvider) == "opencode" {
			return channelInitDoneMsg{notice: notice + " Opencode was saved as the LLM provider. Restart WUPHF to launch the headless Opencode office runtime."}
		}
		l, err := team.NewLauncher("")
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelInitDoneMsg{err: err}
		}
		return channelInitDoneMsg{notice: notice + " Setup applied. Team reloaded with the new configuration."}
	}
}

func applyProviderSelection(providerName string) tea.Cmd {
	return func() tea.Msg {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			return channelInitDoneMsg{err: errors.New("choose a provider")}
		}

		cfg, _ := config.Load()
		currentProvider := config.ResolveLLMProvider("")
		cfg.LLMProvider = providerName
		if err := config.Save(cfg); err != nil {
			return channelInitDoneMsg{err: err}
		}

		if current := strings.TrimSpace(os.Getenv("WUPHF_HEADLESS_PROVIDER")); current != "" {
			return channelInitDoneMsg{notice: "Provider switched to " + providerName + ". Restart WUPHF to reload the office runtime with the new configuration."}
		}
		if providerName == "codex" {
			l, err := team.NewLauncher("")
			if err != nil {
				return channelInitDoneMsg{err: err}
			}
			if err := l.ReconfigureSession(); err != nil {
				return channelInitDoneMsg{err: err}
			}
			return channelInitDoneMsg{notice: "Provider switched to codex. Claude teammate panes were stopped. Restart WUPHF to launch the headless Codex office runtime."}
		}
		if providerName == "opencode" {
			l, err := team.NewLauncher("")
			if err != nil {
				return channelInitDoneMsg{err: err}
			}
			if err := l.ReconfigureSession(); err != nil {
				return channelInitDoneMsg{err: err}
			}
			return channelInitDoneMsg{notice: "Provider switched to opencode. Claude teammate panes were stopped. Restart WUPHF to launch the headless Opencode office runtime."}
		}
		if currentProvider == "codex" || currentProvider == "opencode" {
			return channelInitDoneMsg{notice: "Provider switched to " + providerName + ". Restart WUPHF to reload the office runtime with the new configuration."}
		}

		l, err := team.NewLauncher("")
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelInitDoneMsg{err: err}
		}
		return channelInitDoneMsg{notice: "Provider switched to " + providerName + ". Team reloaded with the new configuration."}
	}
}
