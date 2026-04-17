package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/team"
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
func newBrokerRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, normalizeBrokerURL(url), body)
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
		req, err := newBrokerRequest(http.MethodGet, url, nil)
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
			Messages []brokerMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMsg{}
		}
		return channelMsg{messages: result.Messages}
	}
}

func pollMembers(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/members?channel="+channel, nil)
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
			Members []channelMember `json:"members"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMembersMsg{}
		}
		return channelMembersMsg{members: result.Members}
	}
}

func pollOfficeMembers() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/office-members", nil)
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
			Members []officeMemberInfo `json:"members"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelOfficeMembersMsg{}
		}
		return channelOfficeMembersMsg{members: result.Members}
	}
}

func pollChannels() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/channels", nil)
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
			Channels []channelInfo `json:"channels"`
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
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channels/dm", bytes.NewReader(body))
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
		resp, err := client.Get(brokerURL("/health"))
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
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channels", bytes.NewReader(body))
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
			notice = fmt.Sprintf("Created #%s.", normalizeSidebarSlug(slug))
		case "remove":
			notice = fmt.Sprintf("Removed #%s.", normalizeSidebarSlug(slug))
		}
		return channelPostDoneMsg{notice: notice, action: action, slug: normalizeSidebarSlug(slug)}
	}
}

func mutateChannelMember(channel, action, slug string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":  action,
			"channel": channel,
			"slug":    slug,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channel-members", bytes.NewReader(body))
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
		notice := fmt.Sprintf("%s @%s in #%s.", titleCaser.String(action), normalizeSidebarSlug(slug), normalizeSidebarSlug(channel))
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
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/office-members", bytes.NewReader(body))
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
		notice := fmt.Sprintf("%s @%s.", titleCaser.String(action), normalizeSidebarSlug(slug))
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
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/tasks", bytes.NewReader(body))
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
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/usage", nil)
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

		var result channelUsageState
		if err := json.Unmarshal(body, &result); err != nil {
			return channelUsageMsg{}
		}
		if result.Agents == nil {
			result.Agents = make(map[string]channelUsageTotals)
		}
		return channelUsageMsg{usage: result}
	}
}

func pollTasks(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/tasks?channel="+channel, nil)
		if err != nil {
			return channelTasksMsg{}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return channelTasksMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelTasksMsg{}
		}
		var result struct {
			Tasks []channelTask `json:"tasks"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelTasksMsg{}
		}
		return channelTasksMsg{tasks: result.Tasks}
	}
}

func pollSkills(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/skills?channel="+channel, nil)
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
			Skills []channelSkill `json:"skills"`
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
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/actions", nil)
		if err != nil {
			return channelActionsMsg{}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return channelActionsMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelActionsMsg{}
		}
		var result struct {
			Actions []channelAction `json:"actions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelActionsMsg{}
		}
		return channelActionsMsg{actions: result.Actions}
	}
}

func pollSignals() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/signals", nil)
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
			Signals []channelSignal `json:"signals"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSignalsMsg{}
		}
		return channelSignalsMsg{signals: result.Signals}
	}
}

func pollDecisions() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/decisions", nil)
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
			Decisions []channelDecision `json:"decisions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelDecisionsMsg{}
		}
		return channelDecisionsMsg{decisions: result.Decisions}
	}
}

func pollWatchdogs() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/watchdogs", nil)
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
			Watchdogs []channelWatchdog `json:"watchdogs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelWatchdogsMsg{}
		}
		return channelWatchdogsMsg{alerts: result.Watchdogs}
	}
}

func pollScheduler() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/scheduler", nil)
		if err != nil {
			return channelSchedulerMsg{}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return channelSchedulerMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelSchedulerMsg{}
		}
		var result struct {
			Jobs []channelSchedulerJob `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSchedulerMsg{}
		}
		return channelSchedulerMsg{jobs: result.Jobs}
	}
}

func pollRequests(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/requests?channel="+channel, nil)
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
			Requests []channelInterview `json:"requests"`
			Pending  *channelInterview  `json:"pending"`
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
		req, _ := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/requests", bytes.NewReader(body))
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelInterruptDoneMsg{err: err}
		}
		defer resp.Body.Close()
		return channelInterruptDoneMsg{}
	}
}

func postInterviewAnswer(interview channelInterview, choiceID, choiceText, customText string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"id":          interview.ID,
			"choice_id":   choiceID,
			"choice_text": choiceText,
			"custom_text": customText,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/requests/answer", bytes.NewReader(body))
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
