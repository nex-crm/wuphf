package onboarding

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/runtimebin"
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
//
// Probes run concurrently. CheckOne's per-probe timeout is 10s (see comment
// there for rationale) and CheckAll is invoked from an HTTP handler with a
// 5s client deadline at cmd/wuphf/onboarding.go; running probes serially
// would mean worst-case wall-clock = 7 × 10s = 70s, far past any sane HTTP
// budget. Concurrent probes cap wall-clock at max(probe), well under the
// client timeout. Order of `names` is preserved in the returned slice.
func CheckAll() []PrereqResult {
	names := []string{"node", "git", "claude", "codex", "opencode", "cursor", "windsurf"}
	results := make([]PrereqResult, len(names))
	var wg sync.WaitGroup
	wg.Add(len(names))
	for i, name := range names {
		go func(i int, name string) {
			defer wg.Done()
			results[i] = CheckOne(name)
		}(i, name)
	}
	wg.Wait()
	return results
}

// CheckOne probes a single binary by name. It resolves from PATH plus common
// CLI install directories, then invokes `<name> --version` to capture the
// version string. If resolution fails the binary is considered absent and the
// version field is left empty.
func CheckOne(name string) PrereqResult {
	spec := prereqSpecs[name]
	r := PrereqResult{
		Name:       name,
		Required:   spec.required,
		InstallURL: spec.installURL,
	}

	path, err := runtimebin.LookPath(name)
	if err != nil {
		return r
	}
	r.Found = true
	r.OK = true

	// Best-effort version capture; ignore errors.
	//
	// 10s (up from 3s) to keep the probe reliable when the machine is under
	// parallel test load — `go test ./...` can stack 20+ concurrent fork+exec
	// calls, and a 3s window was flaky on a developer laptop running the
	// pre-push hook. This is a one-shot `--version` probe, not a hot path;
	// the timeout is a floor on machine health, not on binary response time.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
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
