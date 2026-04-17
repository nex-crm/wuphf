package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type channelBrief struct {
	Metadata  metadataBlock  `json:"metadata"`
	Channel   channelBlock   `json:"channel"`
	Render    renderBlock    `json:"render"`
	Episode   episodeBlock   `json:"episode"`
	Packaging packagingBlock `json:"packaging"`
	CTA       ctaBlock       `json:"cta"`
	Publish   publishBlock   `json:"publish"`
	QA        qaBlock        `json:"qa"`
	Approval  approvalBlock  `json:"approval"`
}

type metadataBlock struct {
	ID        string `json:"id"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
	Source    string `json:"source"`
}

type channelBlock struct {
	BrandName          string   `json:"brand_name"`
	Thesis             string   `json:"thesis"`
	Tagline            string   `json:"tagline"`
	NarrationDirection string   `json:"narration_direction"`
	WritingStyle       []string `json:"writing_style"`
}

type renderBlock struct {
	TargetDurationMinutes string   `json:"target_duration_minutes"`
	SceneOrder            []string `json:"scene_order"`
	MusicDirection        string   `json:"music_direction"`
	VisualMotifs          []string `json:"visual_motifs"`
}

type episodeBlock struct {
	EpisodeID    string          `json:"episode_id"`
	WorkingSlug  string          `json:"working_slug"`
	Pillar       string          `json:"pillar"`
	Audience     string          `json:"audience"`
	Workflow     string          `json:"workflow"`
	SearchIntent string          `json:"search_intent"`
	Promise      string          `json:"promise"`
	ProofAsset   episodeAssetRef `json:"proof_asset"`
}

type episodeAssetRef struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	OfferID string `json:"offer_id"`
}

type packagingBlock struct {
	FinalTitle   string         `json:"final_title"`
	BackupTitles []string       `json:"backup_titles"`
	TitleFamily  string         `json:"title_family"`
	HookPromise  string         `json:"hook_promise"`
	Thumbnail    thumbnailBlock `json:"thumbnail"`
}

type thumbnailBlock struct {
	Family      string   `json:"family"`
	Text        string   `json:"text"`
	FocalObject string   `json:"focal_object"`
	VisualNotes []string `json:"visual_notes"`
	Avoid       []string `json:"avoid"`
}

type ctaBlock struct {
	PrimaryOfferID     string `json:"primary_offer_id"`
	PrimaryOfferName   string `json:"primary_offer_name"`
	SecondaryOfferID   string `json:"secondary_offer_id"`
	SecondaryOfferName string `json:"secondary_offer_name"`
	OnScreenLine       string `json:"on_screen_line"`
}

type publishBlock struct {
	PlaylistID string        `json:"playlist_id"`
	Tags       []string      `json:"tags"`
	Chapters   []chapterBeat `json:"chapters"`
}

type chapterBeat struct {
	Time  string `json:"time"`
	Label string `json:"label"`
}

type qaBlock struct {
	MustPass []string `json:"must_pass"`
	BlockIf  []string `json:"block_if"`
}

type approvalBlock struct {
	Mode           string          `json:"mode"`
	Status         string          `json:"status"`
	ClientName     string          `json:"client_name"`
	LivePacketPath string          `json:"live_packet_path"`
	Approvers      []approverBlock `json:"approvers"`
}

type approverBlock struct {
	Role   string `json:"role"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type scriptPacket struct {
	Metadata            packetMetadata      `json:"metadata"`
	SourceBrief         packetSourceBrief   `json:"source_brief"`
	Approval            approvalBlock       `json:"approval"`
	Narration           narrationBlock      `json:"narration"`
	Story               storyBlock          `json:"story"`
	PackagingGuardrails packagingGuardrails `json:"packaging_guardrails"`
	ProductionNotes     productionNotes     `json:"production_notes"`
	QAGates             qaBlock             `json:"qa_gates"`
}

type packetMetadata struct {
	ID            string `json:"id"`
	Version       int    `json:"version"`
	GeneratedFrom string `json:"generated_from"`
	Generator     string `json:"generator"`
}

type packetSourceBrief struct {
	BriefID     string `json:"brief_id"`
	EpisodeID   string `json:"episode_id"`
	WorkingSlug string `json:"working_slug"`
	Title       string `json:"title"`
	Workflow    string `json:"workflow"`
	Promise     string `json:"promise"`
}

type narrationBlock struct {
	VoiceoverDirection string   `json:"voiceover_direction"`
	WritingStyle       []string `json:"writing_style"`
	TargetDuration     string   `json:"target_duration_minutes"`
}

type storyBlock struct {
	PrimaryHook  string          `json:"primary_hook"`
	ViewerPayoff string          `json:"viewer_payoff"`
	PrimaryCTA   string          `json:"primary_cta"`
	SecondaryCTA string          `json:"secondary_cta"`
	Sections     []scriptSection `json:"sections"`
}

type scriptSection struct {
	ID            string   `json:"id"`
	Purpose       string   `json:"purpose"`
	ChapterLabel  string   `json:"chapter_label,omitempty"`
	VoiceoverBeat string   `json:"voiceover_beat"`
	OnScreenBeats []string `json:"on_screen_beats"`
	ProofPoints   []string `json:"proof_points"`
}

type packagingGuardrails struct {
	Title          string   `json:"title"`
	BackupTitles   []string `json:"backup_titles"`
	ThumbnailText  string   `json:"thumbnail_text"`
	ThumbnailFocus string   `json:"thumbnail_focus"`
	ThumbnailNotes []string `json:"thumbnail_notes"`
	ThumbnailAvoid []string `json:"thumbnail_avoid"`
}

type productionNotes struct {
	MusicDirection string   `json:"music_direction"`
	VisualMotifs   []string `json:"visual_motifs"`
	Tags           []string `json:"tags"`
}

type reviewBundleSummary struct {
	Title           string
	Workflow        string
	Promise         string
	TargetDuration  string
	PrimaryCTA      string
	ThumbnailText   string
	ApprovalMode    string
	ApprovalStatus  string
	ClientName      string
	Approvers       []string
	LivePacketPath  string
	ApprovalFocus   []string
	BlockingRisks   []string
	SectionLabels   []string
	GeneratedFiles  []string
	SourceBriefPath string
	PacketGenerator string
}

type slackPayload struct {
	Channel   string   `json:"channel"`
	Text      string   `json:"text"`
	Checklist []string `json:"checklist"`
}

type googleDrivePayload struct {
	FolderName   string   `json:"folder_name"`
	DocumentName string   `json:"document_name"`
	Viewers      []string `json:"viewers"`
	Tags         []string `json:"tags"`
	Notes        []string `json:"notes"`
}

type notionPayload struct {
	Database   string            `json:"database"`
	Title      string            `json:"title"`
	Status     string            `json:"status"`
	Properties map[string]string `json:"properties"`
	Checklist  []string          `json:"checklist"`
}

func main() {
	inPath := flag.String("in", "", "Path to normalized channel brief JSON")
	outPath := flag.String("out", "", "Path to write script packet JSON")
	bundleDir := flag.String("bundle-dir", "", "Path to write review bundle files")
	flag.Parse()

	if strings.TrimSpace(*inPath) == "" || strings.TrimSpace(*outPath) == "" {
		fmt.Fprintln(os.Stderr, "usage: youtube-script-packet --in <brief.json> --out <script-packet.json> [--bundle-dir <review-bundle-dir>]")
		os.Exit(2)
	}

	brief, err := loadBrief(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load brief: %v\n", err)
		os.Exit(1)
	}
	packet, err := buildScriptPacket(brief)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build script packet: %v\n", err)
		os.Exit(1)
	}
	if err := writePacket(*outPath, packet); err != nil {
		fmt.Fprintf(os.Stderr, "write packet: %v\n", err)
		os.Exit(1)
	}
	if err := writeReviewBundle(resolveBundleDir(*bundleDir, *outPath), brief, packet); err != nil {
		fmt.Fprintf(os.Stderr, "write review bundle: %v\n", err)
		os.Exit(1)
	}
}

func loadBrief(path string) (channelBrief, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return channelBrief{}, err
	}
	var brief channelBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return channelBrief{}, err
	}
	if err := validateBrief(brief); err != nil {
		return channelBrief{}, err
	}
	return brief, nil
}

func validateBrief(brief channelBrief) error {
	switch {
	case strings.TrimSpace(brief.Metadata.ID) == "":
		return fmt.Errorf("metadata.id required")
	case strings.TrimSpace(brief.Channel.BrandName) == "":
		return fmt.Errorf("channel.brand_name required")
	case strings.TrimSpace(brief.Channel.NarrationDirection) == "":
		return fmt.Errorf("channel.narration_direction required")
	case strings.TrimSpace(brief.Episode.EpisodeID) == "":
		return fmt.Errorf("episode.episode_id required")
	case strings.TrimSpace(brief.Episode.WorkingSlug) == "":
		return fmt.Errorf("episode.working_slug required")
	case strings.TrimSpace(brief.Episode.Promise) == "":
		return fmt.Errorf("episode.promise required")
	case strings.TrimSpace(brief.Packaging.FinalTitle) == "":
		return fmt.Errorf("packaging.final_title required")
	case strings.TrimSpace(brief.Packaging.HookPromise) == "":
		return fmt.Errorf("packaging.hook_promise required")
	case len(brief.Render.SceneOrder) == 0:
		return fmt.Errorf("render.scene_order required")
	case len(brief.Publish.Chapters) == 0:
		return fmt.Errorf("publish.chapters required")
	case strings.TrimSpace(brief.CTA.PrimaryOfferName) == "":
		return fmt.Errorf("cta.primary_offer_name required")
	}
	return nil
}

func buildScriptPacket(brief channelBrief) (scriptPacket, error) {
	sections := make([]scriptSection, 0, len(brief.Render.SceneOrder))
	for idx, sceneID := range brief.Render.SceneOrder {
		sections = append(sections, buildSection(sceneID, brief, chapterForIndex(brief.Publish.Chapters, idx)))
	}

	packet := scriptPacket{
		Metadata: packetMetadata{
			ID:            fmt.Sprintf("%s_script_packet", brief.Episode.WorkingSlug),
			Version:       1,
			GeneratedFrom: brief.Metadata.UpdatedAt,
			Generator:     "cmd/youtube-script-packet",
		},
		SourceBrief: packetSourceBrief{
			BriefID:     brief.Metadata.ID,
			EpisodeID:   brief.Episode.EpisodeID,
			WorkingSlug: brief.Episode.WorkingSlug,
			Title:       brief.Packaging.FinalTitle,
			Workflow:    brief.Episode.Workflow,
			Promise:     brief.Episode.Promise,
		},
		Approval: brief.Approval,
		Narration: narrationBlock{
			VoiceoverDirection: brief.Channel.NarrationDirection,
			WritingStyle:       brief.Channel.WritingStyle,
			TargetDuration:     brief.Render.TargetDurationMinutes,
		},
		Story: storyBlock{
			PrimaryHook:  brief.Packaging.HookPromise,
			ViewerPayoff: brief.Episode.Promise,
			PrimaryCTA:   brief.CTA.OnScreenLine,
			SecondaryCTA: brief.Episode.ProofAsset.Name,
			Sections:     sections,
		},
		PackagingGuardrails: packagingGuardrails{
			Title:          brief.Packaging.FinalTitle,
			BackupTitles:   brief.Packaging.BackupTitles,
			ThumbnailText:  brief.Packaging.Thumbnail.Text,
			ThumbnailFocus: brief.Packaging.Thumbnail.FocalObject,
			ThumbnailNotes: brief.Packaging.Thumbnail.VisualNotes,
			ThumbnailAvoid: brief.Packaging.Thumbnail.Avoid,
		},
		ProductionNotes: productionNotes{
			MusicDirection: brief.Render.MusicDirection,
			VisualMotifs:   brief.Render.VisualMotifs,
			Tags:           brief.Publish.Tags,
		},
		QAGates: brief.QA,
	}
	return packet, nil
}

func chapterForIndex(chapters []chapterBeat, idx int) chapterBeat {
	if idx >= 0 && idx < len(chapters) {
		return chapters[idx]
	}
	if len(chapters) == 0 {
		return chapterBeat{}
	}
	return chapters[len(chapters)-1]
}

func buildSection(sceneID string, brief channelBrief, chapter chapterBeat) scriptSection {
	base := scriptSection{
		ID:           sceneID,
		ChapterLabel: chapter.Label,
	}
	switch sceneID {
	case "cold_open":
		base.Purpose = "Open with the operating pain and show the payoff fast."
		base.VoiceoverBeat = fmt.Sprintf("%s %s", brief.Packaging.HookPromise, "Name the workflow fast, call out the cost of leaving it manual, and promise one concrete system instead of a tool roundup.")
		base.OnScreenBeats = []string{
			fmt.Sprintf("Open on %s", brief.Packaging.Thumbnail.FocalObject),
			fmt.Sprintf("Flash the workflow label: %s", brief.Episode.Workflow),
			fmt.Sprintf("Lock the promise: %s", brief.Episode.Promise),
		}
		base.ProofPoints = []string{
			brief.Packaging.FinalTitle,
			brief.Packaging.Thumbnail.Text,
		}
	case "problem_framing":
		base.Purpose = "Make the viewer feel the cost of the current manual process."
		base.VoiceoverBeat = fmt.Sprintf("Frame %s as hidden operating drag for %s. Make the cost concrete in missed follow-up, delayed handoffs, or founder rework before you name any tools, then tie the mess back to %s.", brief.Episode.Workflow, brief.Episode.Audience, brief.Episode.SearchIntent)
		base.OnScreenBeats = []string{
			"Show the before state and the work that slips through",
			fmt.Sprintf("Anchor the audience: %s", brief.Episode.Audience),
		}
		base.ProofPoints = []string{
			brief.Channel.Thesis,
			brief.Channel.Tagline,
		}
	case "system_map":
		base.Purpose = "Show the full system before walking step by step."
		base.VoiceoverBeat = fmt.Sprintf("Lay out the %s system map in one clean pass: intake, routing, action, and the human checkpoint. Clarify what gets automated, what still needs approval, and position the proof asset as the rulebook behind the workflow.", brief.Episode.Workflow)
		base.OnScreenBeats = []string{
			"Draw the workflow as a simple before/after map",
			"Label the queue stages and handoff points",
			"Keep the human approval point visible",
		}
		base.ProofPoints = []string{
			brief.Episode.ProofAsset.Name,
			firstNonEmpty(brief.QA.MustPass...),
		}
	case "step_walkthrough":
		base.Purpose = "Walk the viewer through the workflow in a way they can copy."
		base.VoiceoverBeat = "Break the system into concrete steps. For each step, show the input, the decision, the output, and the human check that keeps the workflow trustworthy."
		base.OnScreenBeats = []string{
			"One step per screen with minimal labels",
			"Show inputs, routing logic, and output queue",
			"Call out the no-autopilot boundary",
		}
		base.ProofPoints = []string{
			"Trigger -> route -> review -> close loop",
			nonEmptyOrFallback(firstMatching(brief.QA.MustPass, "Human review"), "Human review stays explicit where customer risk appears."),
		}
	case "proof_asset_reveal":
		base.Purpose = "Reveal the asset that makes the episode actionable."
		base.VoiceoverBeat = fmt.Sprintf("Introduce %s as the operating rule set behind the workflow. Show the decisions it makes easier and why it lets a small team deploy the system this week without guessing.", brief.Episode.ProofAsset.Name)
		base.OnScreenBeats = []string{
			fmt.Sprintf("Show the asset: %s", brief.Episode.ProofAsset.Name),
			fmt.Sprintf("Connect it to the offer: %s", brief.CTA.PrimaryOfferName),
		}
		base.ProofPoints = []string{
			brief.Episode.ProofAsset.Type,
			brief.CTA.PrimaryOfferName,
		}
	case "cta_endcard":
		base.Purpose = "Close with one primary action and one narrow next step."
		base.VoiceoverBeat = fmt.Sprintf("%s Keep the ask narrow, tie it to diagnosing the viewer's next bottleneck, and ask for the next messy workflow they want rebuilt so the CTA feels like the next operating move, not a hard sell.", brief.CTA.OnScreenLine)
		base.OnScreenBeats = []string{
			fmt.Sprintf("Primary CTA only: %s", brief.CTA.PrimaryOfferName),
			fmt.Sprintf("Secondary reference: %s", brief.CTA.SecondaryOfferName),
		}
		base.ProofPoints = []string{
			firstNonEmpty(brief.QA.MustPass...),
			nonEmptyOrFallback(firstMatching(brief.QA.BlockIf, "generic AI tools roundup"), "Keep the ending tied to the workflow, not a generic tools list."),
		}
	default:
		base.Purpose = "Carry the next story beat without drifting off the workflow."
		base.VoiceoverBeat = fmt.Sprintf("Advance the story while keeping the episode locked on %s and the promised outcome.", brief.Episode.Workflow)
		base.OnScreenBeats = []string{
			"Carry forward the workflow state",
		}
		base.ProofPoints = []string{
			brief.Episode.Promise,
		}
	}
	return base
}

func firstMatching(values []string, needle string) string {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), strings.ToLower(needle)) {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonEmptyOrFallback(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func writePacket(path string, packet scriptPacket) error {
	return writeJSON(path, packet)
}

func resolveBundleDir(bundleDir, outPath string) string {
	if strings.TrimSpace(bundleDir) != "" {
		return bundleDir
	}
	base := strings.TrimSuffix(filepath.Base(outPath), filepath.Ext(outPath))
	return filepath.Join(filepath.Dir(outPath), base+"-review-bundle")
}

func writeReviewBundle(dir string, brief channelBrief, packet scriptPacket) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	files := []string{
		"summary.md",
		"slack-payload.json",
		"google-drive-payload.json",
		"notion-payload.json",
	}
	sort.Strings(files)

	summary := reviewBundleSummary{
		Title:           packet.PackagingGuardrails.Title,
		Workflow:        packet.SourceBrief.Workflow,
		Promise:         packet.SourceBrief.Promise,
		TargetDuration:  packet.Narration.TargetDuration,
		PrimaryCTA:      packet.Story.PrimaryCTA,
		ThumbnailText:   packet.PackagingGuardrails.ThumbnailText,
		ApprovalMode:    packet.Approval.Mode,
		ApprovalStatus:  packet.Approval.Status,
		ClientName:      packet.Approval.ClientName,
		Approvers:       approverLines(packet.Approval.Approvers),
		LivePacketPath:  packet.Approval.LivePacketPath,
		ApprovalFocus:   append([]string{}, packet.QAGates.MustPass...),
		BlockingRisks:   append([]string{}, packet.QAGates.BlockIf...),
		SectionLabels:   sectionLabels(packet.Story.Sections),
		GeneratedFiles:  files,
		SourceBriefPath: brief.Metadata.Source,
		PacketGenerator: packet.Metadata.Generator,
	}
	if err := writeText(filepath.Join(dir, "summary.md"), renderSummary(summary)); err != nil {
		return err
	}

	slack := slackPayload{
		Channel: "#client-intake-pilot",
		Text: fmt.Sprintf(
			"Approval packet ready for %s at %s. Confirm title/thumbnail alignment, CTA order, and the named approver states before client handoff.",
			packet.SourceBrief.Title,
			nonEmptyOrFallback(packet.Approval.LivePacketPath, "the configured live packet path"),
		),
		Checklist: append([]string{
			nonEmptyOrFallback(fmt.Sprintf("Approval status: %s", packet.Approval.Status), "Approval status: pending"),
			firstNonEmpty(packet.QAGates.MustPass...),
			nonEmptyOrFallback(firstMatching(packet.QAGates.MustPass, "Human review"), "Human approval stays explicit."),
			nonEmptyOrFallback(firstMatching(packet.QAGates.BlockIf, "generic AI tools roundup"), "Reject if the packet drifts off the workflow."),
		}, approverLines(packet.Approval.Approvers)...),
	}
	if err := writeJSON(filepath.Join(dir, "slack-payload.json"), slack); err != nil {
		return err
	}

	drive := googleDrivePayload{
		FolderName:   fmt.Sprintf("review-%s", packet.SourceBrief.WorkingSlug),
		DocumentName: fmt.Sprintf("%s review bundle", packet.SourceBrief.Title),
		Viewers:      []string{"consultant@loopsmith.example", "reviewer@loopsmith.example"},
		Tags:         append([]string{}, packet.ProductionNotes.Tags...),
		Notes: []string{
			fmt.Sprintf("Source brief: %s", brief.Metadata.ID),
			fmt.Sprintf("Client: %s", nonEmptyOrFallback(packet.Approval.ClientName, "internal pilot")),
			fmt.Sprintf("Workflow: %s", packet.SourceBrief.Workflow),
			fmt.Sprintf("Primary proof asset: %s", packet.Story.SecondaryCTA),
			fmt.Sprintf("Live packet path: %s", nonEmptyOrFallback(packet.Approval.LivePacketPath, "not set")),
		},
	}
	if err := writeJSON(filepath.Join(dir, "google-drive-payload.json"), drive); err != nil {
		return err
	}

	notion := notionPayload{
		Database: "Client Approval Queue",
		Title:    packet.SourceBrief.Title,
		Status:   nonEmptyOrFallback(packet.Approval.Status, "Needs review"),
		Properties: map[string]string{
			"Episode ID":      packet.SourceBrief.EpisodeID,
			"Working Slug":    packet.SourceBrief.WorkingSlug,
			"Workflow":        packet.SourceBrief.Workflow,
			"Target Duration": packet.Narration.TargetDuration,
			"Primary CTA":     packet.Story.PrimaryCTA,
			"Thumbnail Text":  packet.PackagingGuardrails.ThumbnailText,
			"Approval Mode":   packet.Approval.Mode,
			"Client":          packet.Approval.ClientName,
			"Live Packet":     packet.Approval.LivePacketPath,
			"Generator":       packet.Metadata.Generator,
			"Generated From":  packet.Metadata.GeneratedFrom,
		},
		Checklist: append(append([]string{}, approverLines(packet.Approval.Approvers)...), packet.QAGates.MustPass...),
	}
	if err := writeJSON(filepath.Join(dir, "notion-payload.json"), notion); err != nil {
		return err
	}

	return nil
}

func renderSummary(summary reviewBundleSummary) string {
	var b strings.Builder
	b.WriteString("# Live Approval Review Bundle\n\n")
	b.WriteString("## Overview\n")
	b.WriteString(fmt.Sprintf("- Title: %s\n", summary.Title))
	b.WriteString(fmt.Sprintf("- Client: %s\n", nonEmptyOrFallback(summary.ClientName, "internal pilot")))
	b.WriteString(fmt.Sprintf("- Approval mode: %s\n", nonEmptyOrFallback(summary.ApprovalMode, "dry_run")))
	b.WriteString(fmt.Sprintf("- Approval status: %s\n", nonEmptyOrFallback(summary.ApprovalStatus, "needs_review")))
	b.WriteString(fmt.Sprintf("- Workflow: %s\n", summary.Workflow))
	b.WriteString(fmt.Sprintf("- Promise: %s\n", summary.Promise))
	b.WriteString(fmt.Sprintf("- Target duration: %s minutes\n", summary.TargetDuration))
	b.WriteString(fmt.Sprintf("- Primary CTA: %s\n", summary.PrimaryCTA))
	b.WriteString(fmt.Sprintf("- Thumbnail text: %s\n", summary.ThumbnailText))
	b.WriteString(fmt.Sprintf("- Source brief: %s\n", summary.SourceBriefPath))
	b.WriteString(fmt.Sprintf("- Live packet path: %s\n", nonEmptyOrFallback(summary.LivePacketPath, "not set")))
	b.WriteString(fmt.Sprintf("- Packet generator: %s\n\n", summary.PacketGenerator))

	b.WriteString("## Named Approvers\n")
	for _, item := range summary.Approvers {
		b.WriteString(fmt.Sprintf("- %s\n", item))
	}
	b.WriteString("\n")

	b.WriteString("## Review Focus\n")
	for _, item := range summary.ApprovalFocus {
		b.WriteString(fmt.Sprintf("- %s\n", item))
	}
	b.WriteString("\n## Block If\n")
	for _, item := range summary.BlockingRisks {
		b.WriteString(fmt.Sprintf("- %s\n", item))
	}
	b.WriteString("\n## Story Beats\n")
	for _, label := range summary.SectionLabels {
		b.WriteString(fmt.Sprintf("- %s\n", label))
	}
	b.WriteString("\n## Bundle Files\n")
	for _, file := range summary.GeneratedFiles {
		b.WriteString(fmt.Sprintf("- %s\n", file))
	}
	return b.String()
}

func sectionLabels(sections []scriptSection) []string {
	labels := make([]string, 0, len(sections))
	for _, section := range sections {
		label := section.ID
		if strings.TrimSpace(section.ChapterLabel) != "" {
			label = fmt.Sprintf("%s: %s", section.ID, section.ChapterLabel)
		}
		labels = append(labels, label)
	}
	return labels
}

func approverLines(approvers []approverBlock) []string {
	lines := make([]string, 0, len(approvers))
	for _, approver := range approvers {
		parts := []string{}
		if strings.TrimSpace(approver.Role) != "" {
			parts = append(parts, approver.Role)
		}
		if strings.TrimSpace(approver.Name) != "" {
			parts = append(parts, approver.Name)
		}
		line := strings.Join(parts, ": ")
		if line == "" {
			line = "Unassigned approver"
		}
		if strings.TrimSpace(approver.Status) != "" {
			line = fmt.Sprintf("%s (%s)", line, approver.Status)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{"Approver roster not set"}
	}
	return lines
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeText(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value), 0o644)
}
