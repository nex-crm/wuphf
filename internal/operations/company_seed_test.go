package operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		name                string
		setup               func(t *testing.T) // optional per-case setup (e.g. env scrubbing)
		input               func(wikiRoot string) CompanySeedInput
		wantCompanyMD       bool
		wantOwnerMD         bool
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
			wantCompanyMD: true,
			wantOwnerMD:   true,
			wantReadme:    true,
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
			wantCompanyMD: true,
			wantReadme:    true,
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
			wantReadme:    true,
			wantCompanyMD: false,
			wantOwnerMD:   false,
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
			wantReadme:    true,
			wantCompanyMD: true,
			wantOwnerMD:   false,
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
			wantOwnerMD:      true,
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
			checkExists(companyPath, tc.wantCompanyMD, "company.md")
			checkExists(ownerPath, tc.wantOwnerMD, "owner.md")

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

			if len(tc.wantOwnerContent) > 0 && tc.wantOwnerMD {
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
