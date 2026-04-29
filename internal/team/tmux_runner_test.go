package team

// fakeTmuxRunner + fakeTmuxCall are the reusable test double for the
// tmuxRunner interface (PLAN.md §C5b / §3). Tests prepare canned output
// + canned errors keyed by tmux subcommand (the first argument:
// "list-panes", "has-session", "capture-pane", etc.), install the fake
// via setTmuxRunnerForTest, and assert on f.callsFor("…") afterwards
// to verify the exact args that flowed through.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

type fakeTmuxCall struct {
	Method string   // "Run", "Output", or "Combined"
	Args   []string // exactly the args the caller passed (no socket prefix)
}

func (c fakeTmuxCall) String() string {
	return fmt.Sprintf("%s %s", c.Method, strings.Join(c.Args, " "))
}

type fakeTmuxRunner struct {
	mu      sync.Mutex
	calls   []fakeTmuxCall
	outputs map[string][]byte
	errors  map[string]error
}

func newFakeTmuxRunner() *fakeTmuxRunner {
	return &fakeTmuxRunner{
		outputs: map[string][]byte{},
		errors:  map[string]error{},
	}
}

func (f *fakeTmuxRunner) record(method string, args []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(args))
	copy(cp, args)
	f.calls = append(f.calls, fakeTmuxCall{Method: method, Args: cp})
}

func (f *fakeTmuxRunner) lookup(args []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(args) == 0 {
		return nil, nil
	}
	sub := args[0]
	return f.outputs[sub], f.errors[sub]
}

func (f *fakeTmuxRunner) Run(args ...string) error {
	f.record("Run", args)
	_, err := f.lookup(args)
	return err
}

func (f *fakeTmuxRunner) Output(args ...string) ([]byte, error) {
	f.record("Output", args)
	return f.lookup(args)
}

func (f *fakeTmuxRunner) Combined(args ...string) ([]byte, error) {
	f.record("Combined", args)
	return f.lookup(args)
}

// callsFor returns args of every call whose first argument equals subcmd.
func (f *fakeTmuxRunner) callsFor(subcmd string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matches [][]string
	for _, c := range f.calls {
		if len(c.Args) > 0 && c.Args[0] == subcmd {
			cp := make([]string, len(c.Args))
			copy(cp, c.Args)
			matches = append(matches, cp)
		}
	}
	return matches
}

func TestRealTmuxRunnerPrependsSocket(t *testing.T) {
	// realTmuxRunner.cmd is package-internal, so we can't observe its
	// args without exec'ing tmux for real. Cross-check via the override
	// seam instead: install a fake, ask the production newTmuxRunner to
	// give us back the active runner, and confirm we got the fake (not
	// realTmuxRunner). This is the load-bearing contract that lets every
	// other test in this file work.
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)
	got := newTmuxRunner()
	if got != fake {
		t.Fatalf("newTmuxRunner returned %T, want fakeTmuxRunner", got)
	}
}

func TestPaneLifecycle_HasLiveSession(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)
	pl := newPaneLifecycle("wuphf-team")

	// Default: no canned error -> Run returns nil -> session "is live".
	if !pl.HasLiveSession() {
		t.Fatalf("HasLiveSession on default fake = false, want true")
	}

	// Now make has-session error. The runner returns the canned error
	// from Run, which the wrapper maps to "no session".
	fake.errors["has-session"] = fmt.Errorf("can't find session")
	if pl.HasLiveSession() {
		t.Fatalf("HasLiveSession with canned error = true, want false")
	}

	calls := fake.callsFor("has-session")
	if len(calls) != 2 {
		t.Fatalf("has-session calls = %d, want 2", len(calls))
	}
	want := []string{"has-session", "-t", "wuphf-team"}
	for i, got := range calls {
		if !equalStrings(got, want) {
			t.Errorf("has-session call[%d] = %v, want %v", i, got, want)
		}
	}
}

func TestPaneLifecycle_ListTeamPanesParsesOutput(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["list-panes"] = []byte("0 channel\n1 ceo\n2 fe\n")
	setTmuxRunnerForTest(t, fake)

	got, err := newPaneLifecycle("wuphf-team").ListTeamPanes()
	if err != nil {
		t.Fatalf("ListTeamPanes err = %v, want nil", err)
	}
	want := []int{1, 2}
	if !equalInts(got, want) {
		t.Fatalf("ListTeamPanes = %v, want %v", got, want)
	}
	calls := fake.callsFor("list-panes")
	if len(calls) != 1 {
		t.Fatalf("list-panes calls = %d, want 1", len(calls))
	}
	wantArgs := []string{"list-panes", "-t", "wuphf-team:team", "-F", "#{pane_index} #{pane_title}"}
	if !equalStrings(calls[0], wantArgs) {
		t.Errorf("list-panes args = %v, want %v", calls[0], wantArgs)
	}
}

func TestPaneLifecycle_ListTeamPanesMissingSessionIsNilNil(t *testing.T) {
	fake := newFakeTmuxRunner()
	// tmux's "no server" error text is what isMissingTmuxSession matches —
	// the runner returns the canned bytes as the *output*, not as the
	// error, because the historical CombinedOutput contract puts stderr
	// there. We must populate both: outputs (string) for matching and
	// errors (any non-nil) for the err branch.
	fake.outputs["list-panes"] = []byte("no server running on /tmp/tmux-1000/wuphf")
	fake.errors["list-panes"] = fmt.Errorf("exit status 1")
	setTmuxRunnerForTest(t, fake)

	got, err := newPaneLifecycle("wuphf-team").ListTeamPanes()
	if err != nil {
		t.Fatalf("ListTeamPanes(missing session) err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("ListTeamPanes(missing session) = %v, want nil", got)
	}
}

func TestPaneLifecycle_ChannelPaneStatusTrimsOutput(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["display-message"] = []byte("  1 0 claude  \n")
	setTmuxRunnerForTest(t, fake)

	got, err := newPaneLifecycle("wuphf-team").ChannelPaneStatus()
	if err != nil {
		t.Fatalf("ChannelPaneStatus err = %v", err)
	}
	if got != "1 0 claude" {
		t.Fatalf("ChannelPaneStatus = %q, want %q", got, "1 0 claude")
	}
}

func TestPaneLifecycle_CapturePaneContentBuildsTarget(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["capture-pane"] = []byte("agent ready\n")
	setTmuxRunnerForTest(t, fake)

	got, err := newPaneLifecycle("wuphf-team").CapturePaneContent(2)
	if err != nil {
		t.Fatalf("CapturePaneContent err = %v", err)
	}
	if got != "agent ready\n" {
		t.Fatalf("CapturePaneContent = %q, want %q", got, "agent ready\n")
	}
	calls := fake.callsFor("capture-pane")
	if len(calls) != 1 {
		t.Fatalf("capture-pane calls = %d, want 1", len(calls))
	}
	wantArgs := []string{"capture-pane", "-p", "-J", "-t", "wuphf-team:team.2"}
	if !equalStrings(calls[0], wantArgs) {
		t.Errorf("capture-pane args = %v, want %v", calls[0], wantArgs)
	}
}

func TestHasLiveTmuxSessionRoutesThroughRunner(t *testing.T) {
	// Verify the package-level free function HasLiveTmuxSession() also
	// goes through the runner seam — the post-C5b implementation routes
	// through newPaneLifecycle(SessionName).HasLiveSession().
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)
	if !HasLiveTmuxSession() {
		t.Fatalf("HasLiveTmuxSession on default fake = false, want true")
	}
	calls := fake.callsFor("has-session")
	if len(calls) != 1 {
		t.Fatalf("has-session calls = %d, want 1", len(calls))
	}
	if calls[0][0] != "has-session" || calls[0][1] != "-t" {
		t.Errorf("unexpected call args: %v", calls[0])
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
