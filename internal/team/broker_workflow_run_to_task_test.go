package team

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/workflow"
)

func TestRunTaskTitleLine(t *testing.T) {
	if got := runTaskTitleLine("Draft the replies\nand file them", 80); got != "Draft the replies" {
		t.Fatalf("title should be the first line, got %q", got)
	}
	if got := runTaskTitleLine("", 80); got != "" {
		t.Fatalf("empty prompt -> empty title, got %q", got)
	}
	long := strings.Repeat("x", 200)
	if got := runTaskTitleLine(long, 80); len([]rune(got)) > 81 {
		t.Fatalf("title should be capped, got %d runes", len([]rune(got)))
	}
}

func TestComposeRunTaskDetails(t *testing.T) {
	run := workflow.RunRecord{
		At: "2026-06-19T00:00:00Z",
		Result: workflow.RunResult{
			StateSeq: []string{"start", "done"},
			Outputs:  map[string]any{"digest": "the digest body", "email_count": 25},
		},
	}
	d := composeRunTaskDetails("Daily email digest from Gmail", run, "Draft the replies.")
	for _, want := range []string{
		"Daily email digest from Gmail", "start → done", "Produced (25 items)",
		"the digest body", "What to do:", "Draft the replies.",
	} {
		if !strings.Contains(d, want) {
			t.Fatalf("details missing %q:\n%s", want, d)
		}
	}
}

// An empty prompt is allowed — the run context alone is a valid task brief.
func TestComposeRunTaskDetailsNoPrompt(t *testing.T) {
	d := composeRunTaskDetails("WF", workflow.RunRecord{Result: workflow.RunResult{StateSeq: []string{"start"}}}, "")
	if strings.Contains(d, "What to do:") {
		t.Fatalf("no prompt -> no 'What to do' section: %s", d)
	}
}
