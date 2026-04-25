package providertest

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// RegisterForTest installs e for tests that need a fake provider kind.
//
// Keeping this helper outside internal/provider avoids importing testing from
// production provider code.
func RegisterForTest(t testing.TB, e *provider.Entry) {
	t.Helper()
	if e == nil || e.Kind == "" {
		t.Fatal("RegisterForTest: Entry must be non-nil with non-empty Kind")
	}
	restore := provider.RegisterTemporary(e)
	t.Cleanup(restore)
}
