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

func TestResolve(t *testing.T) {
	cases := []struct {
		name          string
		in            ResolveInput
		wantDecision  Decision
		wantEffective ConnectionState
	}{
		// probe succeeded: effective == probed
		{"probe connected mutating -> approve", ResolveInput{ProbeOK: true, Probed: StateConnected}, DecisionApprove, StateConnected},
		{"probe connected read-only -> proceed", ResolveInput{ProbeOK: true, Probed: StateConnected, ReadOnly: true}, DecisionProceed, StateConnected},
		{"probe missing -> connect", ResolveInput{ProbeOK: true, Probed: StateMissing}, DecisionConnect, StateMissing},
		{"probe unsupported -> fallback", ResolveInput{ProbeOK: true, Probed: StateUnsupported}, DecisionFallback, StateUnsupported},
		// probe FAILED (provider unreachable): serve fresh connected last-known-good
		{"outage + fresh connected LKG -> approve", ResolveInput{ProbeOK: false, LastKnown: StateConnected, LastKnownFresh: true}, DecisionApprove, StateConnected},
		{"outage + fresh connected LKG read-only -> proceed", ResolveInput{ProbeOK: false, LastKnown: StateConnected, LastKnownFresh: true, ReadOnly: true}, DecisionProceed, StateConnected},
		// probe FAILED + last-known NOT fresh -> indeterminate (block-with-retry), never connect
		{"outage + stale connected LKG -> fail_safe", ResolveInput{ProbeOK: false, LastKnown: StateConnected, LastKnownFresh: false}, DecisionFailSafe, StateIndeterminate},
		// probe FAILED + last-known not connected -> indeterminate, never connect (no false prompt)
		{"outage + missing LKG -> fail_safe", ResolveInput{ProbeOK: false, LastKnown: StateMissing, LastKnownFresh: true}, DecisionFailSafe, StateIndeterminate},
		{"outage + no LKG -> fail_safe", ResolveInput{ProbeOK: false, LastKnown: StateUnknown}, DecisionFailSafe, StateIndeterminate},
	}
	for _, c := range cases {
		gotDecision, gotEffective := Resolve(c.in)
		if gotDecision != c.wantDecision || gotEffective != c.wantEffective {
			t.Errorf("%s: Resolve(%+v) = (%q, %q), want (%q, %q)", c.name, c.in, gotDecision, gotEffective, c.wantDecision, c.wantEffective)
		}
	}
}

// TestResolveOutageNeverConnects guards the fail-safe invariant: a provider
// outage must never produce a connect decision (which would be a false prompt),
// regardless of the last-known state, unless that state is a fresh connection.
func TestResolveOutageNeverConnects(t *testing.T) {
	for _, lk := range []ConnectionState{StateUnknown, StateMissing, StateFailed, StatePending, StateConnected} {
		for _, fresh := range []bool{false, true} {
			d, _ := Resolve(ResolveInput{ProbeOK: false, LastKnown: lk, LastKnownFresh: fresh})
			if d == DecisionConnect {
				t.Errorf("outage with last-known %q (fresh=%v) produced connect; outages must never prompt connect", lk, fresh)
			}
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
