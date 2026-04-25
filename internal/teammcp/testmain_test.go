package teammcp

import (
	"os"
	"testing"

	"github.com/nex-crm/wuphf/internal/team"
)

// TestMain mirrors internal/team/worktree_guard_test.go's flag flip for
// tests that live outside the team package but exercise the
// local_worktree dispatch path (handleTeamTask etc). Without this, real
// `git worktree add` calls land on the developer's wuphf repo and
// register transient branches like `wuphf-<hash>-task-1` on the invoking
// worktree — observed locally during pre-push as dozens of leaked refs
// and an interleaved HEAD ref-lock failure on `git push`.
func TestMain(m *testing.M) {
	team.DisableRealTaskWorktreeForTests()
	os.Exit(m.Run())
}
