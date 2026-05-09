package team

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
	"github.com/nex-crm/wuphf/internal/provider"
)

type brokerCompleter struct{}

func (c brokerCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	return provider.RunConfiguredOneShotCtx(ctx, "", prompt, "")
}

func (b *Broker) runCompanySeedJob(cfg config.Config) {
	wikiRoot := filepath.Join(config.RuntimeHomeDir(), ".wuphf", "wiki")
	input := operations.CompanySeedInput{
		WebsiteURL: cfg.CompanyWebsite,
		FilePaths:  cfg.CompanyFilePaths,
		OwnerName:  cfg.OwnerName,
		OwnerRole:  cfg.OwnerRole,
		Completer:  brokerCompleter{},
		WikiRoot:   wikiRoot,
	}
	ctx, cancel := context.WithTimeout(b.lifecycleCtx, 120*time.Second)
	defer cancel()
	result, err := operations.SeedCompanyContext(ctx, input)
	if err != nil {
		log.Printf("broker: company seed failed: %v", err)
		b.configMu.Lock()
		if c, loadErr := config.Load(); loadErr == nil {
			c.PendingCompanySeed = true
			if saveErr := config.Save(c); saveErr != nil {
				log.Printf("broker: company seed: failed to re-arm pending flag: %v", saveErr)
			}
		}
		b.configMu.Unlock()
		return
	}
	for _, w := range result.Warnings {
		log.Printf("broker: company seed warning: %s", w)
	}
	log.Printf("broker: company seed complete, wrote %d articles", len(result.ArticlesWritten))
	// Persist extracted profile fields back to config.
	b.configMu.Lock()
	defer b.configMu.Unlock()
	if c, err := config.Load(); err == nil {
		if result.Profile.Name != "" {
			c.CompanyName = result.Profile.Name
		}
		if result.Profile.Description != "" {
			c.CompanyDescription = result.Profile.Description
		}
		if len(result.Profile.Notes) > 0 && result.Profile.Notes[0] != "" {
			c.CompanyGoals = result.Profile.Notes[0]
		}
		// Re-arm retry flag when a transient source (URL fetch, LLM) failed
		// so the broker tries again on next startup.
		if result.NeedsRetry {
			c.PendingCompanySeed = true
		}
		if err := config.Save(c); err != nil {
			log.Printf("broker: company seed: failed to persist profile: %v", err)
		}
	}
}
