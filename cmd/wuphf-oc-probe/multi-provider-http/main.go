// multi-provider-http is a pure HTTP orchestrator for testing per-agent
// providers. It does NOT spawn a launcher — the caller must start a real
// `wuphf` binary in the background (e.g. via tmux) and point the orchestrator
// at its broker via BROKER_URL + BROKER_TOKEN env. This keeps MCP server
// resolution (os.Executable) pointing at the real wuphf, so claude/codex
// subprocesses can actually connect.
//
// Hire matrix: 2 claude-code + 2 codex + 2 openclaw = 6 agents. Then run
// channel + DM conversations, CEO-assigned tasks, and record everything.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type agentSpec struct {
	slug         string
	name, role   string
	providerKind string
	model        string
}

type testAgent struct {
	spec      agentSpec
	sessionID string // for openclaw
}

var testAgents = []agentSpec{
	{slug: "pm-alpha", name: "PM Alpha", role: "Senior PM", providerKind: "claude-code"},
	{slug: "pm-beta", name: "PM Beta", role: "PM Launch", providerKind: "claude-code"},
	{slug: "eng-alpha", name: "Eng Alpha", role: "Staff Engineer", providerKind: "codex", model: "gpt-5.4"},
	{slug: "eng-beta", name: "Eng Beta", role: "Infrastructure", providerKind: "codex", model: "gpt-5.4"},
	{slug: "research-oc1", name: "Research OC One", role: "Market Research", providerKind: "openclaw", model: "openai-codex/gpt-5.4"},
	{slug: "research-oc2", name: "Research OC Two", role: "Deep Research", providerKind: "openclaw", model: "openai-codex/gpt-5.4"},
}

var brokerURL string
var brokerToken string

func main() {
	brokerURL = strings.TrimRight(os.Getenv("BROKER_URL"), "/")
	if brokerURL == "" {
		brokerURL = "http://127.0.0.1:7890"
	}
	brokerToken = os.Getenv("BROKER_TOKEN")
	if brokerToken == "" {
		die("BROKER_TOKEN env required (the running wuphf's broker token)")
	}

	// Sanity: broker responds
	waitHealth(30 * time.Second)
	fmt.Printf("orchestrator connected to %s\n", brokerURL)

	// Stream broker messages so we see all agent activity in real time.
	go streamMessageLog()

	// ─── phase 1: hire + add to #general ─────────────────────────────
	fmt.Println("\n═══ PHASE 1: Hire 2× claude-code + 2× codex + 2× openclaw ═══")
	var live []testAgent
	for _, spec := range testAgents {
		body := map[string]any{
			"action":   "create",
			"slug":     spec.slug,
			"name":     spec.name,
			"role":     spec.role,
			"provider": providerBlock(spec),
		}
		resp, err := brokerPOST("/office-members", body)
		if err != nil {
			fmt.Printf("  FAIL hire %s (%s): %v — continuing\n", spec.slug, spec.providerKind, err)
			continue
		}
		fmt.Printf("  ✓ hired @%s (%s)\n", spec.slug, spec.providerKind)
		if _, err := brokerPOST("/channel-members", map[string]any{
			"channel": "general", "action": "add", "slug": spec.slug,
		}); err != nil {
			fmt.Printf("    WARN add to #general: %v\n", err)
		}
		live = append(live, testAgent{spec: spec, sessionID: extractSessionKey(resp)})
	}

	time.Sleep(3 * time.Second) // let office changes propagate

	if len(live) == 0 {
		die("no agents hired; aborting")
	}

	// ─── phase 2: channel fan-out ────────────────────────────────────
	fmt.Println("\n═══ PHASE 2: Channel @mention of all three PROVIDER types ═══")
	// One @mention per provider-kind (pm-alpha, eng-alpha, research-oc1) so
	// each kind demonstrates reply-routing at least once.
	tagged := []string{}
	for _, a := range live {
		if a.spec.slug == "pm-alpha" || a.spec.slug == "eng-alpha" || a.spec.slug == "research-oc1" {
			tagged = append(tagged, a.spec.slug)
		}
	}
	prompt := "Brief check-in: respond with ONE short sentence identifying yourself by slug. " +
		"Keep it under 20 words. Do not add tool calls or questions."
	beforeCh := len(fetchMessages())
	mustPost(postMessage("general", prompt, tagged))
	fmt.Printf("→ #general (@%s): %q\n", strings.Join(tagged, " @"), truncate(prompt, 120))
	channelReplies := waitForReplies(beforeCh, slugsToSet(tagged), "general", 180*time.Second)
	reportReplies("channel", channelReplies)

	// ─── phase 3: DM each hired agent ────────────────────────────────
	fmt.Println("\n═══ PHASE 3: DM each agent ═══")
	dmPrompts := map[string]string{
		"pm-alpha":     "(DM) Name one KPI a new startup should obsess over. One sentence.",
		"pm-beta":      "(DM) What's a common launch pitfall? One sentence.",
		"eng-alpha":    "(DM) In one sentence, what's a sensible Go module layout for a small service?",
		"eng-beta":     "(DM) Name one reliability anti-pattern in infra. One sentence.",
		"research-oc1": "(DM) In one sentence, define TAM.",
		"research-oc2": "(DM) In one sentence, what's a 'beachhead market'?",
	}
	for _, a := range live {
		prompt := dmPrompts[a.spec.slug]
		if prompt == "" {
			continue
		}
		dmSlug := openDM(a.spec.slug)
		beforeDM := len(fetchMessages())
		mustPost(postMessage(dmSlug, prompt, nil))
		fmt.Printf("→ DM %s: %q\n", dmSlug, truncate(prompt, 100))
		replies := waitForReplies(beforeDM, map[string]bool{a.spec.slug: false}, dmSlug, 120*time.Second)
		reportReplies("dm-"+a.spec.slug, replies)
	}

	// ─── phase 4: CEO-assigns-work ───────────────────────────────────
	// CEO is baked into the office; we ask CEO to route a user request to a
	// specific agent by @mention. This exercises two-hop reasoning: human →
	// CEO → specialist. Watch for CEO's routing message followed by the
	// specialist's reply, both in #general.
	fmt.Println("\n═══ PHASE 4: CEO assigns work ═══")
	ceoPrompt := "@ceo please ask one of your product managers (pm-alpha or pm-beta) to give us a one-sentence " +
		"positioning statement for a new CRM tool. Just delegate it; they'll answer."
	beforeCEO := len(fetchMessages())
	mustPost(postMessage("general", ceoPrompt, []string{"ceo"}))
	fmt.Printf("→ #general (@ceo): %q\n", truncate(ceoPrompt, 120))
	// We don't know which PM CEO picks, so wait for ANY reply from ceo + at
	// least one reply from a PM in the next 180s.
	time.Sleep(180 * time.Second)
	postCEO := fetchMessages()
	fmt.Printf("  messages after CEO prompt (last %d):\n", min(10, len(postCEO)-beforeCEO))
	for _, m := range postCEO[beforeCEO:] {
		fmt.Printf("    [%s] %s → #%s: %s\n", m.Source, m.From, m.Channel, truncate(m.Content, 140))
	}

	// ─── phase 5: cleanup ────────────────────────────────────────────
	fmt.Println("\n═══ PHASE 5: Cleanup ═══")
	for _, a := range live {
		if _, err := brokerPOST("/office-members", map[string]any{
			"action": "remove", "slug": a.spec.slug,
		}); err != nil {
			fmt.Printf("  WARN remove %s: %v\n", a.spec.slug, err)
		} else {
			fmt.Printf("  ✓ removed @%s\n", a.spec.slug)
		}
	}

	fmt.Println("\n═══ ORCHESTRATOR DONE ═══")
}

// ─── helpers ────────────────────────────────────────────────────────────────

func providerBlock(spec agentSpec) map[string]any {
	p := map[string]any{"kind": spec.providerKind, "model": spec.model}
	if spec.providerKind == "openclaw" {
		p["openclaw"] = map[string]any{} // auto-create session
	}
	return p
}

func extractSessionKey(raw string) string {
	var out struct {
		Member struct {
			Provider struct {
				Openclaw *struct {
					SessionKey string `json:"session_key"`
				} `json:"openclaw"`
			} `json:"provider"`
		} `json:"member"`
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out.Member.Provider.Openclaw != nil {
		return out.Member.Provider.Openclaw.SessionKey
	}
	return ""
}

func slugsToSet(slugs []string) map[string]bool {
	m := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		m[s] = false
	}
	return m
}

func reportReplies(label string, got map[string]string) {
	if len(got) == 0 {
		fmt.Printf("  %s: NO REPLIES\n", label)
		return
	}
	for slug, content := range got {
		fmt.Printf("  %s ← @%s: %s\n", label, slug, truncate(content, 200))
	}
}

func streamMessageLog() {
	seen := map[string]bool{}
	for {
		for _, m := range fetchMessages() {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			src := m.Source
			if src == "" {
				src = "-"
			}
			fmt.Printf("    ◦ [%s] %s → #%s: %s\n", src, m.From, m.Channel, truncate(m.Content, 120))
		}
		time.Sleep(2 * time.Second)
	}
}

type messageRow struct {
	ID      string   `json:"id"`
	From    string   `json:"from"`
	Content string   `json:"content"`
	Channel string   `json:"channel"`
	Source  string   `json:"source"`
	Tagged  []string `json:"tagged"`
}

func fetchMessages() []messageRow {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, brokerURL+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+brokerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Messages []messageRow `json:"messages"`
	}
	_ = json.Unmarshal(raw, &out)
	return out.Messages
}

func postMessage(channel, content string, tagged []string) error {
	body := map[string]any{"from": "human", "content": content, "channel": channel, "tagged": tagged}
	_, err := brokerPOST("/messages", body)
	return err
}

func openDM(slug string) string {
	resp, err := brokerPOST("/channels/dm", map[string]any{"agent": slug})
	if err != nil {
		return "dm-" + slug
	}
	var out struct {
		Channel struct{ Slug string } `json:"channel"`
		Slug    string                `json:"slug"`
	}
	_ = json.Unmarshal([]byte(resp), &out)
	if out.Channel.Slug != "" {
		return out.Channel.Slug
	}
	if out.Slug != "" {
		return out.Slug
	}
	return "dm-" + slug
}

func waitForReplies(before int, want map[string]bool, channel string, timeout time.Duration) map[string]string {
	got := map[string]string{}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msgs := fetchMessages()
		for _, m := range msgs[min(before, len(msgs)):] {
			if m.Channel != channel {
				continue
			}
			if _, tracked := want[m.From]; !tracked {
				continue
			}
			if _, seen := got[m.From]; seen {
				continue
			}
			got[m.From] = m.Content
			want[m.From] = true
		}
		done := true
		for _, v := range want {
			if !v {
				done = false
				break
			}
		}
		if done {
			return got
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Fprintf(os.Stderr, "  ! timeout on %s; partial=%v\n", channel, got)
	return got
}

func mustPost(err error) {
	if err != nil {
		die("post: %v", err)
	}
}

func brokerPOST(path string, body any) (string, error) {
	data, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, brokerURL+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+brokerToken)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return string(raw), nil
}

func waitHealth(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, brokerURL+"/health", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	die("broker unreachable at %s", brokerURL)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
