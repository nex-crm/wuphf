package team

// headless_turn_kill.go — honest surfacing of killed turns (ten-out-of-ten
// Wave F2). ICP-eval v3 [19:05:30]: a provider process killed by the OS
// (OOM / forced stop) left `signal: killed` raw exhaust as the ONLY trace of
// a 21-minute silent stall. A killed turn now (a) posts one human-readable
// system note into the turn's channel and (b) feeds a humanized detail into
// the recovery path, so the retry prompt, the progress pill, and any
// resulting block reason all carry the honest line instead of the raw
// signal string. The raw error stays in the agent log for operators.

import (
	"fmt"
	"strings"
	"time"
)

// isTurnKilledError reports whether a headless turn died to an external kill
// signal. exec.ExitError renders SIGKILL as "signal: killed" and SIGTERM as
// "signal: terminated"; both mean the process was stopped from outside (OOM
// killer, watchdog escalation, manual kill), not that the agent errored.
func isTurnKilledError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "signal: killed") || strings.Contains(msg, "signal: terminated")
}

// turnKilledHumanDetail is the one-line honest replacement for the raw
// signal exhaust. It rides into updateHeadlessProgress and
// recoverFailedHeadlessTurn (retry prompt / block reason).
func turnKilledHumanDetail(slug string) string {
	return fmt.Sprintf("@%s's agent process was killed by the system before it could finish — usually memory pressure or a forced stop, not a fault in the work itself", slug)
}

// postTurnKilledNote posts the one human-readable system note for a killed
// turn into the turn's channel (falling back to #general when the turn had
// no channel). Best-effort: a nil broker (tests) is a no-op.
func (l *Launcher) postTurnKilledNote(slug, channel string) {
	if l == nil || l.broker == nil {
		return
	}
	target := strings.TrimSpace(channel)
	if target == "" {
		target = "general"
	}
	l.broker.PostSystemMessage(target,
		fmt.Sprintf("%s. The turn will be retried or the task paused — no action needed unless it repeats.", turnKilledHumanDetail(slug)),
		"error",
	)
}

// chatTurnIsTaskless reports whether a turn is a plain chat/DM reply with no
// task behind it — the shared gate for the chat-turn surfacing helpers. It
// reuses timedOutTaskForTurn so the task/taskless split stays identical to the
// recovery layer's: a turn that resolves to a real task is left to BlockTask +
// self-healing, not these chat notes.
func (l *Launcher) chatTurnIsTaskless(slug string, turn headlessCodexTurn) bool {
	if l == nil {
		return false
	}
	task := l.timedOutTaskForTurn(slug, turn)
	return task == nil || strings.TrimSpace(task.ID) == ""
}

// noteChatTurnStall posts one honest system note when a turn that is NOT
// attached to a task — a plain chat or DM reply — ends without a reply because
// it timed out or errored. Task turns are skipped: their failure already
// surfaces through BlockTask + self-healing in the decision inbox. Without it,
// a chat reply that times out or errors leaves the user staring at silence —
// the agent "stalled and never replied" with no visible reason. Best-effort: a
// nil broker (tests) is a no-op.
func (l *Launcher) noteChatTurnStall(slug string, turn headlessCodexTurn, reason string) {
	if l == nil || l.broker == nil {
		return
	}
	if !l.chatTurnIsTaskless(slug, turn) {
		return
	}
	target := strings.TrimSpace(turn.Channel)
	if target == "" {
		target = "general"
	}
	l.broker.PostSystemMessage(target,
		fmt.Sprintf("@%s couldn't finish replying — %s. Try asking again.", slug, strings.TrimSpace(reason)),
		"error",
	)
}

// noteChatTurnNoReply posts one honest line when a turn that a real person
// directly prompted (a DM or @-mention, marked turn.FromHuman) completes
// successfully yet the agent never posts a reply anywhere. The provider runners
// already salvage a forgotten broadcast by posting the model's final text when
// it ran but didn't call team_broadcast (postHeadlessFinalMessageIfSilent), so
// this only fires in the narrower case where the turn returned with no
// user-facing output at all — a genuine silent miss against a human who is owed
// an answer (the human-priority prompt instructs the agent to always reply,
// status, or acknowledge). Gated three ways to avoid false positives on
// intentional silence: only human-prompted turns (agent-to-agent turns may
// legitimately stay quiet), only taskless turns, and only when the agent posted
// no substantive message in ANY channel since the turn began. Best-effort: a
// nil broker (tests) is a no-op.
func (l *Launcher) noteChatTurnNoReply(slug string, turn headlessCodexTurn, startedAt time.Time) {
	if l == nil || l.broker == nil {
		return
	}
	if !turn.FromHuman {
		return
	}
	if !l.chatTurnIsTaskless(slug, turn) {
		return
	}
	if l.agentPostedSubstantiveMessageSince(slug, startedAt) {
		return
	}
	target := strings.TrimSpace(turn.Channel)
	if target == "" {
		target = "general"
	}
	l.broker.PostSystemMessage(target,
		fmt.Sprintf("@%s finished without posting a reply — this is usually a hiccup, not a refusal. Try asking again.", slug),
		"error",
	)
}
