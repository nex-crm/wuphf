package team

// broker_onboarding_scratch_wiki_test.go — regression test for the
// skip-website scratch path's wiki seeding (issue #940).
//
// Before this fix, materializeScratchWikiStubs was orphaned and the
// skip-website scratch path wrote nothing under <wiki>/team/about/. The
// with-website path (runScanPhase → SeedCompanyContext) already seeds the
// same three files via PR #963; the skip-website path now matches.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/onboarding"
)

// TestRunSeedPhaseScratchMaterializesAboutSkeleton asserts the scratch
// (blueprintID="") path writes <wiki>/team/about/{README,company,owner}.md
// after runSeedPhase returns. This is the deliberate paired surface with
// SeedCompanyContext on the with-website path: both branches land the user
// in an office with a populated team/about/ section.
func TestRunSeedPhaseScratchMaterializesAboutSkeleton(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	b := newTestBroker(t)
	s := &onboarding.State{
		Version: 2,
		FormAnswers: onboarding.FormAnswers{
			CompanyName: "Scratch Skip-Website Corp",
			Description: "We test the empty office seeder.",
			OwnerName:   "Sam Founder",
			OwnerRole:   "CEO",
			// BlueprintID intentionally empty → scratch path.
		},
	}

	if err := b.runSeedPhase(s); err != nil {
		t.Fatalf("runSeedPhase: %v", err)
	}

	wikiRoot := filepath.Join(tmpHome, ".wuphf", "wiki")
	aboutDir := filepath.Join(wikiRoot, "team", "about")

	for _, name := range []string{"README.md", "company.md", "owner.md"} {
		path := filepath.Join(aboutDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s on skip-website scratch path, got err=%v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s exists but is empty — placeholder body did not land", path)
		}
	}

	// Sanity: the company.md body carries the company name from FormAnswers
	// (no website was scanned, so the wiki has to fall back to the form).
	companyMD, err := os.ReadFile(filepath.Join(aboutDir, "company.md"))
	if err != nil {
		t.Fatalf("read company.md: %v", err)
	}
	if want := "Scratch Skip-Website Corp"; !strings.Contains(string(companyMD), want) {
		t.Fatalf("company.md missing company name %q:\n%s", want, companyMD)
	}

	// Owner.md should carry the owner name.
	ownerMD, err := os.ReadFile(filepath.Join(aboutDir, "owner.md"))
	if err != nil {
		t.Fatalf("read owner.md: %v", err)
	}
	if want := "Sam Founder"; !strings.Contains(string(ownerMD), want) {
		t.Fatalf("owner.md missing owner name %q:\n%s", want, ownerMD)
	}
}

// TestRunSeedPhaseScratchIdempotent asserts re-running the scratch seeder
// over an existing about/ skeleton does not clobber user edits. The
// O_CREATE|O_EXCL guard in writeWikiStubIfAbsent is the load-bearing
// invariant.
func TestRunSeedPhaseScratchIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	b := newTestBroker(t)
	s := &onboarding.State{
		Version: 2,
		FormAnswers: onboarding.FormAnswers{
			CompanyName: "Idempotent Corp",
		},
	}
	if err := b.runSeedPhase(s); err != nil {
		t.Fatalf("first runSeedPhase: %v", err)
	}

	companyPath := filepath.Join(tmpHome, ".wuphf", "wiki", "team", "about", "company.md")
	userBody := []byte("# Idempotent Corp\n\nHand-edited by the user.\n")
	if err := os.WriteFile(companyPath, userBody, 0o644); err != nil {
		t.Fatalf("user-edit simulation: %v", err)
	}

	// Re-run with the same form state (e.g. user re-enters onboarding).
	if err := b.runSeedPhase(s); err != nil {
		t.Fatalf("second runSeedPhase: %v", err)
	}

	got, err := os.ReadFile(companyPath)
	if err != nil {
		t.Fatalf("read company.md after re-run: %v", err)
	}
	if string(got) != string(userBody) {
		t.Fatalf("re-run overwrote user edit:\nwant %q\ngot  %q", userBody, got)
	}
}
