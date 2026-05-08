package tui

import (
	"context"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
	"github.com/nex-crm/wuphf/internal/provider"
)

// cliCompleter implements operations.Completer using the configured LLM provider.
type cliCompleter struct{}

func (c cliCompleter) Complete(_ context.Context, prompt string) (string, error) {
	return provider.RunConfiguredOneShot("", prompt, "")
}

// runCompanyScan launches operations.SeedCompanyContext as a tea.Cmd.
// It clears PendingCompanySeed before running to prevent a double-seed if
// the broker starts concurrently. On success it calls cfgSave to persist
// the extracted profile, then emits companyScanDoneMsg. On failure it
// emits companyScanErrMsg.
func runCompanyScan(
	input operations.CompanySeedInput,
	cfgSave func(operations.CompanyProfile, string, string),
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		// Clear PendingCompanySeed before running to avoid a double-run if
		// the broker starts concurrently.
		if cfg, err := config.Load(); err == nil && cfg.PendingCompanySeed {
			cfg.PendingCompanySeed = false
			_ = config.Save(cfg)
		}
		result, err := operations.SeedCompanyContext(ctx, input)
		if err != nil {
			return companyScanErrMsg{err: err}
		}
		if result.NeedsRetry {
			if c, loadErr := config.Load(); loadErr == nil {
				c.PendingCompanySeed = true
				if saveErr := config.Save(c); saveErr != nil {
					log.Printf("tui: company scan: failed to re-arm pending flag: %v", saveErr)
				}
			}
		}
		cfgSave(result.Profile, input.OwnerName, input.OwnerRole)
		return companyScanDoneMsg{result: result}
	}
}

// saveCompanyProfile writes the extracted company profile fields back to config.
func saveCompanyProfile(profile operations.CompanyProfile, ownerName, ownerRole string) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if profile.Name != "" {
		cfg.CompanyName = profile.Name
	}
	if profile.Description != "" {
		cfg.CompanyDescription = profile.Description
	}
	if len(profile.Notes) > 0 && profile.Notes[0] != "" {
		cfg.CompanyGoals = profile.Notes[0]
	}
	if profile.Website != "" {
		cfg.CompanyWebsite = profile.Website
	}
	cfg.OwnerName = ownerName
	cfg.OwnerRole = ownerRole
	if err := config.Save(cfg); err != nil {
		log.Printf("tui: save company profile: %v", err)
	}
}

// splitFilePaths splits a comma-separated list of file paths and expands
// leading ~/ to the user home directory.
func splitFilePaths(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	home := config.RuntimeHomeDir()
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if home != "" && strings.HasPrefix(p, "~/") {
			p = home + p[1:]
		}
		out = append(out, p)
	}
	return out
}
