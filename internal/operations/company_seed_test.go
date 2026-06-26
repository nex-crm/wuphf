package operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type mockCompleter struct{ response string }

func (m mockCompleter) Complete(_ context.Context, _ string) (string, error) {
	return m.response, nil
}

// mustTempFile creates a temp file with the given content and returns its path.
// Panics on any setup error so table-driven tests fail loudly on bad setups.
func mustTempFile(pattern, content string) string {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		panic(fmt.Sprintf("mustTempFile CreateTemp: %v", err))
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		panic(fmt.Sprintf("mustTempFile WriteString: %v", err))
	}
	if err := f.Close(); err != nil {
		panic(fmt.Sprintf("mustTempFile Close: %v", err))
	}
	return f.Name()
}

func TestSeedCompanyContext(t *testing.T) {
	validJSON := `{"company_name":"Acme","description":"B2B SaaS","industry":"SaaS","audience":"Ops teams","goals":"Launch Q3","key_facts":["Fact 1","Fact 2"]}`

	tests := []struct {
		name  string
		setup func(t *testing.T) // optional per-case setup (e.g. env scrubbing)
		input func(wikiRoot string) CompanySeedInput
		// wantRealCompanyMD is true when company.md should hold a real
		// LLM-derived profile (not the placeholder). The placeholder file
		// is always written so the README link is never dead — see #946 —
		// so we no longer assert on file existence directly.
		wantRealCompanyMD bool
		// wantRealOwnerMD is true when owner.md should hold a real
		// owner profile (not the placeholder).
		wantRealOwnerMD     bool
		wantOwnerContent    []string // substrings that must appear in owner.md
		wantReadme          bool
		wantWarningContains string
	}{
		{
			name: "happy path with owner",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					OwnerName: "Alice",
					OwnerRole: "CEO",
					Completer: mockCompleter{response: validJSON},
					WikiRoot:  wikiRoot,
					FilePaths: []string{mustTempFile("wuphf-seed-*.txt", "Acme is a B2B SaaS company.")},
				}
			},
			wantRealCompanyMD: true,
			wantRealOwnerMD:   true,
			wantReadme:        true,
		},
		{
			name: "JSON fence stripping",
			input: func(wikiRoot string) CompanySeedInput {
				fenced := "```json\n" + validJSON + "\n```"
				return CompanySeedInput{
					Completer: mockCompleter{response: fenced},
					WikiRoot:  wikiRoot,
					FilePaths: []string{mustTempFile("wuphf-seed-*.txt", "Acme builds software.")},
				}
			},
			wantRealCompanyMD: true,
			wantReadme:        true,
		},
		{
			name: "URL scheme rejected",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					WebsiteURL: "file:///etc/passwd",
					WikiRoot:   wikiRoot,
				}
			},
			wantReadme:          true,
			wantWarningContains: "must be http or https",
		},
		{
			name: "both URL and files empty no completer",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					WikiRoot: wikiRoot,
				}
			},
			wantReadme: true,
			// No real content for either file; both should hold the placeholder.
		},
		{
			name: "owner name and role empty",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					Completer: mockCompleter{response: validJSON},
					WikiRoot:  wikiRoot,
					FilePaths: []string{mustTempFile("wuphf-seed-*.txt", "Some company content.")},
				}
			},
			wantReadme:        true,
			wantRealCompanyMD: true,
			// owner.md stays as placeholder (no name / role supplied).
		},
		{
			name: "owner name provided",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					OwnerName: "Alice",
					OwnerRole: "CEO",
					WikiRoot:  wikiRoot,
				}
			},
			wantReadme:       true,
			wantRealOwnerMD:  true,
			wantOwnerContent: []string{"Alice", "CEO"},
		},
		{
			name: "pdftotext not found",
			// extractPDF resolves pdftotext via exec.LookPath, which reads PATH
			// from the environment on every call. Scrub PATH for this case so
			// the not-installed branch fires regardless of whether the host
			// (a developer Mac with poppler installed via Homebrew, say) has
			// pdftotext available.
			setup: func(t *testing.T) {
				t.Setenv("PATH", "")
			},
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					WikiRoot:  wikiRoot,
					FilePaths: []string{mustTempFile("wuphf-seed-*.pdf", "%PDF fake")},
				}
			},
			wantReadme:          true,
			wantWarningContains: "pdftotext not installed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			wikiRoot := t.TempDir()
			input := tc.input(wikiRoot)

			result, err := SeedCompanyContext(context.Background(), input)
			if err != nil {
				t.Fatalf("SeedCompanyContext returned error: %v", err)
			}

			readmePath := filepath.Join(wikiRoot, "team", "about", "README.md")
			companyPath := filepath.Join(wikiRoot, "team", "about", "company.md")
			ownerPath := filepath.Join(wikiRoot, "team", "about", "owner.md")

			checkExists := func(path string, want bool, label string) {
				t.Helper()
				_, err := os.Stat(path)
				exists := err == nil
				if exists != want {
					t.Errorf("%s: exists=%v, want %v", label, exists, want)
				}
			}

			checkExists(readmePath, tc.wantReadme, "README.md")

			// company.md and owner.md are seeded with a placeholder on first
			// run so the README links never dangle (#946). Whenever the
			// README itself was written this run, both link targets must
			// exist too — either as placeholders or as real articles.
			if tc.wantReadme {
				checkExists(companyPath, true, "company.md")
				checkExists(ownerPath, true, "owner.md")
			}

			// When the LLM extraction succeeds the placeholder must have
			// been overwritten with real content; the inverse is asserted
			// implicitly by leaving the placeholder TODO marker in the file.
			readBody := func(path string) string {
				t.Helper()
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatalf("read %s: %v", path, readErr)
				}
				return string(data)
			}
			const todoMarker = "<!-- TODO:"
			if tc.wantReadme {
				if tc.wantRealCompanyMD && strings.Contains(readBody(companyPath), todoMarker) {
					t.Errorf("company.md still contains placeholder TODO marker but wantRealCompanyMD=true")
				}
				if !tc.wantRealCompanyMD && !strings.Contains(readBody(companyPath), todoMarker) {
					t.Errorf("company.md missing placeholder TODO marker but wantRealCompanyMD=false")
				}
				if tc.wantRealOwnerMD && strings.Contains(readBody(ownerPath), todoMarker) {
					t.Errorf("owner.md still contains placeholder TODO marker but wantRealOwnerMD=true")
				}
				if !tc.wantRealOwnerMD && !strings.Contains(readBody(ownerPath), todoMarker) {
					t.Errorf("owner.md missing placeholder TODO marker but wantRealOwnerMD=false")
				}
			}

			if tc.wantWarningContains != "" {
				found := false
				for _, w := range result.Warnings {
					if strings.Contains(w, tc.wantWarningContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning containing %q, got: %v", tc.wantWarningContains, result.Warnings)
				}
			}

			if len(tc.wantOwnerContent) > 0 && tc.wantRealOwnerMD {
				data, err := os.ReadFile(ownerPath)
				if err != nil {
					t.Fatalf("read owner.md: %v", err)
				}
				got := string(data)
				for _, want := range tc.wantOwnerContent {
					if !strings.Contains(got, want) {
						t.Errorf("owner.md missing %q, got: %s", want, got)
					}
				}
			}
		})
	}
}

// TestSeedCompanyContext_NoDeadLinksInSeededReadme is the regression guard
// for issue #946. The seeded team/about/README.md links to company.md and
// owner.md; both must resolve to files that exist after a first-run seed
// even when the user has not supplied a website, files, or completer (the
// minimal scratch path).
func TestSeedCompanyContext_NoDeadLinksInSeededReadme(t *testing.T) {
	tests := []struct {
		name  string
		input func(wikiRoot string) CompanySeedInput
	}{
		{
			name: "minimal scratch seed — no website, no owner, no completer",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{WikiRoot: wikiRoot}
			},
		},
		{
			name: "scratch seed with company name only",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					WikiRoot:    wikiRoot,
					CompanyName: "Acme",
				}
			},
		},
		{
			name: "scratch seed with owner only",
			input: func(wikiRoot string) CompanySeedInput {
				return CompanySeedInput{
					WikiRoot:  wikiRoot,
					OwnerName: "Alice",
					OwnerRole: "Founder",
				}
			},
		},
	}

	// linkRE matches markdown link targets: [label](target).
	linkRE := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			wikiRoot := t.TempDir()
			if _, err := SeedCompanyContext(context.Background(), tc.input(wikiRoot)); err != nil {
				t.Fatalf("SeedCompanyContext: %v", err)
			}

			aboutDir := filepath.Join(wikiRoot, "team", "about")
			readmePath := filepath.Join(aboutDir, "README.md")
			body, err := os.ReadFile(readmePath)
			if err != nil {
				t.Fatalf("read README.md: %v", err)
			}

			matches := linkRE.FindAllStringSubmatch(string(body), -1)
			if len(matches) == 0 {
				t.Fatal("seeded README.md has no markdown links — refusing to silently pass")
			}
			for _, m := range matches {
				target := m[1]
				// Skip absolute URLs; only validate relative paths.
				if strings.Contains(target, "://") || strings.HasPrefix(target, "/") {
					continue
				}
				// Strip any trailing anchor.
				if i := strings.Index(target, "#"); i != -1 {
					target = target[:i]
				}
				if target == "" {
					continue
				}
				resolved := filepath.Join(aboutDir, target)
				if _, statErr := os.Stat(resolved); statErr != nil {
					t.Errorf("seeded README links to %q which does not resolve: %v", target, statErr)
				}
			}
		})
	}
}

func TestSeedCompanyContext_ReadmeSkipIfExists(t *testing.T) {
	wikiRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wikiRoot, "team", "about"), 0o755); err != nil {
		t.Fatal(err)
	}
	readmePath := filepath.Join(wikiRoot, "team", "about", "README.md")
	originalContent := "# Original README\n"
	if err := os.WriteFile(readmePath, []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	input := CompanySeedInput{WikiRoot: wikiRoot}
	if _, err := SeedCompanyContext(context.Background(), input); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := SeedCompanyContext(context.Background(), input); err != nil {
		t.Fatalf("second call: %v", err)
	}

	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != originalContent {
		t.Errorf("README.md was overwritten: got %q, want %q", string(data), originalContent)
	}
}

// renderWikiIndexSections walks wikiRoot/team and returns the set of
// directory-section headers the wiki index would emit. It mirrors the
// directory-grouping contract of (*team.Repo).regenerateIndexLocked in
// internal/team/wiki_git.go: one "## <dir>" section per directory that
// contains a non-dotfile .md article. The team package cannot be imported
// here (team imports operations — that would be a cycle), so this reproduces
// the load-bearing slice of that contract: a getting-started page on disk must
// produce a "team/getting-started" index section.
func renderWikiIndexSections(t *testing.T, wikiRoot string) map[string]bool {
	t.Helper()
	teamDir := filepath.Join(wikiRoot, "team")
	sections := map[string]bool{}
	err := filepath.Walk(teamDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || !strings.HasSuffix(strings.ToLower(base), ".md") {
			return nil
		}
		rel, err := filepath.Rel(wikiRoot, path)
		if err != nil {
			return err
		}
		sections[filepath.ToSlash(filepath.Dir(rel))] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk team dir: %v", err)
	}
	return sections
}

// TestSeedGettingStarted proves both office-seed paths (scratch and blueprint)
// materialize the team/getting-started/ tree into a brand-new wiki, that the
// index page plus at least one subpage land, and that the regenerated wiki
// index would list the "team/getting-started" section. See
// docs/specs/office-onboarding-uplift.md section 5.
func TestSeedGettingStarted(t *testing.T) {
	const indexPage = "team/getting-started/index.md"
	const subPage = "team/getting-started/the-wiki.md"

	tests := []struct {
		name string
		// preSeed mirrors what each office-seed path does to the wiki BEFORE
		// SeedGettingStarted runs, so we exercise the getting-started seed in
		// the same on-disk shape both paths produce.
		preSeed func(t *testing.T, wikiRoot string)
	}{
		{
			name: "scratch path — empty wiki, getting-started seeded fresh",
			preSeed: func(_ *testing.T, _ string) {
				// Scratch path: getting-started is the first thing on disk.
			},
		},
		{
			name: "blueprint path — about/ already seeded, getting-started seeded alongside",
			preSeed: func(t *testing.T, wikiRoot string) {
				// Blueprint path materializes team/about/ first via the company
				// seed; getting-started must coexist with it, not clobber it.
				if _, err := SeedCompanyContext(context.Background(), CompanySeedInput{
					WikiRoot:    wikiRoot,
					CompanyName: "Acme",
					OwnerName:   "Alice",
					OwnerRole:   "CEO",
				}); err != nil {
					t.Fatalf("pre-seed about/: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			wikiRoot := t.TempDir()
			tc.preSeed(t, wikiRoot)

			written, err := SeedGettingStarted(wikiRoot)
			if err != nil {
				t.Fatalf("SeedGettingStarted: %v", err)
			}

			// Both the index page and at least one subpage must be reported as
			// newly written and must exist on disk.
			wantWritten := map[string]bool{indexPage: false, subPage: false}
			for _, w := range written {
				if _, ok := wantWritten[w]; ok {
					wantWritten[w] = true
				}
			}
			for page, seen := range wantWritten {
				if !seen {
					t.Errorf("SeedGettingStarted did not report writing %q (wrote: %v)", page, written)
				}
				if _, statErr := os.Stat(filepath.Join(wikiRoot, filepath.FromSlash(page))); statErr != nil {
					t.Errorf("expected %q on disk: %v", page, statErr)
				}
			}

			// index.md must carry the authored front-matter + H1 so the index
			// renders a real title rather than the bare filename.
			indexBody, readErr := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(indexPage)))
			if readErr != nil {
				t.Fatalf("read index.md: %v", readErr)
			}
			for _, want := range []string{"title: How your office works", "# How your office works"} {
				if !strings.Contains(string(indexBody), want) {
					t.Errorf("index.md missing %q", want)
				}
			}

			// The regenerated wiki index must list the Getting Started section.
			sections := renderWikiIndexSections(t, wikiRoot)
			if !sections["team/getting-started"] {
				keys := make([]string, 0, len(sections))
				for k := range sections {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				t.Errorf("wiki index sections %v missing team/getting-started", keys)
			}
		})
	}
}

// TestSeedGettingStarted_SkipIfExists proves a page authored by a prior seed is
// never clobbered: a second SeedGettingStarted call writes nothing and leaves
// the existing body intact. This is the idempotency guarantee both seed paths
// rely on (the blueprint path can re-run on a wiki that already has the pages).
func TestSeedGettingStarted_SkipIfExists(t *testing.T) {
	wikiRoot := t.TempDir()

	firstWritten, err := SeedGettingStarted(wikiRoot)
	if err != nil {
		t.Fatalf("first SeedGettingStarted: %v", err)
	}
	if len(firstWritten) == 0 {
		t.Fatal("first SeedGettingStarted wrote nothing")
	}

	// Mutate one page so we can detect an unwanted overwrite.
	indexPath := filepath.Join(wikiRoot, "team", "getting-started", "index.md")
	sentinel := "# Edited by a human\n"
	if err := os.WriteFile(indexPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("overwrite index.md: %v", err)
	}

	secondWritten, err := SeedGettingStarted(wikiRoot)
	if err != nil {
		t.Fatalf("second SeedGettingStarted: %v", err)
	}
	if len(secondWritten) != 0 {
		t.Errorf("second SeedGettingStarted overwrote existing pages: %v", secondWritten)
	}

	got, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	if string(got) != sentinel {
		t.Errorf("index.md was clobbered: got %q, want %q", string(got), sentinel)
	}
}

// TestSeedGettingStarted_EmptyWikiRoot guards the wikiRoot precondition: an
// empty wiki root is a programming error and must return an error rather than
// silently writing into the process working directory.
func TestSeedGettingStarted_EmptyWikiRoot(t *testing.T) {
	if _, err := SeedGettingStarted(""); err == nil {
		t.Fatal("SeedGettingStarted(\"\") returned nil error, want non-nil")
	}
}
