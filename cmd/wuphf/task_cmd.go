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
// All subcommands talk to a running `wuphf` broker over its existing HTTP
// API. `task start` posts to /tasks/intake (broker-only auth) for the
// intake roundtrip, then to /tasks/{id}/transition for the ready and
// running advances. `task list` calls GET /tasks/inbox; `task block` calls
// POST /tasks/{id}/block. `task review` opens the Decision Packet URL in
// the user's default browser.
//
// All HTTP calls go through newBrokerRequest, which reads the broker
// auth token from $WUPHF_BROKER_TOKEN (preferred) or the on-disk
// brokeraddr.ResolveTokenFile() path, matching `wuphf log`'s pattern.
//
// The package-level brokerClient hook lets tests inject a fake without
// standing up a real broker process. Production code uses the
// httpBrokerClient implementation, which targets the local broker the
// user has running (default 127.0.0.1:7890; override via brokeraddr).
//
// Lane B exposes IntakeOutcome + AutoAssignCountdown + ErrIntakeNoProvider
// for this caller; the CLI honours Spec.AutoAssign with a 3-second
// cancellable countdown over stdin keypresses.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Auth: every subcommand reads the broker token from $WUPHF_BROKER_TOKEN")
	fmt.Fprintln(os.Stderr, "      (preferred) or the on-disk token file written by `wuphf` on start.")
}

// brokerClient is the thin interface the CLI uses to talk to a running
// broker. Production code installs httpBrokerClient via
// brokerClientFactory; tests inject a fake for unit coverage and a real
// httptest-backed client for ICP/integration coverage.
type brokerClient interface {
	StartIntake(ctx context.Context, intent string) (*team.IntakeOutcome, error)
	TransitionLifecycle(ctx context.Context, taskID string, to team.LifecycleState, reason string) error
	BlockTask(ctx context.Context, taskID, on, reason string) error
	ListInbox(ctx context.Context, filter string) (team.InboxPayload, error)
}

// brokerClientFactory builds a brokerClient on demand. Tests override this
// to inject a fake; production callers leave it nil and fall through to
// the default httpBrokerClient against the local broker.
var brokerClientFactory func() brokerClient

// resolveBrokerClient returns the brokerClient the CLI should use for the
// current invocation. Tests set brokerClientFactory; production paths
// fall through to httpBrokerClient against the local broker.
func resolveBrokerClient() brokerClient {
	if brokerClientFactory != nil {
		if c := brokerClientFactory(); c != nil {
			return c
		}
	}
	return defaultHTTPBrokerClient()
}

// httpBrokerClient is the production implementation of brokerClient. It
// posts JSON to the running broker via the same newBrokerRequest helper
// every other command in this package uses, so the auth token and base
// URL come from the same source of truth.
type httpBrokerClient struct {
	httpClient *http.Client
}

func defaultHTTPBrokerClient() brokerClient {
	return &httpBrokerClient{httpClient: &http.Client{Timeout: 35 * time.Second}}
}

func (c *httpBrokerClient) StartIntake(ctx context.Context, intent string) (*team.IntakeOutcome, error) {
	body, err := json.Marshal(map[string]string{"intent": intent})
	if err != nil {
		return nil, fmt.Errorf("intake: marshal: %w", err)
	}
	req, err := newBrokerRequest(ctx, http.MethodPost, brokerURL("/tasks/intake"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w (is the broker running? try `wuphf` first)", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("intake: broker returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var wire struct {
		TaskID     string    `json:"taskId"`
		Spec       team.Spec `json:"spec"`
		AutoAssign string    `json:"autoAssign,omitempty"`
	}
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return nil, fmt.Errorf("intake: parse response: %w", err)
	}
	out := &team.IntakeOutcome{
		TaskID:     wire.TaskID,
		Spec:       wire.Spec,
		AutoAssign: wire.AutoAssign,
	}
	if wire.AutoAssign != "" {
		out.Countdown = team.NewAutoAssignCountdown()
	}
	return out, nil
}

func (c *httpBrokerClient) TransitionLifecycle(ctx context.Context, taskID string, to team.LifecycleState, reason string) error {
	if !isSafeTaskID(taskID) {
		return fmt.Errorf("transition: task id contains invalid characters")
	}
	body, err := json.Marshal(map[string]string{"to": string(to), "reason": reason})
	if err != nil {
		return fmt.Errorf("transition: marshal: %w", err)
	}
	req, err := newBrokerRequest(ctx, http.MethodPost, brokerURL("/tasks/"+taskID+"/transition"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w (is the broker running?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("transition: broker returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func (c *httpBrokerClient) BlockTask(ctx context.Context, taskID, on, reason string) error {
	if !isSafeTaskID(taskID) {
		return fmt.Errorf("block: task id contains invalid characters")
	}
	body, err := json.Marshal(map[string]string{"on": on, "reason": reason})
	if err != nil {
		return fmt.Errorf("block: marshal: %w", err)
	}
	req, err := newBrokerRequest(ctx, http.MethodPost, brokerURL("/tasks/"+taskID+"/block"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w (is the broker running?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("block: broker returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func (c *httpBrokerClient) ListInbox(ctx context.Context, filter string) (team.InboxPayload, error) {
	url := brokerURL("/tasks/inbox?filter=" + filter)
	req, err := newBrokerRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return team.InboxPayload{}, err
	}
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return team.InboxPayload{}, fmt.Errorf("%w (is the broker running? try `wuphf` first)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return team.InboxPayload{}, fmt.Errorf("broker returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload team.InboxPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return team.InboxPayload{}, fmt.Errorf("parse: %w", err)
	}
	return payload, nil
}

// runTaskStart implements `wuphf task start [intent]`. It posts the
// intent to a running broker via POST /tasks/intake, prompts the user
// to confirm, then posts intake → ready → running transitions.
func runTaskStart(args []string) {
	fs := flag.NewFlagSet("task start", flag.ExitOnError)
	autoAssign := fs.String("auto-assign", "", "Override Spec.AutoAssign (skip the countdown)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf task start — drive an intent through intake")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf task start \"fix the cache invalidation bug\"")
		fmt.Fprintln(os.Stderr, "  wuphf task start                       # reads the intent from stdin")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Auth: reads broker token from $WUPHF_BROKER_TOKEN or the on-disk token file.")
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

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if err := runTaskStartWithClient(ctx, resolveBrokerClient(), intent, *autoAssign, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "task start: %v\n", err)
		os.Exit(1)
	}
}

// runTaskStartWithClient is the testable core. The brokerClient is the
// live HTTP wire in production and a fake/httptest-backed impl in tests.
// stdin is parameterised so tests can pipe a confirmation answer.
func runTaskStartWithClient(ctx context.Context, client brokerClient, intent, autoAssignOverride string, stdin io.Reader) error {
	if client == nil {
		return errors.New("broker client unavailable")
	}

	fmt.Fprint(os.Stderr, "Interrogating spec... ")
	startedAt := time.Now()
	tickStop := streamElapsed(startedAt)
	outcome, err := client.StartIntake(ctx, intent)
	close(tickStop)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	if outcome == nil {
		return errors.New("intake: empty outcome")
	}

	printSpec(outcome.Spec)

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
			_, _ = stdin.Read(buf)
			outcome.Countdown.Cancel()
			close(cancelCh)
		}()
		if outcome.Countdown.Wait(ctx) {
			confirmed = true
		} else {
			ok, err := promptYesNo(stdin, "Start running this task? (y/n) ")
			if err != nil {
				return err
			}
			confirmed = ok
		}
		_ = cancelCh
	} else {
		ok, err := promptYesNo(stdin, "Start running this task? (y/n) ")
		if err != nil {
			return err
		}
		confirmed = ok
	}

	if !confirmed {
		fmt.Fprintln(os.Stderr, "Cancelled. Task left in intake.")
		return nil
	}

	if err := client.TransitionLifecycle(ctx, outcome.TaskID, team.LifecycleStateReady, "task start: human confirmed"); err != nil {
		return fmt.Errorf("intake → ready: %w", err)
	}
	if err := client.TransitionLifecycle(ctx, outcome.TaskID, team.LifecycleStateRunning, "task start: ready → running"); err != nil {
		return fmt.Errorf("ready → running: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Task %s now running.\n", outcome.TaskID)
	return nil
}

// promptYesNo (F-FU-4) is the single y/n prompt helper used by every
// task subcommand that needs an interactive confirmation. Reads one
// line from stdin, treats "y" / "yes" (case-insensitive, trimmed) as
// affirmative, anything else as negative. Empty input returns false.
func promptYesNo(stdin io.Reader, prompt string) (bool, error) {
	fmt.Fprint(os.Stderr, prompt)
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	ans := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return ans == "y" || ans == "yes", nil
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

// runTaskList implements `wuphf task list`. Calls GET /tasks/inbox via
// the brokerClient, groups by LifecycleState, prints to stdout.
//
// Auth: $WUPHF_BROKER_TOKEN env var (preferred) or the on-disk token
// file at brokeraddr.ResolveTokenFile(). Same source of truth as
// `wuphf log`. (F-FU-2)
func runTaskList(args []string) {
	fs := flag.NewFlagSet("task list", flag.ExitOnError)
	filter := fs.String("filter", "all", "Inbox filter (needs_decision/running/blocked/merged_today/all)")
	jsonOut := fs.Bool("json", false, "Print the raw inbox payload as JSON (writes to stdout; errors to stderr)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf task list — print the inbox grouped by lifecycle state")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf task list                            All tasks across all states")
		fmt.Fprintln(os.Stderr, "  wuphf task list --filter=needs_decision    Only the inbox-headline filter")
		fmt.Fprintln(os.Stderr, "  wuphf task list --json                     Emit the raw broker payload")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Auth: reads broker token from $WUPHF_BROKER_TOKEN (preferred) or the")
		fmt.Fprintln(os.Stderr, "on-disk token file written by `wuphf` on broker start. Pipe-safe: data goes")
		fmt.Fprintln(os.Stderr, "to stdout, errors to stderr.")
	}
	_ = fs.Parse(args)
	client := resolveBrokerClient()
	payload, err := client.ListInbox(context.Background(), *filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task list: %v\n", err)
		os.Exit(1)
	}
	if *jsonOut {
		raw, _ := json.Marshal(payload)
		fmt.Println(string(raw))
		return
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
	if err := openBrowser(context.Background(), url); err != nil {
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

func openBrowser(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	return cmd.Start()
}

// runTaskBlock implements `wuphf task block <id> --on <ref>`. POSTs
// to /tasks/{id}/block on the running broker via the brokerClient.
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
	if err := resolveBrokerClient().BlockTask(context.Background(), id, *on, *reason); err != nil {
		fmt.Fprintf(os.Stderr, "task block: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Task %s blocked on %s.\n", id, *on)
}
