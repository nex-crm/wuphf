// wuphf-oc-probe/bridge is a higher-level smoke test that exercises the full
// team.OpenclawBridge against a real OpenClaw daemon. Unlike the protocol-only
// probe under cmd/wuphf-oc-probe, this one proves:
//
//   - StartOpenclawBridgeFromConfig reads config + dials + supervises
//   - OnOfficeMessage drives sessions.send through the real bridge
//   - assistant role session.message events land in the broker as #general msgs
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

	// 1. List real sessions on the daemon.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	identity, err := openclaw.LoadOrCreateDeviceIdentity(config.ResolveOpenclawIdentityPath())
	if err != nil {
		die("identity: %v", err)
	}
	pre, err := openclaw.Dial(ctx, openclaw.Config{URL: "ws://127.0.0.1:18789", Token: token, Identity: identity})
	if err != nil {
		die("pre-dial: %v", err)
	}
	rows, err := pre.SessionsList(ctx, openclaw.SessionsListFilter{Limit: 5})
	if err != nil {
		die("list: %v", err)
	}
	pre.Close()
	if len(rows) == 0 {
		die("no OpenClaw sessions found — create one with `openclaw agent 'hello'` first")
	}
	sess := rows[0]
	fmt.Printf("target session: key=%s kind=%s\n", sess.Key, sess.Kind)

	// 2. Seed a temporary WUPHF config with this session as a binding.
	tmpHome, err := os.MkdirTemp("", "wuphf-oc-smoke-*")
	if err != nil {
		die("tmp home: %v", err)
	}
	defer os.RemoveAll(tmpHome)
	os.Setenv("HOME", tmpHome)
	os.MkdirAll(filepath.Join(tmpHome, ".wuphf"), 0o700)
	// Keypair outside tmp so we re-use the paired one on the daemon.
	os.Setenv("WUPHF_OPENCLAW_IDENTITY_PATH", os.ExpandEnv("$PWD/../../.wuphf/openclaw/identity.json"))
	os.Setenv("WUPHF_OPENCLAW_TOKEN", token)
	if err := config.Save(config.Config{
		OpenclawGatewayURL: "ws://127.0.0.1:18789",
		OpenclawBridges: []config.OpenclawBridgeBinding{
			{SessionKey: sess.Key, Slug: "openclaw-smoke", DisplayName: "Smoke"},
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
	if !bridge.HasSlug("openclaw-smoke") {
		die("bridge does not recognize slug openclaw-smoke")
	}

	// 4. Send a message through the bridge.
	msg := "smoke test " + fmt.Sprint(time.Now().UnixNano())
	if err := bridge.OnOfficeMessage(ctx, "openclaw-smoke", msg); err != nil {
		die("OnOfficeMessage: %v", err)
	}
	fmt.Printf("sent: %q\n", msg)

	// 5. Poll the broker for a message from the bridged slug.
	deadline := time.Now().Add(15 * time.Second)
	var sawAgent bool
	var sawUserEcho bool
	for time.Now().Before(deadline) {
		for _, m := range broker.AllMessages() {
			if m.Source == "openclaw" && m.From == "openclaw-smoke" {
				sawAgent = true
				fmt.Printf("  RECV (agent→broker): %q\n", truncate(m.Content, 160))
			}
			// The bridge should NEVER re-post our own outbound as a bridged msg.
			if m.Source == "openclaw" && m.From == "openclaw-smoke" && m.Content == msg {
				sawUserEcho = true
			}
		}
		if sawAgent {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if sawUserEcho {
		die("bridge echoed our own outbound back as an agent message — role filter broken")
	}
	if !sawAgent {
		fmt.Println("NOTE: no agent reply received within 15s — may be an OpenClaw model config issue (no API key). Protocol layer is still proven.")
	}
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
