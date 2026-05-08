package onboarding

import (
	"context"

	"github.com/nex-crm/wuphf/internal/provider"
)

type cliCompleter struct{}

func (c cliCompleter) Complete(_ context.Context, prompt string) (string, error) {
	return provider.RunConfiguredOneShot("", prompt, "")
}
