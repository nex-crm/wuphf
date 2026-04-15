// wuphf-oc-probe/pack is the "pack parity" smoke test. It proves that a
// WUPHF install with TWO OpenClaw bridge bindings behaves like a real pack:
//
//   - Both bridged sessions register as office members (@mentionable in #general).
//   - A channel post that @mentions both agents fans out and each replies.
//   - A 1:1 DM to either bridged slug is routed and the agent replies,
//     without an @mention (DM partner is inferred from the channel members).
//
// Run with:
//
//	OPENCLAW_TOKEN=... go run ./cmd/wuphf-oc-probe/pack
//
// If OPENCLAW_TOKEN is unset, the token is read from ~/.openclaw/openclaw.json.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
	"github.com/nex-crm/wuphf/internal/team"
)

type packMember struct {
	slug       string
	display    string
	sessionKey string
}

func main() {
	token := resolveToken()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	realIdentityPath := config.ResolveOpenclawIdentityPath()
	identity, err := openclaw.LoadOrCreateDeviceIdentity(realIdentityPath)
	if err != nil {
		die("identity: %v", err)
	}

	// Create two fresh OpenClaw sessions — each gets its own conversation
	// history, so the two bridged slugs behave as distinct agents from the
	// user's perspective even though they share the same model/agent.
	conn, err := openclaw.Dial(ctx, openclaw.Config{URL: "ws://127.0.0.1:18789", Token: token, Identity: identity})
	if err != nil {
		die("dial openclaw: %v", err)
	}
	members := []packMember{
		{slug: "pm-bot", display: "PM Bot"},
		{slug: "eng-bot", display: "Eng Bot"},
	}
	runID := fmt.Sprint(time.Now().UnixNano())
	for i, m := range members {
		raw, err := conn.Call(ctx, "sessions.create", map[string]any{
			"agentId": "main",
			"label":   "wuphf-pack-" + m.slug + "-" + runID,
		})
		if err != nil {
			die("sessions.create for %s: %v", m.slug, err)
		}
		var out struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.Key == "" {
			die("sessions.create for %s returned no key: %s", m.slug, string(raw))
		}
		members[i].sessionKey = out.Key
		fmt.Printf("created session for %s: %s\n", m.slug, out.Key)
	}
	conn.Close()

	// Seed a temporary WUPHF HOME so broker state doesn't clash with a real
	// install. Point identity + token back at the paired daemon.
	tmpHome, err := os.MkdirTemp("", "wuphf-oc-pack-*")
	if err != nil {
		die("tmp home: %v", err)
	}
	defer os.RemoveAll(tmpHome)
	os.Setenv("HOME", tmpHome)
	os.MkdirAll(filepath.Join(tmpHome, ".wuphf"), 0o700)
	os.Setenv("WUPHF_OPENCLAW_IDENTITY_PATH", realIdentityPath)
	os.Setenv("WUPHF_OPENCLAW_TOKEN", token)

	bindings := make([]config.OpenclawBridgeBinding, len(members))
	for i, m := range members {
		bindings[i] = config.OpenclawBridgeBinding{
			SessionKey:  m.sessionKey,
			Slug:        m.slug,
			DisplayName: m.display,
		}
	}
	if err := config.Save(config.Config{
		OpenclawGatewayURL: "ws://127.0.0.1:18789",
		OpenclawBridges:    bindings,
	}); err != nil {
		die("save config: %v", err)
	}

	// Boot a broker + start the real bridge via the production bootstrap path.
	broker := team.NewBroker()
	bridge, err := team.StartOpenclawBridgeFromConfig(ctx, broker)
	if err != nil {
		die("start bridge: %v", err)
	}
	if bridge == nil {
		die("bootstrap returned nil bridge — bindings not persisted?")
	}
	defer bridge.Stop()
	// The router that forwards @mentions and DMs lives next to the bridge in
	// production via launcher.go:3295. Start the exact same goroutine here
	// so the probe exercises the real end-to-end routing path.
	team.StartOpenclawRouter(ctx, broker, bridge)
	fmt.Println("bridge + router started")

	time.Sleep(500 * time.Millisecond)
	for _, m := range members {
		if !bridge.HasSlug(m.slug) {
			die("bridge does not recognize slug %q", m.slug)
		}
	}

	// 1. Office-member registration check (both agents must show up in the
	// sidebar + @mention autocomplete).
	gotMembers := map[string]bool{}
	for _, om := range broker.OfficeMembers() {
		for _, want := range members {
			if om.Slug == want.slug {
				fmt.Printf("  MEMBER: %q name=%q role=%q createdBy=%q\n", om.Slug, om.Name, om.Role, om.CreatedBy)
				gotMembers[om.Slug] = true
			}
		}
	}
	for _, m := range members {
		if !gotMembers[m.slug] {
			die("bridged slug %q never registered as an office member", m.slug)
		}
	}

	// 2. Channel test: post to #general tagging both agents. Both must reply.
	channelPrompt := "hello both of you — reply with exactly the single word: pong"
	before := len(broker.AllMessages())
	if _, err := broker.PostMessage("human", "general", channelPrompt, []string{members[0].slug, members[1].slug}, ""); err != nil {
		die("post to #general: %v", err)
	}
	fmt.Printf("\nSEND #general (@%s @%s): %q\n", members[0].slug, members[1].slug, channelPrompt)
	channelReplies := waitForReplies(broker, before, map[string]bool{members[0].slug: false, members[1].slug: false}, "general", 30*time.Second)
	for slug, reply := range channelReplies {
		fmt.Printf("  RECV #general from %s: %q\n", slug, truncate(reply, 140))
	}

	// 3. DM test: open a 1:1 DM per agent, post a distinct question, expect a
	// distinct reply. No @mentions — DM partner resolution must do the work.
	dmPrompts := map[string]string{
		members[0].slug: "reply with exactly the single word: alpha",
		members[1].slug: "reply with exactly the single word: bravo",
	}
	for _, m := range members {
		dmSlug, err := broker.EnsureDirectChannel(m.slug)
		if err != nil {
			die("open DM with %s: %v", m.slug, err)
		}
		beforeDM := len(broker.AllMessages())
		prompt := dmPrompts[m.slug]
		if _, err := broker.PostMessage("human", dmSlug, prompt, nil, ""); err != nil {
			die("post DM to %s: %v", m.slug, err)
		}
		fmt.Printf("\nSEND DM→%s (%s): %q\n", m.slug, dmSlug, prompt)
		want := map[string]bool{m.slug: false}
		replies := waitForReplies(broker, beforeDM, want, dmSlug, 30*time.Second)
		fmt.Printf("  RECV DM←%s: %q\n", m.slug, truncate(replies[m.slug], 140))
	}

	// 4. Multi-turn channel test. Address just pm-bot so we also verify that
	// @mention targeting keeps routing correctly across turns and that the
	// per-session conversation history OpenClaw keeps is visible through the
	// bridge.
	fmt.Println("\n--- channel multi-turn (@pm-bot only) ---")
	channelTurns := []string{
		"pm-bot: please remember the number 42 for me. Reply with exactly: ok, 42",
		"pm-bot: what number did I ask you to remember? Reply with just the digits.",
		"pm-bot: add 8 to that number. Reply with just the digits.",
	}
	for i, prompt := range channelTurns {
		beforeT := len(broker.AllMessages())
		if _, err := broker.PostMessage("human", "general", prompt, []string{members[0].slug}, ""); err != nil {
			die("channel turn %d post: %v", i+1, err)
		}
		fmt.Printf("SEND ch-turn-%d: %q\n", i+1, prompt)
		want := map[string]bool{members[0].slug: false}
		r := waitForReplies(broker, beforeT, want, "general", 45*time.Second)
		fmt.Printf("  RECV ch-turn-%d from %s: %q\n", i+1, members[0].slug, truncate(r[members[0].slug], 160))
	}

	// 5. Multi-turn DM test. Same shape, but in eng-bot's DM — confirms
	// DM-partner inference handles repeated sends without the router falling
	// back to "general" on turn N when it got it right on turn 1.
	fmt.Println("\n--- DM multi-turn (eng-bot) ---")
	engDM, err := broker.EnsureDirectChannel(members[1].slug)
	if err != nil {
		die("eng DM open: %v", err)
	}
	dmTurns := []string{
		"pick a fruit (just one lowercase word) and tell me what it is.",
		"now spell that fruit backwards. Reply with just the reversed word, lowercase.",
		"how many letters does the fruit you picked have? Reply with just the digit.",
	}
	for i, prompt := range dmTurns {
		beforeT := len(broker.AllMessages())
		if _, err := broker.PostMessage("human", engDM, prompt, nil, ""); err != nil {
			die("DM turn %d post: %v", i+1, err)
		}
		fmt.Printf("SEND dm-turn-%d: %q\n", i+1, prompt)
		want := map[string]bool{members[1].slug: false}
		r := waitForReplies(broker, beforeT, want, engDM, 45*time.Second)
		fmt.Printf("  RECV dm-turn-%d from %s: %q\n", i+1, members[1].slug, truncate(r[members[1].slug], 160))
	}

	// 6. "Pick up a task" test — give one agent a multi-step task in a single
	// DM message and verify the deterministic final answer. This is the
	// closest smoke for "can this agent actually do work" short of wiring
	// MCP tools, files, or shell access.
	fmt.Println("\n--- DM task handoff (pm-bot) ---")
	pmDM, err := broker.EnsureDirectChannel(members[0].slug)
	if err != nil {
		die("pm DM open: %v", err)
	}
	taskPrompt := strings.Join([]string{
		"I'm going to give you a small task. Do each step, then answer.",
		"Step 1: take the number 7.",
		"Step 2: multiply it by 6.",
		"Step 3: subtract 2 from the result.",
		"Step 4: reply with just the final number, no words, no punctuation.",
	}, " ")
	beforeTask := len(broker.AllMessages())
	if _, err := broker.PostMessage("human", pmDM, taskPrompt, nil, ""); err != nil {
		die("task post: %v", err)
	}
	fmt.Printf("SEND task: %q\n", truncate(taskPrompt, 200))
	want := map[string]bool{members[0].slug: false}
	r := waitForReplies(broker, beforeTask, want, pmDM, 60*time.Second)
	taskReply := strings.TrimSpace(r[members[0].slug])
	fmt.Printf("  RECV task from %s: %q\n", members[0].slug, truncate(taskReply, 160))
	if !strings.Contains(taskReply, "40") {
		die("task answer does not contain expected value 40 — got %q", taskReply)
	}

	fmt.Println("\nall checks passed")
	fmt.Println("PASS")
}

// waitForReplies polls broker.AllMessages() from `before` onward until each
// slug in `want` has posted at least one message on `channel` via source
// "openclaw". Dies on timeout. Returns the first reply content per slug.
func waitForReplies(broker *team.Broker, before int, want map[string]bool, channel string, timeout time.Duration) map[string]string {
	got := make(map[string]string, len(want))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msgs := broker.AllMessages()
		for _, m := range msgs[before:] {
			if m.Channel != channel {
				continue
			}
			if _, tracked := want[m.From]; !tracked {
				continue
			}
			if m.Source != "openclaw" {
				continue
			}
			if _, seen := got[m.From]; !seen {
				got[m.From] = m.Content
				want[m.From] = true
			}
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
		time.Sleep(300 * time.Millisecond)
	}
	die("timeout waiting for replies on %s; got=%v want=%v", channel, got, want)
	return got
}

func resolveToken() string {
	if t := os.Getenv("OPENCLAW_TOKEN"); t != "" {
		return t
	}
	raw, err := os.ReadFile(os.ExpandEnv("$HOME/.openclaw/openclaw.json"))
	if err != nil {
		die("OPENCLAW_TOKEN unset and ~/.openclaw/openclaw.json unreadable: %v", err)
	}
	var cfg struct {
		Gateway struct {
			Auth struct {
				Token string `json:"token"`
			} `json:"auth"`
		} `json:"gateway"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		die("parse openclaw config: %v", err)
	}
	if cfg.Gateway.Auth.Token == "" {
		die("no token found in ~/.openclaw/openclaw.json")
	}
	return cfg.Gateway.Auth.Token
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
