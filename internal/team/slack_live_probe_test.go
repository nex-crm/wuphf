package team

// slack_live_probe_test.go is an env-guarded LIVE integration probe: it runs the
// real packer egress pipeline (Pack -> Deliver) through the real SlackBridge
// against a real workspace, proving redaction + classification + audit on the
// wire. It is skipped unless SLACK_LIVE_PROBE=1 and the Slack env vars are set,
// so CI and normal test runs never touch the network.
//
//	SLACK_LIVE_PROBE=1 SLACK_BOT_TOKEN=xoxb-… SLACK_CHANNEL_ID=C… \
//	  go test ./internal/team/ -run TestSlackLiveEgressProbe -v

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/packer"
)

// liveProbeBrain is a minimal in-memory BrainHandle whose plan step deliberately
// carries a fake (but pattern-matching) API key, so the probe proves the
// scanner redacts it before the text reaches Slack.
type liveProbeBrain struct{}

func (liveProbeBrain) PlanStep(string) (string, error) {
	return "Reconcile the June invoices and post totals in this thread. " +
		"Ops pasted a credential into the task: sk-proj-FAKELIVEPROBE000000000000000001 — " +
		"the packer must redact it before egress.", nil
}
func (liveProbeBrain) TaskLearnings(string, int) ([]packer.BrainItem, error) { return nil, nil }
func (liveProbeBrain) TaskWikiRefs(string) ([]packer.BrainItem, error)       { return nil, nil }
func (liveProbeBrain) Roster(string) ([]packer.BrainItem, error)             { return nil, nil }

func TestSlackLiveEgressProbe(t *testing.T) {
	if os.Getenv("SLACK_LIVE_PROBE") != "1" {
		t.Skip("live probe disabled (set SLACK_LIVE_PROBE=1)")
	}
	botToken := strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN"))
	channelID := strings.TrimSpace(os.Getenv("SLACK_CHANNEL_ID"))
	if botToken == "" || channelID == "" {
		t.Skip("SLACK_BOT_TOKEN / SLACK_CHANNEL_ID not set")
	}

	req := packer.ContextRequest{
		TaskID:        "live-probe-task",
		TaskUpdatedAt: "live",
		PlanID:        "live-probe-plan",
		PlanVersion:   1,
		Target: packer.BotProfile{
			Version:   1,
			Slug:      "live-probe-vendor-bot",
			Trust:     packer.BotUntrusted,
			ReadScope: packer.ReadMentionOnly,
			Identity: packer.BotIdentity{
				SlackTeamID: "live",
				AppUserID:   "U_PROBE_TARGET",
				InstallID:   "probe",
			},
		},
		Intent:          packer.StepIntent{Text: "Reconcile June invoices and report totals.", Taint: packer.TaintClean},
		Thread:          packer.ThreadRef{WorkspaceID: "live", ChannelID: channelID},
		EgressPolicyVer: 1,
		IdempotencyKey:  "live-probe-" + channelID,
	}

	packed, audit, err := packer.Pack(
		liveProbeBrain{},
		packer.NewDefaultEgressPolicy(1),
		packer.EgressScanner{},
		req,
		packer.GatherOptions{ReturnPact: "Reply in this thread with the totals and tag @ceo."},
		packer.DeliveryAudience{LeastTrustedPresent: packer.BotUntrusted},
	)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if strings.Contains(packed.MentionText, "sk-proj-FAKELIVEPROBE") {
		t.Fatalf("planted key survived Pack: %s", packed.MentionText)
	}

	sink := NewPackerInjectionSink()
	rec, err := packer.Deliver(context.Background(), NewSlackBridge(botToken), nil, packer.EgressScanner{}, sink, nil, packed, req)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if rec.Status != packer.DeliverySent || rec.MessageTS == "" {
		t.Fatalf("delivery record = %+v, want sent with a ts", rec)
	}

	t.Logf("LIVE egress delivered: ts=%s hash=%s tokens=%d", rec.MessageTS, rec.RenderedHash[:12], rec.TokenCount)
	for _, a := range audit {
		t.Logf("  audit: %-12s %-10s class=%s redactions=%d", a.Kind, a.Ref, a.Class, a.Redactions)
	}
}
