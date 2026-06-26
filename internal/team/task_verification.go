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
	"crypto/sha256"
	"encoding/hex"
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
// without b.mu held. workDir is the directory the task's work actually
// lives in — its worktree, or the owner's agent scratch dir when the task
// has none. Callers must never pass the broker process cwd: a relative
// `test -f` probe against the host launch directory can false-pass on
// stale host files (V3-N5) and never sees the agent's real deliverable.
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
	// Approve on a pre-execution task is a START (un-park / staff), not a
	// done-claim — running the definition-of-done check before any work has
	// happened would block the human's start affordance forever. The check
	// binds on the completing transition instead.
	if strings.TrimSpace(body.Action) == "approve" && isPreExecutionLifecycleState(task.LifecycleState) {
		b.mu.Unlock()
		return nil
	}
	// Complete on a parked (pre-execution) task is refused by the parked-
	// task gate in the locked mutation phase for every non-internal actor.
	// Let that gate own the error: failing the DoD check here instead told
	// the agent the work just needs fixing when the real blocker is that
	// the task was never started — and executed a command before any work
	// could exist. Internal recovery actors still run the check, since the
	// parked-task gate exempts them.
	if strings.TrimSpace(body.Action) == "complete" && isPreExecutionLifecycleState(task.LifecycleState) &&
		!isInternalTaskActor(strings.TrimSpace(body.CreatedBy)) {
		b.mu.Unlock()
		return nil
	}
	v := *task.Verification
	workDir := strings.TrimSpace(task.WorktreePath)
	owner := strings.TrimSpace(task.Owner)
	b.mu.Unlock()

	if v.Kind == taskVerificationKindNone {
		return nil
	}

	// No task worktree: the owner's turns ran in their agent scratch dir
	// (headless_workspace.go), so the check must look there — never the
	// broker process cwd, where a relative file probe can false-pass on
	// stale host-repo files (V3-N5/J3: landing/index.html predating the
	// run) and never sees the agent's real deliverable.
	if workDir == "" {
		workDir = agentScratchDir(owner)
	}

	// Execute phase: no lock held.
	res := runTaskVerification(v, workDir)

	// Stamp phase: re-find by ID (the task may have been mutated while the
	// check ran) and persist the result. Specs are immutable after create
	// today, but fail closed anyway: if the task's spec no longer matches
	// the one we executed (task replaced under the same ID), discard the
	// result instead of stamping a stale verdict (review TOCTOU finding).
	// UpdatedAt is deliberately NOT touched here — a verification attempt
	// is not a mutation of the task's logical state; CheckedAt on the
	// result carries the attempt time.
	b.mu.Lock()
	if task := b.taskByIDLocked(id); task != nil {
		if task.Verification == nil || task.Verification.Kind != v.Kind || task.Verification.Spec != v.Spec {
			b.mu.Unlock()
			return taskMutationError(
				TaskMutationConflict,
				fmt.Sprintf("task %s verification spec changed while the check ran — retry the action", id),
				nil,
			)
		}
		resCopy := res
		task.VerificationResult = &resCopy
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

// ── Resubmission artifact delta (done-integrity fix family) ─────────────────
//
// ICP-eval v2 [00:30]: after a human request-changes, @ae announced "revised
// and back in review" while the artifact on disk was byte-identical — the
// review gate accepted a resubmission with zero artifact delta. The broker
// now stamps a content hash of the delivered artifact at request_changes
// time (TaskReviewObjection.ArtifactHash) and, on the next agent
// submit_for_review/complete, requires the artifact bytes to have CHANGED.
// When the artifact cannot be read (no worktree, external/visual-artifact
// reference), the resubmission is allowed but the action log records that
// no delta could be verified.

// taskResubmitUnverifiedActionKind is the audit-trail action kind stamped
// when a changes-requested task is resubmitted without a verifiable
// artifact delta. Not a notify-loop kind — audit only.
const taskResubmitUnverifiedActionKind = "task_resubmit_unverified"

// taskResubmitGateAction reports whether the mutation re-lands work that a
// reviewer previously bounced.
func taskResubmitGateAction(action string) bool {
	switch action {
	case "submit_for_review", "complete":
		return true
	}
	return false
}

// resolveTaskArtifactFile maps a wiki-relative artifact reference onto a
// readable file, trying the task worktree first, then the wiki root.
// Returns "" when the reference is not a readable file anywhere (external
// reference, visual-artifact id, missing roots).
func resolveTaskArtifactFile(artifact, workDir, wikiRoot string) string {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" || validateTaskArtifactPath(artifact) != nil {
		return ""
	}
	rel := filepath.FromSlash(artifact)
	for _, root := range []string{strings.TrimSpace(workDir), strings.TrimSpace(wikiRoot)} {
		if root == "" {
			continue
		}
		full := filepath.Join(root, rel)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full
		}
	}
	return ""
}

// taskArtifactExists reports whether an artifact reference resolves to a
// real deliverable: a readable file in the task worktree or the wiki repo,
// or a stored visual artifact (ra_<hex> id with its metadata on disk).
// B5 knowledge-integrity: "done" with a Definition requires the artifact to
// EXIST, not merely be a non-empty string — the v3 run leaked chat-only
// deliverables through phantom artifact paths (QBR one-pager = msg-303
// only, V3-N10). MUST be called without b.mu held (file I/O).
func taskArtifactExists(artifact, workDir, wikiRoot string) bool {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return false
	}
	if validateRichArtifactID(artifact) == nil {
		wikiRoot = strings.TrimSpace(wikiRoot)
		if wikiRoot == "" {
			return false
		}
		meta := filepath.Join(wikiRoot, filepath.FromSlash(richArtifactMetaPath(artifact)))
		info, err := os.Stat(meta)
		return err == nil && !info.IsDir()
	}
	return resolveTaskArtifactFile(artifact, workDir, wikiRoot) != ""
}

// peekTaskDoneArtifact is the unlocked pre-phase for the B5 done-artifact
// existence gate in MutateTask. It peeks (under lock) the artifact reference
// this mutation would land done with — body.ArtifactPath when set, else the
// task's stored artifact — and stats it outside the lock. Returns the
// reference checked and whether it exists; ("", true) when the task has no
// Definition (no gate) or cannot be found (downstream handles not-found).
func (b *Broker) peekTaskDoneArtifact(body TaskPostRequest) (string, bool) {
	id := strings.TrimSpace(body.ID)
	if id == "" {
		return "", true
	}
	b.mu.Lock()
	task := b.taskByIDLocked(id)
	if task == nil || task.Definition == nil {
		b.mu.Unlock()
		return "", true
	}
	artifact := strings.TrimSpace(body.ArtifactPath)
	if artifact == "" {
		artifact = strings.TrimSpace(task.Artifact)
	}
	workDir := strings.TrimSpace(task.WorktreePath)
	wikiRoot := b.wikiRootLocked()
	b.mu.Unlock()
	if artifact == "" {
		return "", false
	}
	if workDir == "" && wikiRoot == "" {
		// No root to verify against (no wiki backend, no worktree): degrade
		// open — the same posture as the resubmission-delta gate. The live
		// system always has a wiki root, so the existence gate binds there.
		return artifact, true
	}
	return artifact, taskArtifactExists(artifact, workDir, wikiRoot)
}

// hashTaskArtifactFile returns "sha256:<hex>" of the file content, or ""
// when the file cannot be read. MUST be called without b.mu held.
func hashTaskArtifactFile(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// wikiRootLocked returns the active wiki repo root, or "". Caller holds b.mu.
func (b *Broker) wikiRootLocked() string {
	if b.wikiWorker == nil || b.wikiWorker.Repo() == nil {
		return ""
	}
	return b.wikiWorker.Repo().Root()
}

// computeTaskArtifactHash peeks the task's artifact reference under lock and
// hashes the resolved file outside it (same lock discipline as the
// verification gate). Returns the artifact reference it hashed and the hash
// ("" when unreadable). MUST be called without b.mu held.
func (b *Broker) computeTaskArtifactHash(taskID string) (artifact, hash string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", ""
	}
	b.mu.Lock()
	task := b.taskByIDLocked(taskID)
	if task == nil || strings.TrimSpace(task.Artifact) == "" {
		b.mu.Unlock()
		return "", ""
	}
	artifact = strings.TrimSpace(task.Artifact)
	workDir := strings.TrimSpace(task.WorktreePath)
	wikiRoot := b.wikiRootLocked()
	b.mu.Unlock()
	return artifact, hashTaskArtifactFile(resolveTaskArtifactFile(artifact, workDir, wikiRoot))
}

// gateTaskResubmissionArtifactDelta is the MutateTask pre-phase for agent
// resubmissions of changes-requested work. It returns nil when the mutation
// may proceed and a TaskMutationInvalid error when the artifact is
// byte-identical to the version the reviewer bounced. Runs alongside the
// verification peek — file reads happen OUTSIDE b.mu. Human actors are
// exempt (the human knows what they reviewed; their actions also clear the
// objection on the locked path).
func (b *Broker) gateTaskResubmissionArtifactDelta(body TaskPostRequest) error {
	action := strings.TrimSpace(body.Action)
	if !taskResubmitGateAction(action) {
		return nil
	}
	actor := strings.TrimSpace(body.CreatedBy)
	if isHumanMessageSender(actor) {
		return nil
	}
	id := strings.TrimSpace(body.ID)
	if id == "" {
		return nil
	}

	// Peek phase: copy what the check needs, under lock.
	b.mu.Lock()
	task := b.taskByIDLocked(id)
	if task == nil || task.ChangesRequested == nil || strings.TrimSpace(task.Artifact) == "" {
		b.mu.Unlock()
		return nil
	}
	storedHash := strings.TrimSpace(task.ChangesRequested.ArtifactHash)
	artifact := strings.TrimSpace(task.Artifact)
	workDir := strings.TrimSpace(task.WorktreePath)
	wikiRoot := b.wikiRootLocked()
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	b.mu.Unlock()

	// Compare phase: no lock held.
	currentHash := hashTaskArtifactFile(resolveTaskArtifactFile(artifact, workDir, wikiRoot))
	if storedHash != "" && currentHash != "" {
		if currentHash == storedHash {
			return taskMutationError(
				TaskMutationInvalid,
				fmt.Sprintf("cannot %s %s: resubmission requires an artifact change — the artifact (%s) is byte-identical to the version changes were requested on. Edit the deliverable to address the feedback, then resubmit.", action, id, artifact),
				nil,
			)
		}
		return nil
	}

	// Stamp phase: the delta could not be verified (no readable worktree or
	// wiki copy, external reference, or no hash was captured at
	// request_changes time). Allow the resubmission but leave the audit
	// line. Persisted by the resubmitting mutation's own saveLocked.
	b.mu.Lock()
	if t := b.taskByIDLocked(id); t != nil && t.ChangesRequested != nil {
		b.appendActionLocked(taskResubmitUnverifiedActionKind, "office", channel, actor,
			truncateSummary("resubmitted without verifiable artifact delta: "+artifact, 140), id)
	}
	b.mu.Unlock()
	return nil
}
