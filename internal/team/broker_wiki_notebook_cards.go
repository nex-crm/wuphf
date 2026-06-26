package team

// broker_wiki_notebook_cards.go emits system-authored chat cards in #general
// when one of four wiki/notebook surface events happens:
//
//   - wiki_article_created       — a new wiki article (NOT an update) lands.
//   - notebook_entry_created     — an agent writes a NEW notebook entry.
//   - notebook_promotion_requested — an agent submits a notebook→wiki review.
//   - notebook_promotion_resolved  — a reviewer approves OR requests changes.
//
// These cards mirror postIssueLifecycleCardLocked: same lock discipline (caller
// holds b.mu), same payload-as-map-then-marshal shape, same appendMessageLocked
// emission path. The frontend renders kind-specific cards via MessageBubble's
// kind switch.
//
// All cards post to #general — these surfaces are not task-scoped so the
// channel is hardcoded rather than derived from a teamTask.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// wikiArticleIsNew returns true iff the given article path does not yet
// exist on disk. Any read error other than not-found returns false so we
// suppress the "new article" card on ambiguous failures.
func wikiArticleIsNew(repo *Repo, path string) bool {
	_, err := readArticle(repo, path)
	return err != nil && os.IsNotExist(err)
}

// notebookEntryIsNew is the notebook counterpart to wikiArticleIsNew.
func notebookEntryIsNew(worker *WikiWorker, path string) bool {
	_, err := worker.NotebookRead(path)
	return err != nil && os.IsNotExist(err)
}

// wikiNotebookCardChannel is the channel all four card kinds post to. Kept
// here as a single source of truth so a future move (e.g. to a dedicated
// #wiki channel) is one constant change.
const wikiNotebookCardChannel = "general"

// emitCardLocked is the shared tail used by all four helpers: marshal payload,
// bump counter, append message. Caller holds b.mu.
//
// Returns silently on marshal failure — these cards are observational and
// must never block the underlying write.
func (b *Broker) emitCardLocked(kind, title, content string, payload map[string]string, tagged []string) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   wikiNotebookCardChannel,
		Kind:      kind,
		Title:     title,
		Content:   content,
		Tagged:    tagged,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   raw,
	})
}

// emitNewWikiArticleCard is the unlocked convenience wrapper used by the
// wiki/notebook HTTP handlers, which run without b.mu. It derives a title
// from the article body, then takes b.mu just long enough to append the
// card.
func (b *Broker) emitNewWikiArticleCard(path, content, authorHeader string) {
	author := strings.TrimSpace(authorHeader)
	title := markdownTitle(content, path)
	b.mu.Lock()
	b.postWikiArticleCreatedCardLocked(path, title, author)
	b.mu.Unlock()
}

// emitNewNotebookEntryCard mirrors emitNewWikiArticleCard for notebook
// writes. Falls back to the body slug when the agent header is missing.
func (b *Broker) emitNewNotebookEntryCard(slug, path, content, authorHeader string) {
	author := strings.TrimSpace(authorHeader)
	if author == "" {
		author = strings.TrimSpace(slug)
	}
	title := markdownTitle(content, path)
	b.mu.Lock()
	b.postNotebookEntryCreatedCardLocked(slug, path, title, author)
	b.mu.Unlock()
}

// postWikiArticleCreatedCardLocked emits a #general card when a brand-new
// wiki article is first created. Update writes do NOT post a card —
// detection lives at the call site.
//
// Caller holds b.mu for write.
func (b *Broker) postWikiArticleCreatedCardLocked(path, title, author string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = path
	}
	author = strings.TrimSpace(author)
	payload := map[string]string{
		"path":   path,
		"title":  title,
		"author": author,
	}
	authorTag := author
	if authorTag == "" {
		authorTag = "system"
	} else {
		authorTag = "@" + authorTag
	}
	content := fmt.Sprintf("New wiki article: %s (by %s)", title, authorTag)
	b.emitCardLocked("wiki_article_created", title, content, payload, []string{"ceo"})
}

// postNotebookEntryCreatedCardLocked emits a #general card when an agent
// writes a NEW notebook entry. Update writes do NOT post a card.
//
// Caller holds b.mu for write.
func (b *Broker) postNotebookEntryCreatedCardLocked(slug, path, title, author string) {
	slug = strings.TrimSpace(slug)
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = path
	}
	author = strings.TrimSpace(author)
	payload := map[string]string{
		"slug":   slug,
		"path":   path,
		"title":  title,
		"author": author,
	}
	authorTag := author
	if authorTag == "" {
		authorTag = "system"
	} else {
		authorTag = "@" + authorTag
	}
	content := fmt.Sprintf("New notebook entry: %s (by %s)", title, authorTag)
	tagged := []string{"ceo"}
	if author != "" && author != "ceo" {
		tagged = append(tagged, author)
	}
	tagged = dedupeReassignTags(tagged)
	b.emitCardLocked("notebook_entry_created", title, content, payload, tagged)
}

// postNotebookPromotionRequestedCardLocked emits a #general card every time
// a notebook→wiki promotion is submitted. Every SubmitPromotion is new by
// definition; no dedup needed.
//
// Caller holds b.mu for write.
func (b *Broker) postNotebookPromotionRequestedCardLocked(promotionID, sourcePath, targetPath, submitter string) {
	promotionID = strings.TrimSpace(promotionID)
	if promotionID == "" {
		return
	}
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	submitter = strings.TrimSpace(submitter)
	payload := map[string]string{
		"promotion_id": promotionID,
		"source_path":  sourcePath,
		"target_path":  targetPath,
		"submitter":    submitter,
	}
	submitterTag := submitter
	if submitterTag == "" {
		submitterTag = "an agent"
	} else {
		submitterTag = "@" + submitterTag
	}
	title := "Promotion requested"
	if targetPath != "" {
		title = "Promotion requested: " + targetPath
	}
	content := fmt.Sprintf("%s requested review to promote %s → %s", submitterTag, sourcePath, targetPath)
	tagged := []string{"ceo"}
	if submitter != "" && submitter != "ceo" {
		tagged = append(tagged, submitter)
	}
	tagged = dedupeReassignTags(tagged)
	b.emitCardLocked("notebook_promotion_requested", title, content, payload, tagged)
}

// PromotionDecision discriminates approve from request-changes on a single
// notebook_promotion_resolved card kind. Exported so callers can pass it
// without re-stringifying.
type PromotionDecision string

const (
	PromotionDecisionApproved         PromotionDecision = "approved"
	PromotionDecisionChangesRequested PromotionDecision = "changes_requested"
)

// postNotebookPromotionResolvedCardLocked emits a #general card when a
// reviewer resolves a promotion. The single card kind covers both approve
// and request-changes; the `decision` payload field disambiguates.
//
// `originalSubmitter` is the source-slug from the underlying Promotion;
// when non-empty it's also tagged so the author gets a chat ping.
//
// Caller holds b.mu for write.
func (b *Broker) postNotebookPromotionResolvedCardLocked(promotionID, sourcePath, targetPath, reviewer string, decision PromotionDecision, rationale, originalSubmitter string) {
	promotionID = strings.TrimSpace(promotionID)
	if promotionID == "" {
		return
	}
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	reviewer = strings.TrimSpace(reviewer)
	rationale = strings.TrimSpace(rationale)
	originalSubmitter = strings.TrimSpace(originalSubmitter)
	switch decision {
	case PromotionDecisionApproved, PromotionDecisionChangesRequested:
	default:
		// Unknown decision: refuse to emit a misleading card.
		return
	}
	payload := map[string]string{
		"promotion_id": promotionID,
		"source_path":  sourcePath,
		"target_path":  targetPath,
		"reviewer":     reviewer,
		"decision":     string(decision),
		"rationale":    rationale,
		"submitter":    originalSubmitter,
	}
	reviewerTag := reviewer
	if reviewerTag == "" {
		reviewerTag = "Human"
	} else {
		reviewerTag = "@" + reviewerTag
	}
	var (
		title   string
		content string
	)
	if decision == PromotionDecisionApproved {
		title = "Promotion approved"
		if targetPath != "" {
			title = "Promotion approved: " + targetPath
		}
		content = fmt.Sprintf("%s approved promotion of %s → %s", reviewerTag, sourcePath, targetPath)
	} else {
		title = "Changes requested"
		if targetPath != "" {
			title = "Changes requested: " + targetPath
		}
		content = fmt.Sprintf("%s requested changes on promotion %s → %s", reviewerTag, sourcePath, targetPath)
	}
	tagged := []string{"ceo"}
	if originalSubmitter != "" && originalSubmitter != "ceo" {
		tagged = append(tagged, originalSubmitter)
	}
	tagged = dedupeReassignTags(tagged)
	b.emitCardLocked("notebook_promotion_resolved", title, content, payload, tagged)
}
