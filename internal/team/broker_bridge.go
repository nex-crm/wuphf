package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleBridge is the CEO-only endpoint for cross-channel bridging:
// when context relevant to channel B exists in channel A, the CEO
// can carry a summarized version into B with a recorded signal +
// decision + action trail. Restricted to actor="ceo" because a
// bridge writes to a channel the bridging agent may not be a Member
// of (canAccessChannelLocked would otherwise reject the post).
//
// Wire shape:
//
//	POST /bridge
//	{
//	  "actor":         "ceo",
//	  "source_channel": "engineering",
//	  "target_channel": "go-to-market",
//	  "summary":       "...",
//	  "tagged":        ["@bd-lead"],
//	  "reply_to":      ""
//	}
//
// Side-effects (all atomic; one of them failing fails the whole call):
//   1. RecordSignals — one office signal under "channel_bridge"
//   2. RecordDecision — one "bridge_channel" decision referencing the signal
//   3. PostAutomationMessage — the summary lands in target_channel as a
//      "wuphf"-authored message
//   4. RecordAction — one "bridge_channel" action referencing all of the above

// AttachOpenclawBridge wires the OpenClaw bridge into the broker so
// handleOfficeMembers can drive live subscribe/unsubscribe/sessions.create/
// sessions.end calls as members are hired and fired. Called by the launcher
// after StartOpenclawBridgeFromConfig succeeds. Safe to call with nil to
// detach (tests).
func (b *Broker) AttachOpenclawBridge(bridge *OpenclawBridge) {
	b.mu.Lock()
	b.openclawBridge = bridge
	b.mu.Unlock()
}

// openclawBridgeLocked returns the attached bridge pointer. Callers must
// hold b.mu. Kept as a small helper so the field is never read without the
// lock (and so we have one place to note the invariant).
func (b *Broker) openclawBridgeLocked() *OpenclawBridge {
	return b.openclawBridge
}

func (b *Broker) handleBridge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Actor         string   `json:"actor"`
		SourceChannel string   `json:"source_channel"`
		TargetChannel string   `json:"target_channel"`
		Summary       string   `json:"summary"`
		Tagged        []string `json:"tagged"`
		ReplyTo       string   `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	actor := normalizeActorSlug(body.Actor)
	if actor != "ceo" {
		http.Error(w, "only the CEO can bridge channel context", http.StatusForbidden)
		return
	}
	source := normalizeChannelSlug(body.SourceChannel)
	target := normalizeChannelSlug(body.TargetChannel)
	if source == "" || target == "" {
		http.Error(w, "source_channel and target_channel required", http.StatusBadRequest)
		return
	}
	summary := strings.TrimSpace(body.Summary)
	if summary == "" {
		http.Error(w, "summary required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	sourceExists := b.findChannelLocked(source) != nil
	targetExists := b.findChannelLocked(target) != nil
	b.mu.Unlock()
	if !sourceExists || !targetExists {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	records, err := b.RecordSignals([]officeSignal{{
		ID:         fmt.Sprintf("bridge:%s:%s:%s", source, target, truncateSummary(strings.ToLower(summary), 48)),
		Source:     "channel_bridge",
		Kind:       "bridge",
		Title:      "Cross-channel bridge",
		Content:    fmt.Sprintf("CEO bridged context from #%s to #%s: %s", source, target, summary),
		Channel:    target,
		Owner:      "ceo",
		Confidence: "explicit",
		Urgency:    "normal",
	}})
	if err != nil {
		http.Error(w, "failed to record bridge signal", http.StatusInternalServerError)
		return
	}
	signalIDs := make([]string, 0, len(records))
	for _, record := range records {
		signalIDs = append(signalIDs, record.ID)
	}
	decision, err := b.RecordDecision(
		"bridge_channel",
		target,
		fmt.Sprintf("CEO bridged context from #%s to #%s.", source, target),
		"Relevant context existed in another channel, so the CEO carried it into this channel explicitly.",
		"ceo",
		signalIDs,
		false,
		false,
	)
	if err != nil {
		http.Error(w, "failed to record bridge decision", http.StatusInternalServerError)
		return
	}
	content := summary + fmt.Sprintf("\n\nCEO bridged this context from #%s to help #%s.", source, target)
	msg, _, err := b.PostAutomationMessage(
		"wuphf",
		target,
		"Bridge from #"+source,
		content,
		decision.ID,
		"ceo_bridge",
		"CEO bridge",
		uniqueSlugs(body.Tagged),
		strings.TrimSpace(body.ReplyTo),
	)
	if err != nil {
		http.Error(w, "failed to persist bridge message", http.StatusInternalServerError)
		return
	}
	if err := b.RecordAction("bridge_channel", "ceo_bridge", target, actor, truncateSummary(summary, 140), msg.ID, signalIDs, decision.ID); err != nil {
		http.Error(w, "failed to persist bridge action", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":          msg.ID,
		"decision_id": decision.ID,
		"signal_ids":  signalIDs,
	})
}
