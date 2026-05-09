package onboarding

import (
	"context"

	"github.com/nex-crm/wuphf/internal/provider"
)

type cliCompleter struct{}

func (c cliCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	return provider.RunConfiguredOneShotCtx(ctx, "", prompt, "")
}
