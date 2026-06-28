package team

// broker_wiki_cards.go emits a system-authored chat card in #general when a
// new wiki article (NOT an update) first lands. It mirrors
// postIssueLifecycleCardLocked: same lock discipline (caller holds b.mu for
// the *Locked helper), same payload-as-map-then-marshal shape, same
// appendMessageLocked emission path.

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

// wikiCardChannel is the channel the wiki-article card posts to.
const wikiCardChannel = "general"

// emitCardLocked marshals the payload, bumps the counter, and appends the
// message. Caller holds b.mu. Returns silently on marshal failure — these
// cards are observational and must never block the underlying write.
func (b *Broker) emitCardLocked(kind, title, content string, payload map[string]string, tagged []string) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   wikiCardChannel,
		Kind:      kind,
		Title:     title,
		Content:   content,
		Tagged:    tagged,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   raw,
	})
}

// emitNewWikiArticleCard is the unlocked convenience wrapper used by the wiki
// HTTP handler, which runs without b.mu. It derives a title from the article
// body, then takes b.mu just long enough to append the card.
func (b *Broker) emitNewWikiArticleCard(path, content, authorHeader string) {
	author := strings.TrimSpace(authorHeader)
	title := markdownTitle(content, path)
	b.mu.Lock()
	b.postWikiArticleCreatedCardLocked(path, title, author)
	b.mu.Unlock()
}

// postWikiArticleCreatedCardLocked emits a #general card when a brand-new
// wiki article is first created. Update writes do NOT post a card —
// detection lives at the call site. Caller holds b.mu for write.
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
