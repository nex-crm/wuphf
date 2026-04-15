// wuphf-oc-probe is a tiny smoke test that drives the OpenClaw bridge client
// against a running local daemon. Build and run it with:
//
//	OPENCLAW_TOKEN=... go run ./cmd/wuphf-oc-probe
//
// It performs: handshake → sessions.list → subscribe → sessions.send, and
// prints session.message events for ~5 seconds. Used to validate protocol
// assumptions against a real `openclaw daemon` install without polluting the
// unit-test suite.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/openclaw"
)

func main() {
	token := os.Getenv("OPENCLAW_TOKEN")
	if token == "" {
		data, err := os.ReadFile("/tmp/oc-token.txt")
		if err != nil {
			die("OPENCLAW_TOKEN unset and /tmp/oc-token.txt unreadable: %v", err)
		}
		token = strings.TrimSpace(string(data))
	}
	identity, err := openclaw.LoadOrCreateDeviceIdentity(os.ExpandEnv("$HOME/.wuphf/openclaw/identity.json"))
	if err != nil {
		die("identity: %v", err)
	}
	fmt.Println("deviceId:", identity.DeviceID()[:16], "...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := openclaw.Dial(ctx, openclaw.Config{
		URL:      "ws://127.0.0.1:18789",
		Token:    token,
		Identity: identity,
	})
	if err != nil {
		die("dial: %v", err)
	}
	defer client.Close()
	fmt.Println("dialed + handshake ok")

	rows, err := client.SessionsList(ctx, openclaw.SessionsListFilter{Limit: 5, IncludeLastMessage: true})
	if err != nil {
		die("sessions.list: %v", err)
	}
	fmt.Printf("found %d session(s)\n", len(rows))
	if len(rows) == 0 {
		fmt.Println("PASS (no sessions to send to, but protocol works)")
		return
	}
	sess := rows[0]
	fmt.Printf("first session: key=%s displayName=%q kind=%s\n", sess.Key, sess.DisplayName, sess.Kind)

	if err := client.SessionsMessagesSubscribe(ctx, sess.Key); err != nil {
		die("subscribe: %v", err)
	}
	fmt.Println("subscribed")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range client.Events() {
			switch evt.Kind {
			case openclaw.EventKindMessage:
				m := evt.SessionMessage
				fmt.Printf("  EVT message role=%q text=%q\n", m.Role, snippet(m.MessageText, 140))
			case openclaw.EventKindChanged:
				fmt.Printf("  EVT changed reason=%s phase=%s\n", evt.SessionsChanged.Reason, evt.SessionsChanged.Phase)
			case openclaw.EventKindGap:
				fmt.Printf("  EVT gap from=%d to=%d\n", evt.Gap.FromSeq, evt.Gap.ToSeq)
			case openclaw.EventKindClose:
				fmt.Println("  connection closed")
				return
			}
		}
	}()

	res, err := client.SessionsSend(ctx, sess.Key, "hello from wuphf Go probe", fmt.Sprintf("probe-%d", time.Now().UnixNano()))
	if err != nil {
		die("sessions.send: %v", err)
	}
	fmt.Printf("sent: runId=%s status=%s messageSeq=%d\n", res.RunID, res.Status, res.MessageSeq)

	time.Sleep(5 * time.Second)
	client.Close()
	<-done
	fmt.Println("PASS")
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func snippet(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
