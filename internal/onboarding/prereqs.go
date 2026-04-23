package onboarding

import (
	"os/exec"
	"strings"
)

// PrereqResult describes the detection outcome for a single prerequisite binary.
type PrereqResult struct {
	// Name is the binary name (e.g. "node", "git", "claude").
	Name string `json:"name"`

	// Required is true when wuphf cannot function without this binary.
	Required bool `json:"required"`

	// Found is true when the binary was located on PATH.
	Found bool `json:"found"`

	// OK is a compatibility alias for Found used by the browser onboarding UI.
	OK bool `json:"ok"`

	// Version is the parsed version string from <name> --version, or empty.
	Version string `json:"version,omitempty"`

	// InstallURL is the canonical install page for this binary.
	InstallURL string `json:"install_url,omitempty"`
}

// prereqSpec defines static metadata for each required binary.
type prereqSpec struct {
	required   bool
	installURL string
}

var prereqSpecs = map[string]prereqSpec{
	"node":     {required: true, installURL: "https://nodejs.org"},
	"git":      {required: true, installURL: "https://git-scm.com"},
	"claude":   {required: false, installURL: "https://claude.ai/code"},
	"codex":    {required: false, installURL: "https://github.com/openai/codex"},
	"opencode": {required: false, installURL: "https://opencode.ai"},
	"cursor":   {required: false, installURL: "https://cursor.com/"},
	"windsurf": {required: false, installURL: "https://codeium.com/windsurf"},
}

// CheckAll returns a PrereqResult for each tracked binary in a stable order:
// node, git, claude, codex, opencode, cursor, windsurf. At least one of the
// CLI runtimes must be present for wuphf to actually run a turn, but all are
// marked optional here so the user can proceed with whichever runtime
// they have.
func CheckAll() []PrereqResult {
	names := []string{"node", "git", "claude", "codex", "opencode", "cursor", "windsurf"}
	results := make([]PrereqResult, 0, len(names))
	for _, name := range names {
		results = append(results, CheckOne(name))
	}
	return results
}

// CheckOne probes a single binary by name. It runs exec.LookPath to confirm
// the binary exists on PATH, then invokes `<name> --version` to capture the
// version string. If LookPath fails the binary is considered absent and the
// version field is left empty.
func CheckOne(name string) PrereqResult {
	spec := prereqSpecs[name]
	r := PrereqResult{
		Name:       name,
		Required:   spec.required,
		InstallURL: spec.installURL,
	}

	if _, err := exec.LookPath(name); err != nil {
		// Binary not on PATH.
		return r
	}
	r.Found = true
	r.OK = true

	// Best-effort version capture; ignore errors.
	out, err := exec.Command(name, "--version").Output()
	if err == nil {
		r.Version = parseVersion(string(out))
	}
	return r
}

// parseVersion trims whitespace and returns the first non-empty line from
// the version output. Many CLIs output one line; some (like git) prefix with
// the program name which we preserve verbatim.
func parseVersion(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
