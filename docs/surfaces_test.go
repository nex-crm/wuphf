package docs

import (
	"os"
	"strings"
	"testing"
)

const parityMatrixHeader = "| Capability | Domain | Shared API | Web state | TUI state | Missing gaps | Test coverage |"

func TestSurfacesParityMatrixGate(t *testing.T) {
	body, err := os.ReadFile("surfaces.md")
	if err != nil {
		t.Fatalf("read surfaces.md: %v", err)
	}
	md := string(body)

	if strings.Contains(md, "not meant to be at parity") {
		t.Fatalf("surfaces.md regressed to asymmetry-first guidance")
	}
	if !strings.Contains(md, "Every feature PR") {
		t.Fatalf("surfaces.md must state that every feature PR updates the matrix")
	}
	if !strings.Contains(md, parityMatrixHeader) {
		t.Fatalf("surfaces.md missing parity matrix header: %s", parityMatrixHeader)
	}

	rows := parityMatrixRows(t, md)
	if len(rows) < 10 {
		t.Fatalf("expected at least 10 capability rows, got %d", len(rows))
	}
	for _, row := range rows {
		if row[2] == "" {
			t.Fatalf("capability %q is missing a shared API contract", row[0])
		}
		if row[6] == "" {
			t.Fatalf("capability %q is missing test coverage guidance", row[0])
		}
	}
}

func TestSurfaceDomainsAreDocumented(t *testing.T) {
	surfacesBody, err := os.ReadFile("surfaces.md")
	if err != nil {
		t.Fatalf("read surfaces.md: %v", err)
	}
	architectureBody, err := os.ReadFile("architecture/agent-navigability.md")
	if err != nil {
		t.Fatalf("read agent-navigability.md: %v", err)
	}

	documentedDomains := architectureDomains(t, string(architectureBody))
	for _, row := range parityMatrixRows(t, string(surfacesBody)) {
		domain := row[1]
		if !documentedDomains[domain] {
			t.Fatalf("capability %q references undocumented domain %q", row[0], domain)
		}
	}
}

func TestAgentFacingDocsUseCanonicalWebTestRunner(t *testing.T) {
	for _, path := range []string{
		"../AGENTS.md",
		"../CONTRIBUTING.md",
		"agents/INSTRUCTIONS.md",
		"dogfood/ci-triage-playbook.md",
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(body)
		if !strings.Contains(text, "bash scripts/test-web.sh") {
			t.Fatalf("%s must document `bash scripts/test-web.sh` for Web tests", path)
		}
		if strings.Contains(text, "\nbun test\n") {
			t.Fatalf("%s still lists `bun test` as a Web command", path)
		}
	}
}

func architectureDomains(t *testing.T, md string) map[string]bool {
	t.Helper()

	domains := make(map[string]bool)
	lines := strings.Split(md, "\n")
	inMatrix := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "| Domain | Owns |"):
			inMatrix = true
			continue
		case !inMatrix:
			continue
		case trimmed == "":
			next := nextNonEmptyLine(lines, i+1)
			if strings.HasPrefix(next, "|") {
				t.Fatalf("unexpected blank line inside domain matrix at line %d before %q", i+1, next)
			}
			return domains
		case strings.HasPrefix(trimmed, "|---"):
			continue
		case !strings.HasPrefix(trimmed, "|"):
			return domains
		}

		cells := markdownCells(trimmed)
		if len(cells) != 7 {
			t.Fatalf("domain matrix row has %d columns, want 7: %s", len(cells), trimmed)
		}
		domains[cells[0]] = true
	}
	return domains
}

func parityMatrixRows(t *testing.T, md string) [][]string {
	t.Helper()

	lines := strings.Split(md, "\n")
	inMatrix := false
	var rows [][]string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == parityMatrixHeader:
			inMatrix = true
			continue
		case !inMatrix:
			continue
		case trimmed == "":
			next := nextNonEmptyLine(lines, i+1)
			if strings.HasPrefix(next, "|") {
				t.Fatalf("unexpected blank line inside capability parity matrix at line %d before %q", i+1, next)
			}
			return rows
		case strings.HasPrefix(trimmed, "|---"):
			continue
		case !strings.HasPrefix(trimmed, "|"):
			return rows
		}

		cells := markdownCells(trimmed)
		if len(cells) != 7 {
			t.Fatalf("parity matrix row has %d columns, want 7: %s", len(cells), trimmed)
		}
		rows = append(rows, cells)
	}
	return rows
}

func nextNonEmptyLine(lines []string, start int) string {
	for _, line := range lines[start:] {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func markdownCells(line string) []string {
	line = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(line), "|"), "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}
