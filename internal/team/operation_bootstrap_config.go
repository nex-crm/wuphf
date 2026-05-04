package team

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/operations"
)

func buildOperationBootstrapConfig(blueprint operations.Blueprint, pack operationChannelPackDoc, profile operationCompanyProfile) operationBootstrapConfig {
	brandName := operationFirstResolvedNonEmpty(profile.Name, blueprint.BootstrapConfig.ChannelName, blueprint.Name, pack.Channel.BrandName, "Autonomous operation")
	replacements := operationBootstrapTemplateReplacements(
		brandName,
		operationSlug(brandName+" command"),
		operationFirstResolvedNonEmpty(blueprint.BootstrapConfig.Niche, blueprint.Description, profile.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
	)
	if cfg := buildOperationBootstrapConfigFromBlueprint(blueprint.BootstrapConfig, replacements); cfg != nil {
		return *cfg
	}
	brandName = operationFirstNonEmpty(profile.Name, blueprint.Name, pack.Channel.BrandName, "Autonomous operation")
	channelSlug := operationSlug(brandName)
	if channelSlug == "" {
		channelSlug = "autonomous-operation"
	}
	leadMagnetName := operationFirstNonEmpty(profile.Name, blueprint.Name, "Operation starter")
	leadMagnetPath := operationSlug(leadMagnetName)
	if leadMagnetPath == "" {
		leadMagnetPath = "starter"
	}
	return operationBootstrapConfig{
		ChannelName:       brandName,
		ChannelSlug:       channelSlug,
		Niche:             operationFirstNonEmpty(blueprint.Description, profile.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
		Audience:          operationFirstNonEmpty(strings.TrimSpace(profile.Size), strings.Join(pack.Audience.PrimaryICP, ", "), "Operators and stakeholders"),
		Positioning:       operationFirstNonEmpty(profile.Description, blueprint.Objective, blueprint.Description, pack.Channel.ShortBio, pack.Channel.Tagline, "Blueprint-driven operating system"),
		MonetizationHooks: []string{"Approval-gated value capture"},
		PublishingCadence: operationFirstNonEmpty(operationPublishingCadence(pack), "Weekly operating review"),
		LeadMagnet: operationLeadMagnet{
			Name: leadMagnetName,
			CTA:  "Open the starter package",
			Path: leadMagnetPath,
		},
		KPITracking: []operationKPI{
			{
				Name:   "Workflow completions",
				Target: "3+ completed loops per week",
				Why:    "Confirms the operation is running repeatably, not just generating plans.",
			},
			{
				Name:   "Approval turnaround",
				Target: "<24h on blocked steps",
				Why:    "Keeps human checkpoints from stalling the system.",
			},
			{
				Name:   "Outcome conversion",
				Target: "1 measurable business outcome per cycle",
				Why:    "Proves the workflows are tied to value, not just activity.",
			},
		},
	}
}

func buildOperationBootstrapConfigFromBlueprint(cfg operations.BootstrapConfig, replacements map[string]string) *operationBootstrapConfig {
	if operationBootstrapConfigIsEmpty(cfg) {
		return nil
	}
	contentPillars := make([]string, 0, len(cfg.ContentPillars))
	for _, item := range cfg.ContentPillars {
		contentPillars = append(contentPillars, operationRenderTemplateString(item, replacements))
	}
	contentSeries := make([]string, 0, len(cfg.ContentSeries))
	for _, item := range cfg.ContentSeries {
		contentSeries = append(contentSeries, operationRenderTemplateString(item, replacements))
	}
	monetizationHooks := make([]string, 0, len(cfg.MonetizationHooks))
	for _, item := range cfg.MonetizationHooks {
		monetizationHooks = append(monetizationHooks, operationRenderTemplateString(item, replacements))
	}
	assets := make([]operationMonetizationAsset, 0, len(cfg.MonetizationAsset))
	for _, asset := range cfg.MonetizationAsset {
		assets = append(assets, operationMonetizationAsset{
			Stage: operationRenderTemplateString(asset.Stage, replacements),
			Name:  operationRenderTemplateString(asset.Name, replacements),
			Slot:  operationRenderTemplateString(asset.Slot, replacements),
			CTA:   operationRenderTemplateString(asset.CTA, replacements),
		})
	}
	kpis := make([]operationKPI, 0, len(cfg.KPITracking))
	for _, kpi := range cfg.KPITracking {
		kpis = append(kpis, operationKPI{
			Name:   operationRenderTemplateString(kpi.Name, replacements),
			Target: operationRenderTemplateString(kpi.Target, replacements),
			Why:    operationRenderTemplateString(kpi.Why, replacements),
		})
	}
	return &operationBootstrapConfig{
		ChannelName:       operationRenderTemplateString(cfg.ChannelName, replacements),
		ChannelSlug:       operationRenderTemplateString(cfg.ChannelSlug, replacements),
		Niche:             operationRenderTemplateString(cfg.Niche, replacements),
		Audience:          operationRenderTemplateString(cfg.Audience, replacements),
		Positioning:       operationRenderTemplateString(cfg.Positioning, replacements),
		ContentPillars:    contentPillars,
		ContentSeries:     contentSeries,
		MonetizationHooks: monetizationHooks,
		PublishingCadence: operationRenderTemplateString(cfg.PublishingCadence, replacements),
		LeadMagnet: operationLeadMagnet{
			Name: operationRenderTemplateString(cfg.LeadMagnet.Name, replacements),
			CTA:  operationRenderTemplateString(cfg.LeadMagnet.CTA, replacements),
			Path: operationRenderTemplateString(cfg.LeadMagnet.Path, replacements),
		},
		MonetizationAsset: assets,
		KPITracking:       kpis,
	}
}

func operationBootstrapConfigIsEmpty(cfg operations.BootstrapConfig) bool {
	return strings.TrimSpace(cfg.ChannelName) == "" &&
		strings.TrimSpace(cfg.ChannelSlug) == "" &&
		strings.TrimSpace(cfg.Niche) == "" &&
		len(cfg.ContentPillars) == 0 &&
		len(cfg.ContentSeries) == 0 &&
		len(cfg.MonetizationHooks) == 0 &&
		strings.TrimSpace(cfg.PublishingCadence) == "" &&
		strings.TrimSpace(cfg.LeadMagnet.Name) == "" &&
		len(cfg.MonetizationAsset) == 0 &&
		len(cfg.KPITracking) == 0
}

func buildOperationBootstrapConfigFromPack(pack operationChannelPackDoc) operationBootstrapConfig {
	contentSeries := make([]string, 0, len(pack.Channel.Playlists))
	for i, playlist := range pack.Channel.Playlists {
		if i >= 4 {
			break
		}
		contentSeries = append(contentSeries, strings.TrimSpace(playlist.Title))
	}
	return operationBootstrapConfig{
		ChannelName:       pack.Channel.BrandName,
		ChannelSlug:       operationSlug(pack.Channel.BrandName),
		Niche:             pack.Channel.Thesis,
		Audience:          strings.Join(pack.Audience.PrimaryICP, ", "),
		Positioning:       operationFirstNonEmpty(pack.Channel.ShortBio, pack.Channel.Tagline, pack.Channel.Thesis),
		ContentPillars:    append([]string(nil), pack.Audience.JobsToBeDone...),
		ContentSeries:     contentSeries,
		MonetizationHooks: append([]string(nil), pack.OfferDefaults.RevenueLadder...),
		PublishingCadence: operationPublishingCadence(pack),
		LeadMagnet: operationLeadMagnet{
			Name: pack.OfferDefaults.PrimaryLeadMagnet.Name,
			CTA:  "Get the " + strings.TrimSpace(pack.OfferDefaults.PrimaryLeadMagnet.Name),
			Path: pack.OfferDefaults.PrimaryLeadMagnet.LandingPage.Slug,
		},
		MonetizationAsset: buildOperationMonetizationAssets(pack),
		KPITracking:       buildOperationKPIs(pack),
	}
}

func buildOperationMonetizationAssets(pack operationChannelPackDoc) []operationMonetizationAsset {
	out := []operationMonetizationAsset{
		{
			Stage: "Day 0",
			Name:  pack.OfferDefaults.PrimaryLeadMagnet.Name,
			Slot:  "pinned_comment",
			CTA:   "Get the " + strings.TrimSpace(pack.OfferDefaults.PrimaryLeadMagnet.Name),
		},
	}
	for _, asset := range pack.OfferDefaults.SupportingAssets {
		out = append(out, operationMonetizationAsset{
			Stage: "Support",
			Name:  asset.CanonicalAsset,
			Slot:  "description_links",
			CTA:   "Open " + asset.CanonicalAsset,
		})
	}
	return out
}

func buildOperationKPIs(pack operationChannelPackDoc) []operationKPI {
	brand := operationFirstNonEmpty(pack.Channel.BrandName, "channel")
	return []operationKPI{
		{
			Name:   "Primary CTA conversions",
			Target: "25+ monthly",
			Why:    fmt.Sprintf("Proves %s is capturing owned demand instead of only views.", brand),
		},
		{
			Name:   "Workflow click-through",
			Target: "3%+",
			Why:    "Shows the monetization lane matches the workflow problem.",
		},
		{
			Name:   "First paid offer conversion",
			Target: "Within 30 days",
			Why:    "Confirms the content engine reaches buyers, not only viewers.",
		},
		{
			Name:   "Repeatable winners",
			Target: "2 episodes above baseline CTR + retention",
			Why:    "Marks the point where sponsor and scale experiments become sensible.",
		},
	}
}

func operationPublishingCadence(pack operationChannelPackDoc) string {
	days := pack.LaunchDefaults.Cadence.PublishDays
	if len(days) == 0 {
		return ""
	}
	return fmt.Sprintf("%d release days/week (%s)", len(days), strings.Join(days, ", "))
}
