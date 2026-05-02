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
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/setup"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

func currentBrokerAuthToken() string {
	if token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN")); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("NEX_BROKER_TOKEN")); token != "" {
		return token
	}
	path := strings.TrimSpace(brokerTokenPath)
	if path == "" || path == brokeraddr.DefaultTokenFile {
		path = brokeraddr.ResolveTokenFile()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func brokerBaseURL() string {
	return brokeraddr.ResolveBaseURL()
}

func brokerURL(path string) string {
	return brokerBaseURL() + path
}

func normalizeBrokerURL(raw string) string {
	base := brokerBaseURL()
	raw = strings.Replace(raw, "http://127.0.0.1:7890", base, 1)
	raw = strings.Replace(raw, "http://localhost:7890", base, 1)
	return raw
}

// newBrokerRequest creates an HTTP request with the broker auth header.
func newBrokerRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, normalizeBrokerURL(url), body)
	if err != nil {
		return nil, err
	}
	if brokerAuthToken := currentBrokerAuthToken(); brokerAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+brokerAuthToken)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func pollBroker(sinceID string, channel string) tea.Cmd {
	return func() tea.Msg {
		url := brokerURL("/messages?limit=100&channel=" + channel)
		if sinceID != "" {
			url += "&since_id=" + sinceID
		}
		req, err := newBrokerRequest(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			return channelMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelMsg{}
		}

		var result struct {
			Messages []channelui.BrokerMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMsg{}
		}
		return channelMsg{messages: result.Messages}
	}
}

func pollMembers(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/members?channel="+channel, nil)
		if err != nil {
			return channelMembersMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelMembersMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelMembersMsg{}
		}

		var result struct {
			Members []channelui.Member `json:"members"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMembersMsg{}
		}
		return channelMembersMsg{members: result.Members}
	}
}

func pollOfficeMembers() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/office-members", nil)
		if err != nil {
			return channelOfficeMembersMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelOfficeMembersMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelOfficeMembersMsg{}
		}

		var result struct {
			Members []channelui.OfficeMember `json:"members"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelOfficeMembersMsg{}
		}
		return channelOfficeMembersMsg{members: result.Members}
	}
}

func pollChannels() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/channels", nil)
		if err != nil {
			return channelChannelsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelChannelsMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelChannelsMsg{}
		}

		var result struct {
			Channels []channelui.ChannelInfo `json:"channels"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelChannelsMsg{}
		}
		return channelChannelsMsg{channels: result.Channels}
	}
}

// createDMChannel calls POST /channels/dm to open or find a 1:1 DM with agentSlug.
func createDMChannel(agentSlug string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"members": []string{"human", agentSlug},
			"type":    "direct",
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/channels/dm", bytes.NewReader(body))
		if err != nil {
			return channelDMCreatedMsg{err: err, agentSlug: agentSlug}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelDMCreatedMsg{err: err, agentSlug: agentSlug}
		}
		defer resp.Body.Close()
		var result struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelDMCreatedMsg{err: err, agentSlug: agentSlug}
		}
		return channelDMCreatedMsg{slug: result.Slug, name: result.Name, agentSlug: agentSlug}
	}
}

func pollHealth() tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 1200 * time.Millisecond}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, brokerURL("/health"), nil)
		if err != nil {
			return channelHealthMsg{}
		}
		resp, err := client.Do(req)
		if err != nil {
			return channelHealthMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelHealthMsg{}
		}
		var result struct {
			Status        string `json:"status"`
			SessionMode   string `json:"session_mode"`
			OneOnOneAgent string `json:"one_on_one_agent"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelHealthMsg{Connected: true}
		}
		return channelHealthMsg{
			Connected:     true,
			SessionMode:   result.SessionMode,
			OneOnOneAgent: result.OneOnOneAgent,
		}
	}
}

func mutateChannel(action, slug, description string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":      action,
			"slug":        slug,
			"name":        slug,
			"description": description,
			"created_by":  "you",
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/channels", bytes.NewReader(body))
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
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		if err := reconfigureLiveOfficeSession(); err != nil {
			return channelPostDoneMsg{err: err}
		}
		notice := ""
		switch action {
		case "create":
			notice = fmt.Sprintf("Created #%s.", channelui.NormalizeSidebarSlug(slug))
		case "remove":
			notice = fmt.Sprintf("Removed #%s.", channelui.NormalizeSidebarSlug(slug))
		}
		return channelPostDoneMsg{notice: notice, action: action, slug: channelui.NormalizeSidebarSlug(slug)}
	}
}

func mutateChannelMember(channel, action, slug string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":  action,
			"channel": channel,
			"slug":    slug,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/channel-members", bytes.NewReader(body))
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
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		if err := reconfigureLiveOfficeSession(); err != nil {
			return channelPostDoneMsg{err: err}
		}
		notice := fmt.Sprintf("%s @%s in #%s.", titleCaser.String(action), channelui.NormalizeSidebarSlug(slug), channelui.NormalizeSidebarSlug(channel))
		return channelPostDoneMsg{notice: notice}
	}
}

func mutateOfficeMember(action, slug, name string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":     action,
			"slug":       slug,
			"name":       name,
			"role":       name,
			"created_by": "you",
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/office-members", bytes.NewReader(body))
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		if err := reconfigureLiveOfficeSession(); err != nil {
			return channelPostDoneMsg{err: err}
		}
		notice := fmt.Sprintf("%s @%s.", titleCaser.String(action), channelui.NormalizeSidebarSlug(slug))
		return channelPostDoneMsg{notice: notice}
	}
}

func reconfigureLiveOfficeSession() error {
	l, err := team.NewLauncher("")
	if err != nil {
		return err
	}
	return l.ReconfigureSession()
}

func mutateTask(action, taskID, owner, channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":     action,
			"channel":    channel,
			"id":         taskID,
			"owner":      owner,
			"created_by": "you",
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/tasks", bytes.NewReader(body))
		if err != nil {
			return channelTaskMutationDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelTaskMutationDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return channelTaskMutationDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		label := map[string]string{
			"claim":    "Task claimed.",
			"assign":   "Task assigned.",
			"complete": "Task completed.",
			"review":   "Task moved into review.",
			"approve":  "Task approved.",
			"block":    "Task marked blocked.",
			"release":  "Task released.",
		}[action]
		if label == "" {
			label = "Task updated."
		}
		return channelTaskMutationDoneMsg{notice: label}
	}
}

func pollUsage() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/usage", nil)
		if err != nil {
			return channelUsageMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelUsageMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelUsageMsg{}
		}

		var result channelui.UsageState
		if err := json.Unmarshal(body, &result); err != nil {
			return channelUsageMsg{}
		}
		if result.Agents == nil {
			result.Agents = make(map[string]channelui.UsageTotals)
		}
		return channelUsageMsg{usage: result}
	}
}

func pollTasks(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/tasks?channel="+channel, nil)
		if err != nil {
			return channelTasksMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelTasksMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelTasksMsg{}
		}
		var result struct {
			Tasks []channelui.Task `json:"tasks"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelTasksMsg{}
		}
		return channelTasksMsg{tasks: result.Tasks}
	}
}

func pollSkills(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/skills?channel="+channel, nil)
		if err != nil {
			return channelSkillsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSkillsMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelSkillsMsg{}
		}
		var result struct {
			Skills []channelui.Skill `json:"skills"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSkillsMsg{}
		}
		return channelSkillsMsg{skills: result.Skills}
	}
}

func pollOfficeLedger() tea.Cmd {
	return tea.Batch(
		pollActions(),
		pollSignals(),
		pollDecisions(),
		pollWatchdogs(),
		pollScheduler(),
	)
}

func pollActions() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/actions", nil)
		if err != nil {
			return channelActionsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelActionsMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelActionsMsg{}
		}
		var result struct {
			Actions []channelui.Action `json:"actions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelActionsMsg{}
		}
		return channelActionsMsg{actions: result.Actions}
	}
}

func pollSignals() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/signals", nil)
		if err != nil {
			return channelSignalsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSignalsMsg{}
		}
		defer resp.Body.Close()
		var result struct {
			Signals []channelui.Signal `json:"signals"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSignalsMsg{}
		}
		return channelSignalsMsg{signals: result.Signals}
	}
}

func pollDecisions() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/decisions", nil)
		if err != nil {
			return channelDecisionsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelDecisionsMsg{}
		}
		defer resp.Body.Close()
		var result struct {
			Decisions []channelui.Decision `json:"decisions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelDecisionsMsg{}
		}
		return channelDecisionsMsg{decisions: result.Decisions}
	}
}

func pollWatchdogs() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/watchdogs", nil)
		if err != nil {
			return channelWatchdogsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelWatchdogsMsg{}
		}
		defer resp.Body.Close()
		var result struct {
			Watchdogs []channelui.Watchdog `json:"watchdogs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelWatchdogsMsg{}
		}
		return channelWatchdogsMsg{alerts: result.Watchdogs}
	}
}

func pollScheduler() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/scheduler", nil)
		if err != nil {
			return channelSchedulerMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSchedulerMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelSchedulerMsg{}
		}
		var result struct {
			Jobs []channelui.SchedulerJob `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSchedulerMsg{}
		}
		return channelSchedulerMsg{jobs: result.Jobs}
	}
}

func pollRequests(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, "http://127.0.0.1:7890/requests?channel="+channel, nil)
		if err != nil {
			return channelRequestsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelRequestsMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelRequestsMsg{}
		}

		var result struct {
			Requests []channelui.Interview `json:"requests"`
			Pending  *channelui.Interview  `json:"pending"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelRequestsMsg{}
		}
		return channelRequestsMsg{requests: result.Requests, pending: result.Pending}
	}
}

func postHumanInterrupt(channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":   "create",
			"from":     "human",
			"channel":  channel,
			"question": "Human pressed Esc — all work paused. What should the team do now?",
			"kind":     "interrupt",
			"blocking": true,
			"required": true,
			"options": []map[string]string{
				{"id": "resume", "label": "Resume — carry on where you left off"},
				{"id": "stop", "label": "Stop — drop current tasks and wait"},
				{"id": "redirect", "label": "Redirect — I'll type new instructions"},
			},
		})
		req, _ := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/requests", bytes.NewReader(body))
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelInterruptDoneMsg{err: err}
		}
		defer resp.Body.Close()
		return channelInterruptDoneMsg{}
	}
}

func cancelRequest(interview channelui.Interview) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action": "cancel",
			"id":     interview.ID,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/requests", bytes.NewReader(body))
		if err != nil {
			return channelCancelDoneMsg{requestID: interview.ID, err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelCancelDoneMsg{requestID: interview.ID, err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				return channelCancelDoneMsg{requestID: interview.ID, err: fmt.Errorf("broker returned %s", resp.Status)}
			}
			return channelCancelDoneMsg{requestID: interview.ID, err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		return channelCancelDoneMsg{requestID: interview.ID}
	}
}

func postInterviewAnswer(interview channelui.Interview, choiceID, choiceText, customText string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"id":          interview.ID,
			"choice_id":   choiceID,
			"choice_text": choiceText,
			"custom_text": customText,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, "http://127.0.0.1:7890/requests/answer", bytes.NewReader(body))
		if err != nil {
			return channelInterviewAnswerDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelInterviewAnswerDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				return channelInterviewAnswerDoneMsg{err: fmt.Errorf("broker returned %s", resp.Status)}
			}
			return channelInterviewAnswerDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		return channelInterviewAnswerDoneMsg{}
	}
}

// ─── POST commands (skill, session-mode, provider, message) ────────────
//
// Functions below merged from the former channel_client.go. They are
// also broker HTTP calls — the split was a discoverability bug: a
// reader looking for "the file with broker calls" wouldn't know
// whether to look in channel_broker.go or channel_client.go.
//
// These differ from the polls/mutates above in that they're command-
// shaped (single POST, optional callback into team.Launcher for
// session-mode and provider switches) rather than read-shaped.

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

		cfg, err := config.Load()
		if err != nil {
			return channelInitDoneMsg{err: fmt.Errorf("load config: %w", err)}
		}
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
