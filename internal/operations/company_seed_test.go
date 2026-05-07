package operations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockCompleter struct{ response string }

func (m mockCompleter) Complete(_ context.Context, _ string) (string, error) {
	return m.response, nil
}

func TestSeedCompanyContext(t *testing.T) {
	validJSON := `{"company_name":"Acme","description":"B2B SaaS","industry":"SaaS","audience":"Ops teams","goals":"Launch Q3","key_facts":["Fact 1","Fact 2"]}`

	tests := []struct {
		name                string
		input               func(wikiRoot string) CompanySeedInput
		wantCompanyMD       bool
		wantOwnerMD         bool
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
					// Provide content via a temp text file so LLM extraction fires.
					FilePaths: func() []string {
						f, _ := os.CreateTemp("", "wuphf-seed-*.txt")
						f.WriteString("Acme is a B2B SaaS company.")
						f.Close()
						return []string{f.Name()}
					}(),
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
					FilePaths: func() []string {
						f, _ := os.CreateTemp("", "wuphf-seed-*.txt")
						f.WriteString("Acme builds software.")
						f.Close()
						return []string{f.Name()}
					}(),
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
					FilePaths: func() []string {
						f, _ := os.CreateTemp("", "wuphf-seed-*.txt")
						f.WriteString("Some company content.")
						f.Close()
						return []string{f.Name()}
					}(),
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
			wantReadme:  true,
			wantOwnerMD: true,
		},
		{
			name: "pdftotext not found",
			input: func(wikiRoot string) CompanySeedInput {
				// Create a fake .pdf file so the extension check triggers.
				f, _ := os.CreateTemp("", "wuphf-seed-*.pdf")
				f.WriteString("%PDF fake")
				f.Close()
				return CompanySeedInput{
					WikiRoot:  wikiRoot,
					FilePaths: []string{f.Name()},
				}
			},
			wantReadme:          true,
			wantWarningContains: "pdftotext not installed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
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

			// For owner name test, verify content.
			if tc.name == "owner name provided" && tc.wantOwnerMD {
				data, err := os.ReadFile(ownerPath)
				if err != nil {
					t.Fatalf("read owner.md: %v", err)
				}
				content := string(data)
				if !strings.Contains(content, "Alice") {
					t.Errorf("owner.md missing name Alice, got: %s", content)
				}
				if !strings.Contains(content, "CEO") {
					t.Errorf("owner.md missing role CEO, got: %s", content)
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
