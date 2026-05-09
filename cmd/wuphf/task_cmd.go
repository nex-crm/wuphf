package main

// task_cmd.go is the Lane F CLI surface for the multi-agent control loop.
//
// Subcommands (all dispatched from main.go's `task` switch arm):
//
//   - `wuphf task start [intent]`      — prompt-driven intake → ready → running
//   - `wuphf task list [--filter=<f>]` — print inbox grouped by LifecycleState
//   - `wuphf task review <id>`         — open the Decision Packet view in the browser
//   - `wuphf task block <id> --on <pr-or-task-id>` — set blocked_on_pr_merge
//
// The implementation talks to the broker via its existing HTTP API
// (/tasks/inbox, /tasks/{id}, /tasks/{id}/block) and uses raw stdin
// (bufio.Scanner) for confirmation prompts, consistent with the
// `confirmDestructive` pattern already in main.go.
//
// `task start` is the only subcommand that requires the broker to be
// running locally — it calls Broker.StartIntake in-process via a small
// helper that boots a transient broker only when no live one is
// reachable. For v1 the simpler contract is: the user must have
// `wuphf` running (the broker), and the CLI POSTs intent to a future
// /tasks/intake endpoint. For minimal scope, v1 ships in-process
// intake against a Broker borrowed via the package-level handle.
//
// Lane B exposes IntakeProvider + AutoAssignCountdown + ErrIntakeNoProvider
// for this caller; the CLI honours Spec.AutoAssign with a 3-second
// cancellable countdown over stdin keypresses.

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

// runTaskCmd dispatches `wuphf task <verb> [args]`.
func runTaskCmd(args []string) {
	if len(args) == 0 || subcommandWantsHelp(args) {
		printTaskHelp()
		return
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "start":
		runTaskStart(rest)
	case "list", "ls":
		runTaskList(rest)
	case "review", "open":
		runTaskReview(rest)
	case "block":
		runTaskBlock(rest)
	default:
		fmt.Fprintf(os.Stderr, "wuphf task: unknown verb %q\n", verb)
		printTaskHelp()
		os.Exit(2)
	}
}

func printTaskHelp() {
	fmt.Fprintln(os.Stderr, "wuphf task — drive the multi-agent control loop")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  wuphf task start [intent]                Walk an intent through intake → ready → running")
	fmt.Fprintln(os.Stderr, "  wuphf task list [--filter=<filter>]      List tasks grouped by lifecycle state")
	fmt.Fprintln(os.Stderr, "  wuphf task review <id>                   Open the Decision Packet view in your browser")
	fmt.Fprintln(os.Stderr, "  wuphf task block <id> --on <ref>         Mark task blocked on a PR or task")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Filters: needs_decision (default), running, blocked, merged_today, all.")
}

// runTaskStart implements `wuphf task start [intent]`. v1 talks to the
// broker via an in-process call after dialing the local broker over
// HTTP to confirm one is running; without a running broker the CLI
// surfaces a clear error rather than starting a one-shot.
func runTaskStart(args []string) {
	fs := flag.NewFlagSet("task start", flag.ExitOnError)
	autoAssign := fs.String("auto-assign", "", "Override Spec.AutoAssign (skip the countdown)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf task start — drive an intent through intake")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf task start \"fix the cache invalidation bug\"")
		fmt.Fprintln(os.Stderr, "  wuphf task start                       # reads the intent from stdin")
	}
	_ = fs.Parse(args)
	intent := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if intent == "" {
		fmt.Fprint(os.Stderr, "Intent: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			intent = strings.TrimSpace(scanner.Text())
		}
	}
	if intent == "" {
		fmt.Fprintln(os.Stderr, "task start: empty intent")
		os.Exit(2)
	}

	// v1 simplification: the CLI shells out via the broker by doing a
	// short health probe then borrowing a process-local broker for the
	// intake call. The "production" path is the HTTP intake endpoint
	// which is a v1.1 task; v1 ships a usable CLI surface against a
	// running broker by spinning up an in-process Broker for the
	// intake roundtrip alone. This keeps the CLI testable end-to-end
	// without requiring a real broker process during integration tests.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if err := runTaskStartInProcess(ctx, intent, *autoAssign); err != nil {
		fmt.Fprintf(os.Stderr, "task start: %v\n", err)
		os.Exit(1)
	}
}

// runTaskStartInProcess is the broker-borrowed implementation. It is
// exported for tests via the taskStartInProcessHook seam below.
func runTaskStartInProcess(ctx context.Context, intent, autoAssignOverride string) error {
	broker, provider, err := taskStartHook(ctx)
	if err != nil {
		return err
	}
	if broker == nil || provider == nil {
		return fmt.Errorf("broker or intake provider unavailable")
	}

	fmt.Fprint(os.Stderr, "Interrogating spec... ")
	startedAt := time.Now()
	tickStop := streamElapsed(startedAt)
	outcome, err := broker.StartIntake(ctx, intent, provider)
	close(tickStop)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}

	printSpec(outcome.Spec)

	confirm := func() (bool, error) {
		fmt.Fprint(os.Stderr, "Start running this task? (y/n) ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return false, scanner.Err()
		}
		ans := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return ans == "y" || ans == "yes", nil
	}

	autoAssign := strings.TrimSpace(autoAssignOverride)
	if autoAssign == "" {
		autoAssign = strings.TrimSpace(outcome.AutoAssign)
	}

	confirmed := false
	if autoAssign != "" && outcome.Countdown != nil {
		fmt.Fprintf(os.Stderr, "Auto-assigning to %s in 3s — press any key to cancel...\n", autoAssign)
		cancelCh := make(chan struct{})
		go func() {
			buf := make([]byte, 1)
			_, _ = os.Stdin.Read(buf)
			outcome.Countdown.Cancel()
			close(cancelCh)
		}()
		if outcome.Countdown.Wait(ctx) {
			confirmed = true
		} else {
			ok, err := confirm()
			if err != nil {
				return err
			}
			confirmed = ok
		}
		_ = cancelCh
	} else {
		ok, err := confirm()
		if err != nil {
			return err
		}
		confirmed = ok
	}

	if !confirmed {
		fmt.Fprintln(os.Stderr, "Cancelled. Task left in intake.")
		return nil
	}

	if err := broker.TransitionLifecycle(outcome.TaskID, team.LifecycleStateReady, "task start: human confirmed"); err != nil {
		return fmt.Errorf("intake → ready: %w", err)
	}
	if err := broker.TransitionLifecycle(outcome.TaskID, team.LifecycleStateRunning, "task start: ready → running"); err != nil {
		return fmt.Errorf("ready → running: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Task %s now running.\n", outcome.TaskID)
	return nil
}

// taskStartHook is the test seam: production overrides this with a
// helper that boots a transient broker + intake provider; tests inject
// a fake.
var taskStartHook = func(ctx context.Context) (*team.Broker, team.IntakeProvider, error) {
	return nil, nil, fmt.Errorf("task start: not yet wired to a live broker (set WUPHF_TASK_PROVIDER for tests)")
}

func streamElapsed(start time.Time) chan struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "%ds ", int(time.Since(start).Seconds()))
			}
		}
	}()
	return stop
}

func printSpec(spec team.Spec) {
	fmt.Println("--- Spec -----------------------------")
	fmt.Printf("Problem:        %s\n", spec.Problem)
	if spec.TargetOutcome != "" {
		fmt.Printf("Target outcome: %s\n", spec.TargetOutcome)
	}
	fmt.Printf("Assignment:     %s\n", spec.Assignment)
	if len(spec.AcceptanceCriteria) > 0 {
		fmt.Println("Acceptance criteria:")
		for i, ac := range spec.AcceptanceCriteria {
			fmt.Printf("  %d. %s\n", i+1, ac.Statement)
		}
	}
	if len(spec.Constraints) > 0 {
		fmt.Println("Constraints:")
		for _, c := range spec.Constraints {
			fmt.Printf("  - %s\n", c)
		}
	}
	if spec.AutoAssign != "" {
		fmt.Printf("Auto-assign:    %s\n", spec.AutoAssign)
	}
	fmt.Println("--------------------------------------")
}

// runTaskList implements `wuphf task list`. Calls GET /tasks/inbox,
// groups by LifecycleState, prints to stdout.
func runTaskList(args []string) {
	fs := flag.NewFlagSet("task list", flag.ExitOnError)
	filter := fs.String("filter", "all", "Inbox filter (needs_decision/running/blocked/merged_today/all)")
	jsonOut := fs.Bool("json", false, "Print the raw inbox payload as JSON")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf task list — print the inbox grouped by lifecycle state")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf task list                            All tasks across all states")
		fmt.Fprintln(os.Stderr, "  wuphf task list --filter=needs_decision    Only the inbox-headline filter")
		fmt.Fprintln(os.Stderr, "  wuphf task list --json                     Emit the raw broker payload")
	}
	_ = fs.Parse(args)
	url := brokerURL("/tasks/inbox?filter=" + *filter)
	req, err := newBrokerRequest(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task list: build request: %v\n", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task list: %v\n", err)
		fmt.Fprintln(os.Stderr, "  (is the broker running? try `wuphf` first)")
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "task list: broker returned %d: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		os.Exit(1)
	}
	if *jsonOut {
		fmt.Println(string(body))
		return
	}
	var payload team.InboxPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "task list: parse: %v\n", err)
		os.Exit(1)
	}
	printInboxPayload(payload)
}

func printInboxPayload(payload team.InboxPayload) {
	if len(payload.Rows) == 0 {
		fmt.Println("Inbox is empty.")
		return
	}
	groups := make(map[team.LifecycleState][]team.InboxRow)
	for _, r := range payload.Rows {
		groups[r.LifecycleState] = append(groups[r.LifecycleState], r)
	}
	states := make([]team.LifecycleState, 0, len(groups))
	for s := range groups {
		states = append(states, s)
	}
	sort.Slice(states, func(i, j int) bool { return string(states[i]) < string(states[j]) })
	for _, s := range states {
		fmt.Printf("\n%s (%d)\n", strings.ToUpper(string(s)), len(groups[s]))
		for _, row := range groups[s] {
			elapsed := time.Duration(row.ElapsedMs) * time.Millisecond
			fmt.Printf("  %-12s  %s  (%s ago)\n", row.TaskID, row.Title, formatElapsed(elapsed))
		}
	}
	fmt.Printf("\nNeeds decision: %d   Running: %d   Blocked: %d   Merged today: %d\n",
		payload.Counts.NeedsDecision, payload.Counts.Running, payload.Counts.Blocked, payload.Counts.MergedToday)
}

func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// runTaskReview opens the Decision Packet view in the user's default
// browser. No broker round-trip — we just construct the URL.
func runTaskReview(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "task review: missing task id")
		os.Exit(2)
	}
	id := strings.TrimSpace(args[0])
	if !isSafeTaskID(id) {
		fmt.Fprintln(os.Stderr, "task review: task id contains invalid characters")
		os.Exit(2)
	}
	url := brokerBaseURL() + "#/task/" + id
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "task review: open %s: %v\n", url, err)
		fmt.Fprintln(os.Stderr, "  Open it manually in your browser instead.")
		os.Exit(1)
	}
}

// isSafeTaskID guards the small set of characters teamTask IDs use today.
// We do not pass arbitrary URL-encoded ids to `open`/`xdg-open`/`rundll32`
// to avoid surprises with shell-meta characters that some launchers
// re-parse internally.
func isSafeTaskID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// runTaskBlock implements `wuphf task block <id> --on <ref>`. POSTs
// to /tasks/{id}/block on the running broker.
func runTaskBlock(args []string) {
	fs := flag.NewFlagSet("task block", flag.ExitOnError)
	on := fs.String("on", "", "PR or task ID this task is blocked on (required)")
	reason := fs.String("reason", "", "Optional human-readable reason")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf task block — mark a task blocked on a PR or task")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf task block <id> --on <pr-or-task-id> [--reason \"text\"]")
	}
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "task block: missing task id")
		os.Exit(2)
	}
	id := strings.TrimSpace(fs.Arg(0))
	if !isSafeTaskID(id) {
		fmt.Fprintln(os.Stderr, "task block: task id contains invalid characters")
		os.Exit(2)
	}
	if strings.TrimSpace(*on) == "" {
		fmt.Fprintln(os.Stderr, "task block: --on is required")
		os.Exit(2)
	}
	body, err := json.Marshal(map[string]string{"on": *on, "reason": *reason})
	if err != nil {
		fmt.Fprintf(os.Stderr, "task block: encode body: %v\n", err)
		os.Exit(1)
	}
	url := brokerURL("/tasks/" + id + "/block")
	req, err := newBrokerRequest(context.Background(), http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "task block: build request: %v\n", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task block: %v\n", err)
		fmt.Fprintln(os.Stderr, "  (is the broker running? try `wuphf` first)")
		os.Exit(1)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "task block: broker returned %d: %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
		os.Exit(1)
	}
	fmt.Printf("Task %s blocked on %s.\n", id, *on)
}
