package team

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeTaskVerification(t *testing.T) {
	if v, err := normalizeTaskVerification("", "", false); err != nil || v != nil {
		t.Fatalf("empty kind: want nil,nil; got %v,%v", v, err)
	}
	if v, err := normalizeTaskVerification("none", "anything", true); err != nil || v != nil {
		t.Fatalf("none kind: want nil,nil; got %v,%v", v, err)
	}
	if _, err := normalizeTaskVerification("command", "", true); err == nil {
		t.Fatal("command with empty spec: want error")
	}
	if _, err := normalizeTaskVerification("url", "ftp://x", true); err == nil {
		t.Fatal("non-http url: want error")
	}
	if _, err := normalizeTaskVerification("ritual", "x", true); err == nil {
		t.Fatal("unknown kind: want error")
	}
	v, err := normalizeTaskVerification(" Command ", " go test ./... ", true)
	if err != nil || v == nil || v.Kind != "command" || v.Spec != "go test ./..." || !v.Required {
		t.Fatalf("canonicalization: got %+v, %v", v, err)
	}
}

func TestRunTaskVerificationCommand(t *testing.T) {
	if res := runTaskVerification(TaskVerification{Kind: "command", Spec: "exit 0"}, ""); !res.Pass {
		t.Fatalf("exit 0: want pass; detail=%s", res.Detail)
	}
	res := runTaskVerification(TaskVerification{Kind: "command", Spec: "echo broken && exit 3"}, "")
	if res.Pass {
		t.Fatal("exit 3: want fail")
	}
	if !strings.Contains(res.Detail, "broken") {
		t.Fatalf("failure detail must carry command output; got %q", res.Detail)
	}
}

func TestRunTaskVerificationArtifact(t *testing.T) {
	dir := t.TempDir()
	if res := runTaskVerification(TaskVerification{Kind: "artifact", Spec: "out/report.md"}, dir); res.Pass {
		t.Fatal("missing artifact: want fail")
	}
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out", "report.md"), []byte("# done"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := runTaskVerification(TaskVerification{Kind: "artifact", Spec: "out/*.md"}, dir); !res.Pass {
		t.Fatalf("present artifact: want pass; detail=%s", res.Detail)
	}
}

func TestRunTaskVerificationURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if res := runTaskVerification(TaskVerification{Kind: "url", Spec: srv.URL + "/ok"}, ""); !res.Pass {
		t.Fatalf("200 url: want pass; detail=%s", res.Detail)
	}
	if res := runTaskVerification(TaskVerification{Kind: "url", Spec: srv.URL + "/boom"}, ""); res.Pass {
		t.Fatal("500 url: want fail")
	}
}

func newVerificationTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}, {Slug: "eng", Name: "Engineer"}}
	b.channels = []teamChannel{{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}}}
	b.mu.Unlock()
	return b
}

func createVerifiedTask(t *testing.T, b *Broker, spec string) string {
	t.Helper()
	resp, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Gated work " + spec,
		Details: "work with a definition of done", Owner: "eng", CreatedBy: "ceo",
		VerificationKind: "command", VerificationSpec: spec, VerificationRequired: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Pass the human gate: created issues land in Drafting and completion
	// from a pre-start state is impossible by contract (v3 fix family #1).
	// A human approve on a Drafting task ACTIVATES it (drafting→running)
	// without running the DoD check — the check binds on completion.
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "approve", ID: resp.Task.ID, Channel: "general", CreatedBy: "human",
	}); err != nil {
		t.Fatalf("approve & start: %v", err)
	}
	return resp.Task.ID
}

func TestVerificationGateBlocksFailingComplete(t *testing.T) {
	b := newVerificationTestBroker(t)
	id := createVerifiedTask(t, b, "echo not-done-yet && exit 1")

	_, err := b.MutateTask(TaskPostRequest{Action: "complete", ID: id, Channel: "general", CreatedBy: "eng"})
	if err == nil {
		t.Fatal("complete on failing check: want error")
	}
	var mErr *TaskMutationError
	if !errors.As(err, &mErr) || mErr.Kind != TaskMutationVerificationFailed {
		t.Fatalf("want TaskMutationVerificationFailed; got %v", err)
	}
	task := b.TaskByID(id)
	if task == nil || task.VerificationResult == nil || task.VerificationResult.Pass {
		t.Fatalf("failure must be stamped on the task; got %+v", task.VerificationResult)
	}
	if !strings.Contains(task.VerificationResult.Detail, "not-done-yet") {
		t.Fatalf("stamped detail must carry the check output; got %q", task.VerificationResult.Detail)
	}
	if strings.EqualFold(strings.TrimSpace(task.Status()), "done") {
		t.Fatal("task must not be done after a failed gate")
	}
}

func TestVerificationGateAllowsPassingComplete(t *testing.T) {
	b := newVerificationTestBroker(t)
	id := createVerifiedTask(t, b, "exit 0")

	if _, err := b.MutateTask(TaskPostRequest{Action: "complete", ID: id, Channel: "general", CreatedBy: "eng"}); err != nil {
		t.Fatalf("complete on passing check: %v", err)
	}
	task := b.TaskByID(id)
	if task == nil || task.VerificationResult == nil || !task.VerificationResult.Pass {
		t.Fatalf("pass must be stamped; got %+v", task.VerificationResult)
	}
	// Structured-review tasks route to review on complete; approve is the
	// done transition and runs the gate again.
	if _, err := b.MutateTask(TaskPostRequest{Action: "approve", ID: id, Channel: "general", CreatedBy: "ceo"}); err != nil {
		t.Fatalf("approve on passing check: %v", err)
	}
	if got := strings.TrimSpace(b.TaskByID(id).Status()); !strings.EqualFold(got, "done") {
		t.Fatalf("want done; got %q", got)
	}
}

func TestVerificationFailureRendersInExecutionPacket(t *testing.T) {
	b := newVerificationTestBroker(t)
	id := createVerifiedTask(t, b, "echo missing-export-file && exit 2")
	_, _ = b.MutateTask(TaskPostRequest{Action: "complete", ID: id, Channel: "general", CreatedBy: "eng"})

	l := launcherForBrokerFixture(b)
	packet := l.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *b.TaskByID(id), "Back to you.")
	if !strings.Contains(packet, "Machine check") {
		t.Fatalf("packet must carry the verification spec; got:\n%s", packet)
	}
	if !strings.Contains(packet, "LAST VERIFICATION FAILED") || !strings.Contains(packet, "missing-export-file") {
		t.Fatalf("packet must carry the failure output; got:\n%s", packet)
	}
}
