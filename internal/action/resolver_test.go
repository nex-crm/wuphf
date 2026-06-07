package action

import "testing"

func TestMapConnectionState(t *testing.T) {
	cases := []struct {
		in   string
		want ConnectionState
	}{
		{"connected", StateConnected},
		{"active", StateConnected},
		{"ENABLED", StateConnected},
		{"pending", StatePending},
		{"initiated", StatePending},
		{"in_progress", StatePending},
		{"failed", StateFailed},
		{"error", StateFailed},
		{"disconnected", StateMissing},
		{"disabled", StateMissing},
		{"inactive", StateMissing},
		{"available", StateMissing},
		{"", StateMissing},
		{"   ", StateMissing},
		{"something_unknown", StateMissing},
		{"  Connected  ", StateConnected},
	}
	for _, c := range cases {
		if got := MapConnectionState(c.in); got != c.want {
			t.Errorf("MapConnectionState(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestActionIsReadOnly(t *testing.T) {
	cases := []struct {
		actionID string
		want     bool
	}{
		// read verbs as whole tokens
		{"GMAIL_FETCH_EMAILS", true},
		{"SLACK_LIST_CHANNELS", true},
		{"HUBSPOT_GET_CONTACT", true},
		{"NOTION_SEARCH_PAGES", true},
		{"DESCRIBE_SCHEMA", true},
		{"calendar.list", true},
		// mutating verbs veto, even with a read verb present
		{"GMAIL_SEND_EMAIL", false},
		{"GMAIL_LIST_AND_DELETE", false},
		{"FINDONE_AND_UPDATE", false},
		{"SLACK_POST_MESSAGE", false},
		{"HUBSPOT_CREATE_DEAL", false},
		// substring guard: read verb only as a substring does not count
		{"CHECK_BUDGET", false},  // "get" inside "budget" must not match
		{"REVIEW_REPORT", false}, // "view" inside "review" must not match (and "review" not a read verb)
		// no recognized verb at all
		{"GMAIL_THREADS", false},
		{"", false},
		{"   ", false},
	}
	for _, c := range cases {
		if got := ActionIsReadOnly(c.actionID); got != c.want {
			t.Errorf("ActionIsReadOnly(%q) = %v, want %v", c.actionID, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		in   ClassifyInput
		want Decision
	}{
		// connected
		{"connected mutating no grant -> approve", ClassifyInput{ReadOnly: false, State: StateConnected, HasGrant: false}, DecisionApprove},
		{"connected mutating with grant -> proceed", ClassifyInput{ReadOnly: false, State: StateConnected, HasGrant: true}, DecisionProceed},
		{"connected read-only -> proceed", ClassifyInput{ReadOnly: true, State: StateConnected, HasGrant: false}, DecisionProceed},
		{"connected read-only with grant -> proceed", ClassifyInput{ReadOnly: true, State: StateConnected, HasGrant: true}, DecisionProceed},
		// missing/failed always route to connect, regardless of read-only or grant
		{"missing mutating -> connect", ClassifyInput{State: StateMissing}, DecisionConnect},
		{"missing read-only -> connect", ClassifyInput{ReadOnly: true, State: StateMissing}, DecisionConnect},
		{"missing with stale grant -> connect", ClassifyInput{State: StateMissing, HasGrant: true}, DecisionConnect},
		{"failed mutating -> connect", ClassifyInput{State: StateFailed}, DecisionConnect},
		{"failed read-only -> connect", ClassifyInput{ReadOnly: true, State: StateFailed}, DecisionConnect},
		// pending/unknown/checking -> wait
		{"pending -> wait", ClassifyInput{State: StatePending}, DecisionWait},
		{"unknown -> wait", ClassifyInput{State: StateUnknown}, DecisionWait},
		{"checking -> wait", ClassifyInput{State: StateChecking}, DecisionWait},
		{"pending read-only -> wait", ClassifyInput{ReadOnly: true, State: StatePending}, DecisionWait},
		// indeterminate -> fail-safe (never connect)
		{"indeterminate -> fail_safe", ClassifyInput{State: StateIndeterminate}, DecisionFailSafe},
		{"indeterminate read-only -> fail_safe", ClassifyInput{ReadOnly: true, State: StateIndeterminate}, DecisionFailSafe},
		{"indeterminate with grant -> fail_safe", ClassifyInput{State: StateIndeterminate, HasGrant: true}, DecisionFailSafe},
		// unsupported -> fallback
		{"unsupported -> fallback", ClassifyInput{State: StateUnsupported}, DecisionFallback},
		{"unsupported read-only -> fallback", ClassifyInput{ReadOnly: true, State: StateUnsupported}, DecisionFallback},
		// unmodeled state must never proceed
		{"unmodeled state -> wait", ClassifyInput{State: ConnectionState("bogus")}, DecisionWait},
	}
	for _, c := range cases {
		if got := Classify(c.in); got != c.want {
			t.Errorf("%s: Classify(%+v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestClassifyNeverProceedsUnconnectedMutating is the load-bearing invariant:
// no mutating action may proceed without a connected state. This guards the
// core promise of the resolver against future edits to Classify.
func TestClassifyNeverProceedsUnconnectedMutating(t *testing.T) {
	unconnected := []ConnectionState{
		StateUnknown, StateChecking, StatePending, StateMissing,
		StateFailed, StateUnsupported, StateIndeterminate, ConnectionState("bogus"),
	}
	for _, st := range unconnected {
		for _, grant := range []bool{false, true} {
			got := Classify(ClassifyInput{ReadOnly: false, State: st, HasGrant: grant})
			if got == DecisionProceed {
				t.Errorf("mutating action in state %q (grant=%v) classified proceed; must be gated", st, grant)
			}
		}
	}
}
