package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// TestRepoCommitAndReadAgentFile exercises the storage layer end to end against
// a real on-disk git wiki: validated write -> git commit -> read back, plus the
// no-clobber (create twice fails), replace, and path-rejection contracts.
func TestRepoCommitAndReadAgentFile(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	rel := agentFileRel("ceo", "SOUL")

	sha, n, err := repo.CommitAgentFile(ctx, "ceo", rel, "# SOUL — @ceo\nbe excellent", "create", "")
	if err != nil {
		t.Fatalf("commit create: %v", err)
	}
	if sha == "" || n == 0 {
		t.Fatalf("expected sha+bytes, got sha=%q n=%d", sha, n)
	}
	if data, err := os.ReadFile(filepath.Join(repo.Root(), rel)); err != nil || !strings.Contains(string(data), "be excellent") {
		t.Fatalf("read back: err=%v data=%q", err, data)
	}

	// create-again must fail so a human's file is never silently clobbered.
	if _, _, err := repo.CommitAgentFile(ctx, "ceo", rel, "x", "create", ""); err == nil {
		t.Fatal("second create must fail")
	}
	// replace updates it.
	if _, _, err := repo.CommitAgentFile(ctx, "ceo", rel, "# SOUL — @ceo\nrevised", "replace", ""); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(repo.Root(), rel)); !strings.Contains(string(data), "revised") {
		t.Fatalf("replace should update content: %q", data)
	}
	// a non-agent path is rejected by the validator inside Commit.
	if _, _, err := repo.CommitAgentFile(ctx, "ceo", "team/people/x.md", "x", "create", ""); err == nil {
		t.Fatal("invalid agent-file path must be rejected")
	}
}

func TestValidateAgentFilePath(t *testing.T) {
	ok := []string{
		"agents/ceo/SOUL.md",
		"agents/eng/IDENTITY.md",
		"agents/growth-ops/OPERATIONS.md",
		"agents/pam/TOOLS.md",
		"office/USER.md",
	}
	for _, p := range ok {
		if err := validateAgentFilePath(p); err != nil {
			t.Errorf("expected %q valid, got %v", p, err)
		}
	}

	bad := []string{
		"",
		"agents/ceo/MEMORY.md",                // not a canonical file
		"agents/ceo/HEARTBEAT.md",             // dropped by design
		"agents/ceo/soul.md",                  // wrong case
		"agents/ceo/notebook/2026-01-01-x.md", // notebook subtree is off-limits
		"agents/ceo/SOUL",                     // missing .md
		"team/people/ceo.md",                  // article subtree
		"agents/../etc/passwd",                // traversal
		"agents/ceo/../eng/SOUL.md",           // traversal into another agent
		"/agents/ceo/SOUL.md",                 // absolute
		"agents/ceo/sub/SOUL.md",              // nested
		"USER.md",                             // office file only at office/USER.md
		"agents/Bad Slug/SOUL.md",             // invalid slug
	}
	for _, p := range bad {
		if err := validateAgentFilePath(p); err == nil {
			t.Errorf("expected %q rejected, got nil", p)
		}
	}
}

func TestRenderAgentFilesDeterministic(t *testing.T) {
	member := officeMember{
		Slug:         "growth",
		Name:         "Growth Lead",
		Role:         "growth lead",
		Expertise:    []string{"acquisition", "funnels"},
		Personality:  "Relentless about pipeline; allergic to vanity metrics.",
		AllowedTools: []string{"team_task", "team_broadcast"},
		Provider:     provider.ProviderBinding{Kind: "codex", Model: "gpt-5.4"},
	}

	soul := renderAgentSoul(member, false)
	for _, want := range []string{"# SOUL — @growth", "Relentless about pipeline", "## Values", "## Boundaries", "growth lead"} {
		if !strings.Contains(soul, want) {
			t.Errorf("SOUL missing %q:\n%s", want, soul)
		}
	}
	// Lead vs specialist SOUL boundary differs.
	if strings.Contains(soul, "You are the lead") {
		t.Error("specialist SOUL must not claim the lead boundary")
	}
	if leadSoul := renderAgentSoul(member, true); !strings.Contains(leadSoul, "You are the lead") {
		t.Errorf("lead SOUL must carry the lead boundary:\n%s", leadSoul)
	}

	identity := renderAgentIdentity(member)
	for _, want := range []string{"# IDENTITY — @growth", "Name: Growth Lead", "Slug: growth", "Expertise: acquisition, funnels", "Runtime: codex / gpt-5.4"} {
		if !strings.Contains(identity, want) {
			t.Errorf("IDENTITY missing %q:\n%s", want, identity)
		}
	}

	ops := renderAgentOperations(member, false)
	if !strings.Contains(ops, "# OPERATIONS — @growth") || !strings.Contains(ops, "claim with team_task") {
		t.Errorf("specialist OPERATIONS unexpected:\n%s", ops)
	}
	if leadOps := renderAgentOperations(member, true); !strings.Contains(leadOps, "decompose it into owned sub-tasks") {
		t.Errorf("lead OPERATIONS must describe decomposition:\n%s", leadOps)
	}

	tools := renderAgentTools(member)
	for _, want := range []string{"# TOOLS — @growth", "- team_task", "- team_broadcast"} {
		if !strings.Contains(tools, want) {
			t.Errorf("TOOLS missing %q:\n%s", want, tools)
		}
	}
	// Empty allowed-tools falls back to the default toolset line.
	if fallback := renderAgentTools(officeMember{Slug: "x"}); !strings.Contains(fallback, "default office toolset") {
		t.Errorf("empty tools should fall back to default toolset:\n%s", fallback)
	}

	if user := renderOfficeUserFile(); !strings.Contains(user, "# USER") || !strings.Contains(user, "single human operator") {
		t.Errorf("USER file unexpected:\n%s", user)
	}
}

func TestAgentFilesPromptBlock(t *testing.T) {
	// No reader wired -> empty (tests that don't mock a wiki backend).
	if got := (&promptBuilder{}).agentFilesPromptBlock("ceo"); got != "" {
		t.Errorf("nil reader should produce empty block, got %q", got)
	}

	// Reader present but all files empty -> empty.
	empty := &promptBuilder{
		agentInstruction: func(string, string) string { return "" },
		officeUser:       func() string { return "" },
	}
	if got := empty.agentFilesPromptBlock("ceo"); got != "" {
		t.Errorf("all-empty files should produce empty block, got %q", got)
	}

	// Files present -> block contains them, in order, with the authoritative header.
	pb := &promptBuilder{
		agentInstruction: func(_, name string) string {
			switch name {
			case "SOUL":
				return "# SOUL — @ceo\nlead persona"
			case "OPERATIONS":
				return "# OPERATIONS — @ceo\ndecompose"
			default:
				return ""
			}
		},
		officeUser: func() string { return "# USER\nthe human" },
	}
	got := pb.agentFilesPromptBlock("ceo")
	for _, want := range []string{"YOUR FILES (authoritative)", "lead persona", "decompose", "the human"} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "lead persona") > strings.Index(got, "the human") {
		t.Error("agent files should precede the office USER file in the block")
	}
}
