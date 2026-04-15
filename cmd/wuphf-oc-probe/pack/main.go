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
