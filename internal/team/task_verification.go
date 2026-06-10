package team

// task_verification.go — U1.1 verification-gated done (docs/specs/sota-uplift.md).
//
// A task may carry a machine-checkable definition of done. When it does and
// the check is required, the broker executes the check BEFORE allowing the
// complete/approve mutation to land: the harness, not the model, declares
// completion. A failing check blocks the transition and the failure output
// is stamped onto the task so it rides into the owner's next execution
// packet — ground truth re-enters agent context instead of evaporating.
//
// Trust model: verification specs are authored by the CEO/human at task
// scoping time (scope-shaping actions are already restricted to CEO+human in
// checkTaskActionAuthLocked). Command checks run with the same trust the
// office already extends to local_worktree execution — agents run arbitrary
// commands in their worktrees every turn; the gate adds a bounded (5 min,
// output-capped) command in that same sandbox, cwd-pinned to the task's
// worktree. It widens no privilege; it only makes "done" conditional.
//
// Lock discipline: the check runs OUTSIDE b.mu (commands can take minutes;
// holding the broker mutex across an exec is the same hazard class that
// killed the old auto-notebook-writer). gateTaskCompletionVerification does
// peek(locked) → execute(unlocked) → stamp(locked), re-finding the task by
// ID at each locked phase so concurrent mutations stay safe.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TaskVerification is the machine-checkable definition of done on a task.
type TaskVerification struct {
	// Kind is one of "command", "artifact", "url", "none".
	Kind string `json:"kind"`
	// Spec is kind-specific: a shell command (command), a file path or
	// glob relative to the task worktree (artifact), or an http(s) URL
	// expected to answer 2xx (url).
	Spec string `json:"spec,omitempty"`
	// Required gates complete/approve on the check passing. When false
	// the check still runs and stamps its result, but never blocks.
	Required bool `json:"required,omitempty"`
}

// TaskVerificationResult is the stamped outcome of the most recent check.
type TaskVerificationResult struct {
	Pass      bool   `json:"pass"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail,omitempty"`
	CheckedAt string `json:"checked_at"`
}

const (
	taskVerificationKindCommand  = "command"
	taskVerificationKindArtifact = "artifact"
	taskVerificationKindURL      = "url"
	taskVerificationKindNone     = "none"

	taskVerificationTimeout   = 5 * time.Minute
	taskVerificationOutputCap = 4000
)

// normalizeTaskVerification validates and canonicalizes a verification spec
// from the wire. Returns nil for empty/none kinds (no gate).
func normalizeTaskVerification(kind, spec string, required bool) (*TaskVerification, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	spec = strings.TrimSpace(spec)
	switch kind {
	case "", taskVerificationKindNone:
		return nil, nil
	case taskVerificationKindCommand, taskVerificationKindArtifact:
		if spec == "" {
			return nil, fmt.Errorf("verification kind %q requires a non-empty spec", kind)
		}
	case taskVerificationKindURL:
		if !strings.HasPrefix(spec, "http://") && !strings.HasPrefix(spec, "https://") {
			return nil, fmt.Errorf("verification kind url requires an http(s) spec; got %q", spec)
		}
	default:
		return nil, fmt.Errorf("unknown verification kind %q (want command|artifact|url|none)", kind)
	}
	return &TaskVerification{Kind: kind, Spec: spec, Required: required}, nil
}

// runTaskVerification executes a verification check. It MUST be called
// without b.mu held. workDir is the task's worktree (or empty for the
// process cwd).
func runTaskVerification(v TaskVerification, workDir string) TaskVerificationResult {
	now := time.Now().UTC().Format(time.RFC3339)
	res := TaskVerificationResult{Kind: v.Kind, CheckedAt: now}
	switch v.Kind {
	case taskVerificationKindCommand:
		ctx, cancel := context.WithTimeout(context.Background(), taskVerificationTimeout)
		defer cancel()
		cmd := verificationShellCommand(ctx, v.Spec)
		if strings.TrimSpace(workDir) != "" {
			cmd.Dir = workDir
		}
		out, err := cmd.CombinedOutput()
		tail := string(out)
		if len(tail) > taskVerificationOutputCap {
			tail = "…" + tail[len(tail)-taskVerificationOutputCap:]
		}
		if ctx.Err() == context.DeadlineExceeded {
			res.Detail = fmt.Sprintf("check timed out after %s\n%s", taskVerificationTimeout, tail)
			return res
		}
		if err != nil {
			res.Detail = fmt.Sprintf("exit: %v\n%s", err, tail)
			return res
		}
		res.Pass = true
		res.Detail = tail
	case taskVerificationKindArtifact:
		pattern := v.Spec
		if !filepath.IsAbs(pattern) && strings.TrimSpace(workDir) != "" {
			pattern = filepath.Join(workDir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			res.Detail = fmt.Sprintf("bad artifact pattern %q: %v", v.Spec, err)
			return res
		}
		var found []string
		for _, m := range matches {
			if info, statErr := os.Stat(m); statErr == nil && !info.IsDir() && info.Size() > 0 {
				found = append(found, m)
			}
		}
		if len(found) == 0 {
			res.Detail = fmt.Sprintf("no non-empty artifact matches %q", v.Spec)
			return res
		}
		res.Pass = true
		res.Detail = "artifacts: " + strings.Join(found, ", ")
	case taskVerificationKindURL:
		client := &http.Client{Timeout: 15 * time.Second}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.Spec, nil)
		if err != nil {
			res.Detail = fmt.Sprintf("GET %s: %v", v.Spec, err)
			return res
		}
		resp, err := client.Do(req)
		if err != nil {
			res.Detail = fmt.Sprintf("GET %s: %v", v.Spec, err)
			return res
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			res.Detail = fmt.Sprintf("GET %s: HTTP %d", v.Spec, resp.StatusCode)
			return res
		}
		res.Pass = true
		res.Detail = fmt.Sprintf("GET %s: HTTP %d", v.Spec, resp.StatusCode)
	default:
		// Unknown kinds never pass — fail closed, mirroring the
		// deterministic-integrations gate posture.
		res.Detail = fmt.Sprintf("unknown verification kind %q", v.Kind)
	}
	return res
}

// taskVerificationGateActions are the mutations that move a task toward a
// done state and therefore must pass the check first. complete is gated even
// when it only routes to review: work that fails its own definition of done
// should not enter review either.
func taskVerificationGateAction(action string) bool {
	switch action {
	case "complete", "approve":
		return true
	}
	return false
}

// gateTaskCompletionVerification is the MutateTask pre-phase. It returns nil
// when the mutation may proceed (no verification, not required, or check
// passed) and a TaskMutationVerificationFailed error when the required check
// failed. The result is stamped onto the task in both cases so reviewers and
// the owner's next packet see ground truth.
func (b *Broker) gateTaskCompletionVerification(body TaskPostRequest) error {
	if !taskVerificationGateAction(strings.TrimSpace(body.Action)) {
		return nil
	}
	id := strings.TrimSpace(body.ID)
	if id == "" {
		return nil
	}

	// Peek phase: copy what the check needs, under lock.
	b.mu.Lock()
	task := b.taskByIDLocked(id)
	if task == nil || task.Verification == nil {
		b.mu.Unlock()
		return nil
	}
	v := *task.Verification
	workDir := strings.TrimSpace(task.WorktreePath)
	b.mu.Unlock()

	if v.Kind == taskVerificationKindNone {
		return nil
	}

	// Execute phase: no lock held.
	res := runTaskVerification(v, workDir)

	// Stamp phase: re-find by ID (the task may have been mutated while the
	// check ran) and persist the result either way.
	b.mu.Lock()
	if task := b.taskByIDLocked(id); task != nil {
		resCopy := res
		task.VerificationResult = &resCopy
		task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			return fmt.Errorf("verification: persist result: %w", err)
		}
	}
	b.mu.Unlock()

	if !res.Pass && v.Required {
		return taskMutationError(
			TaskMutationVerificationFailed,
			fmt.Sprintf("task %s definition-of-done check failed (%s): %s — fix the work, then complete again; the failure output is recorded on the task", id, v.Kind, strings.TrimSpace(res.Detail)),
			nil,
		)
	}
	return nil
}
