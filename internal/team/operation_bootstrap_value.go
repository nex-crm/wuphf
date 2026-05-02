package team

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/operations"
)

func buildOperationValueCapturePlan(blueprint operations.Blueprint, pack operationChannelPackDoc) []operationMonetizationStep {
	if len(blueprint.MonetizationLadder) > 0 {
		replacements := operationBootstrapTemplateReplacements(
			operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation"),
			operationSlug(operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation")+" command"),
			operationFirstNonEmpty(blueprint.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
		)
		out := make([]operationMonetizationStep, 0, len(blueprint.MonetizationLadder))
		for _, step := range blueprint.MonetizationLadder {
			out = append(out, operationMonetizationStep{
				Kicker: operationRenderTemplateString(step.Kicker, replacements),
				Title:  operationRenderTemplateString(step.Title, replacements),
				Copy:   operationRenderTemplateString(step.Copy, replacements),
				Footer: operationRenderTemplateString(step.Footer, replacements),
			})
		}
		return out
	}
	ladder := pack.OfferDefaults.RevenueLadder
	if len(ladder) == 0 {
		return nil
	}
	out := make([]operationMonetizationStep, 0, len(ladder))
	for i, item := range ladder {
		out = append(out, operationMonetizationStep{
			Kicker: fmt.Sprintf("Step %d", i+1),
			Title:  strings.ReplaceAll(item, "_", " "),
			Copy:   fmt.Sprintf("Turn %s into a reusable commercial lane in the operating system.", strings.ReplaceAll(item, "_", " ")),
			Footer: "Loaded from the legacy pack revenue ladder.",
		})
	}
	return out
}

func buildOperationWorkstreamSeed(blueprint operations.Blueprint, pack operationChannelPackDoc, backlog operationBacklogDoc) []operationQueueItem {
	if len(blueprint.QueueSeed) > 0 {
		replacements := operationBootstrapTemplateReplacements(
			operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation"),
			operationSlug(operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation")+" command"),
			operationFirstNonEmpty(blueprint.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
		)
		out := make([]operationQueueItem, 0, len(blueprint.QueueSeed))
		for _, item := range blueprint.QueueSeed {
			out = append(out, operationQueueItem{
				ID:           operationRenderTemplateString(item.ID, replacements),
				Title:        operationRenderTemplateString(item.Title, replacements),
				Format:       operationRenderTemplateString(item.Format, replacements),
				StageIndex:   item.StageIndex,
				Score:        item.Score,
				UnitCost:     item.UnitCost,
				Eta:          operationRenderTemplateString(item.Eta, replacements),
				Monetization: operationRenderTemplateString(item.Monetization, replacements),
				State:        operationRenderTemplateString(item.State, replacements),
			})
		}
		return out
	}
	format := "Work item"
	if strings.Contains(strings.ToLower(pack.Channel.RenderDefaults.Format), "short") {
		format = "Short"
	} else if strings.TrimSpace(pack.Channel.RenderDefaults.Format) != "" {
		format = strings.TrimSpace(pack.Channel.RenderDefaults.Format)
	}
	items := make([]operationQueueItem, 0, 5)
	for i, episode := range backlog.Episodes {
		if i >= 5 {
			break
		}
		items = append(items, operationQueueItem{
			ID:           operationFirstNonEmpty(strings.TrimSpace(episode.ID), fmt.Sprintf("run-%d", i+1)),
			Title:        operationFirstNonEmpty(strings.TrimSpace(episode.WorkingTitle), fmt.Sprintf("Launch slot %d", i+1)),
			Format:       format,
			StageIndex:   i % 5,
			Score:        operationEpisodeScore(episode),
			UnitCost:     operationQueueUnitCost(format),
			Eta:          fmt.Sprintf("Launch slot %d", i+1),
			Monetization: operationQueueMonetization(episode),
			State:        "active",
		})
	}
	return items
}

func operationEpisodeScore(episode operationBacklogEpisode) int {
	total := episode.Scores.Pain + episode.Scores.BuyerIntent + episode.Scores.Originality + episode.Scores.ProductFit
	if total <= 0 {
		return 75
	}
	return total * 5
}

func operationQueueUnitCost(format string) int {
	if strings.EqualFold(format, "Short") {
		return 4
	}
	return 18
}

func operationQueueMonetization(episode operationBacklogEpisode) string {
	if strings.TrimSpace(episode.PrimaryCTA) == "" {
		return "owned audience"
	}
	parts := []string{strings.ReplaceAll(episode.PrimaryCTA, "_", " ")}
	if strings.TrimSpace(episode.AffiliateCategory) != "" {
		parts = append(parts, strings.ReplaceAll(episode.AffiliateCategory, "_", " "))
	}
	return strings.Join(parts, " + ")
}

func buildOperationOffers(blueprint operations.Blueprint, pack operationChannelPackDoc, monetization operationMonetizationDoc, profile operationCompanyProfile) []operationOffer {
	leadMagnetName := operationFirstNonEmpty(blueprint.BootstrapConfig.LeadMagnet.Name, profile.Name, blueprint.Name, pack.OfferDefaults.PrimaryLeadMagnet.Name, "Operation blueprint")
	leadMagnetID := operationFirstNonEmpty(operationSlug(leadMagnetName), pack.OfferDefaults.PrimaryLeadMagnet.OfferID, operationSlug(operationFirstNonEmpty(profile.Name, blueprint.Name, "operation")))
	destination := operationFirstNonEmpty(blueprint.BootstrapConfig.LeadMagnet.Path, pack.OfferDefaults.PrimaryLeadMagnet.LandingPage.Slug, "bootstrap")
	out := []operationOffer{
		{
			ID:          leadMagnetID,
			Name:        leadMagnetName,
			Type:        "lead_magnet",
			CTA:         operationFirstNonEmpty(blueprint.BootstrapConfig.LeadMagnet.CTA, pack.OfferDefaults.PrimaryLeadMagnet.Promise, "Open the starter package."),
			Destination: destination,
		},
	}
	for _, asset := range blueprint.BootstrapConfig.MonetizationAsset {
		name := strings.TrimSpace(asset.Name)
		if name == "" {
			continue
		}
		out = append(out, operationOffer{
			ID:          operationFirstNonEmpty(operationSlug(name), operationSlug(asset.Stage), operationSlug(asset.Slot)),
			Name:        name,
			Type:        "asset",
			CTA:         operationFirstNonEmpty(asset.CTA, "Open "+name),
			Destination: operationFirstNonEmpty(asset.Slot, asset.Stage),
		})
	}
	for _, product := range monetization.Offers.DigitalProducts {
		name := strings.TrimSpace(product.Name)
		if name == "" {
			continue
		}
		out = append(out, operationOffer{
			ID:   product.ID,
			Name: name,
			Type: "digital_product",
			CTA:  "See " + name,
		})
	}
	for _, service := range monetization.Offers.Services {
		name := strings.TrimSpace(service.Name)
		if name == "" {
			continue
		}
		out = append(out, operationOffer{
			ID:   service.ID,
			Name: name,
			Type: "service",
			CTA:  "Request " + name,
		})
	}
	return out
}
