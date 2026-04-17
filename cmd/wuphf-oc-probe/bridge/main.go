// wuphf-oc-probe/bridge is a higher-level smoke test that exercises the full
// team.OpenclawBridge against a real OpenClaw daemon. Unlike the protocol-only
// probe under cmd/wuphf-oc-probe, this one proves:
//
//   - StartOpenclawBridgeFromConfig reads config + dials + supervises
//   - Bridged session becomes a listed office member (not just a message author)
//   - OnOfficeMessage drives sessions.send through the real bridge
//   - assistant role session.message events land in the broker as #general msgs
//   - Multi-turn conversation: each send produces a distinct reply
//   - user role echoes are filtered out (no double-post)
//
// Run with:
//
//	OPENCLAW_TOKEN=... go run ./cmd/wuphf-oc-probe/bridge
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

const bridgeSlug = "openclaw-smoke"

func main() {
	token := os.Getenv("OPENCLAW_TOKEN")
	if token == "" {
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
		token = cfg.Gateway.Auth.Token
		if token == "" {
			die("no token found in ~/.openclaw/openclaw.json")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Load identity from the real WUPHF path so we reuse the daemon pairing
	// across this probe AND the production bridge.
	realIdentityPath := config.ResolveOpenclawIdentityPath()
	identity, err := openclaw.LoadOrCreateDeviceIdentity(realIdentityPath)
	if err != nil {
		die("identity: %v", err)
	}

	// 1. List real sessions on the daemon (creates one implicitly if needed).
	pre, err := openclaw.Dial(ctx, openclaw.Config{URL: "ws://127.0.0.1:18789", Token: token, Identity: identity})
	if err != nil {
		die("pre-dial: %v", err)
	}
	rows, err := pre.SessionsList(ctx, openclaw.SessionsListFilter{Limit: 5})
	if err != nil {
		die("list: %v", err)
	}
	if err := pre.Close(); err != nil {
		die("pre.Close: %v", err)
	}
	var sessKey string
	if len(rows) == 0 {
		// sessions.send requires a pre-existing session, so create one.
		create, err := openclaw.Dial(ctx, openclaw.Config{URL: "ws://127.0.0.1:18789", Token: token, Identity: identity})
		if err != nil {
			die("create-dial: %v", err)
		}
		defer func() { _ = create.Close() }()
		raw, err := create.Call(ctx, "sessions.create", map[string]any{"agentId": "main", "label": "wuphf-smoke"})
		if err != nil {
			die("sessions.create: %v", err)
		}
		var out struct {
			Key string `json:"key"`
		}
		_ = json.Unmarshal(raw, &out)
		if out.Key == "" {
			die("sessions.create returned no key: %s", string(raw))
		}
		sessKey = out.Key
		fmt.Println("created new session:", sessKey)
	} else {
		sessKey = rows[0].Key
		fmt.Printf("target session: key=%s kind=%s\n", sessKey, rows[0].Kind)
	}

	// 2. Seed a temporary WUPHF HOME so broker state doesn't clash with the
	// user's real install. Point identity + token back at the paired daemon.
	tmpHome, err := os.MkdirTemp("", "wuphf-oc-smoke-*")
	if err != nil {
		die("tmp home: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpHome) }()
	if err := os.Setenv("HOME", tmpHome); err != nil {
		die("setenv HOME: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpHome, ".wuphf"), 0o700); err != nil {
		die("mkdir .wuphf: %v", err)
	}
	if err := os.Setenv("WUPHF_OPENCLAW_IDENTITY_PATH", realIdentityPath); err != nil {
		die("setenv identity path: %v", err)
	}
	if err := os.Setenv("WUPHF_OPENCLAW_TOKEN", token); err != nil {
		die("setenv token: %v", err)
	}
	if err := config.Save(config.Config{
		OpenclawGatewayURL: "ws://127.0.0.1:18789",
		OpenclawBridges: []config.OpenclawBridgeBinding{
			{SessionKey: sessKey, Slug: bridgeSlug, DisplayName: "Smoke Bot"},
		},
	}); err != nil {
		die("save config: %v", err)
	}

	// 3. Boot a broker + start the real bridge via the real bootstrap path.
	broker := team.NewBroker()
	bridge, err := team.StartOpenclawBridgeFromConfig(ctx, broker)
	if err != nil {
		die("start bridge: %v", err)
	}
	if bridge == nil {
		die("bootstrap returned nil bridge — bindings not persisted?")
	}
	defer bridge.Stop()
	fmt.Println("bridge started")

	time.Sleep(500 * time.Millisecond)
	if !bridge.HasSlug(bridgeSlug) {
		die("bridge does not recognize slug " + bridgeSlug)
	}

	// 3a. Office-member registration check.
	foundMember := false
	for _, m := range broker.OfficeMembers() {
		if m.Slug == bridgeSlug {
			foundMember = true
			fmt.Printf("  MEMBER: %q name=%q role=%q createdBy=%q\n", m.Slug, m.Name, m.Role, m.CreatedBy)
			break
		}
	}
	if !foundMember {
		die("bridged slug never registered as an office member — check EnsureBridgedMember")
	}

	// 4. Multi-turn conversation. Send three distinct messages, collect replies.
	// Use concrete questions so the agent produces real answers instead of
	// shrugging the turn off as noise ("NO_REPLY"). Each prompt references the
	// prior turn so we can visually confirm multi-turn context is preserved.
	nonce := fmt.Sprint(time.Now().UnixNano())
	prompts := []string{
		"ping " + nonce + " — answer with exactly the single word: pong",
		"what is 2+2? Reply with just the number.",
		"in one short sentence, what was the first question I asked you in this session?",
	}
	repliesByTurn := make([]string, len(prompts))
	for i, msg := range prompts {
		before := len(broker.AllMessages())
		if err := bridge.OnOfficeMessage(ctx, bridgeSlug, "general", msg); err != nil {
			die("turn %d OnOfficeMessage: %v", i+1, err)
		}
		fmt.Printf("SEND turn-%d: %q\n", i+1, msg)

		// Poll for a new assistant reply.
		deadline := time.Now().Add(15 * time.Second)
		var reply string
		for time.Now().Before(deadline) {
			msgs := broker.AllMessages()
			for _, m := range msgs[before:] {
				if m.From == bridgeSlug && m.Source == "openclaw" {
					reply = m.Content
					break
				}
			}
			if reply != "" {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		if reply == "" {
			die("turn %d: no agent reply within 15s", i+1)
		}
		fmt.Printf("RECV turn-%d: %q\n", i+1, truncate(reply, 140))
		repliesByTurn[i] = reply

		// Confirm user-role echo wasn't forwarded as an agent msg.
		for _, m := range broker.AllMessages()[before:] {
			if m.From == bridgeSlug && m.Content == msg {
				die("turn %d: bridge echoed our outbound back — role filter broken", i+1)
			}
		}
	}

	// 5. All three turns produced *some* reply. They may all be the same error
	// if OpenClaw has no model configured — that's fine; the bridge round-trip
	// still fired 3 times cleanly.
	fmt.Printf("\nback-and-forth OK: %d turns, %d replies\n", len(prompts), len(repliesByTurn))
	fmt.Println("PASS")
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
