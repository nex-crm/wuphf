package team

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	richArtifactKindNotebookHTML = "notebook_html"
	richArtifactKindWikiVisual   = "wiki_visual"
	richArtifactRepresentation   = "html"
	richArtifactTrustDraft       = "draft"
	richArtifactTrustPromoted    = "promoted"
	richArtifactSanitizerVersion = "sandbox-v2"
	richArtifactRoot             = "wiki/visual-artifacts"
	richArtifactMaxHTMLBytes     = 1024 * 1024
)

// Promotion status values surfaced to the frontend. The contract is:
//
//	{ "status": "draft" }
//	{ "status": "promoted_to_notebook", "owner_slug": "...", "entry_slug": "..." }
//	{ "status": "promoted_to_wiki",     "wiki_path":  "team/.../...md" }
//
// See web/src/api/richArtifacts.ts ArtifactPromotion for the consumer side.
const (
	ArtifactPromotionStatusDraft              = "draft"
	ArtifactPromotionStatusPromotedToNotebook = "promoted_to_notebook"
	ArtifactPromotionStatusPromotedToWiki     = "promoted_to_wiki"
)

// ArtifactPromotion is the canonical "where does this artifact live now?"
// signal. Exactly one of the optional fields is populated, picked by Status.
// JSON tags use snake_case to match the frontend's ArtifactPromotion union.
type ArtifactPromotion struct {
	Status string `json:"status"`
	// promoted_to_notebook fields.
	OwnerSlug string `json:"owner_slug,omitempty"`
	EntrySlug string `json:"entry_slug,omitempty"`
	// promoted_to_wiki fields.
	WikiPath string `json:"wiki_path,omitempty"`
}

// AttachedNotebookEntry names the notebook entry that hosts a visual artifact
// as its canonical home. Auto-created at artifact-create time so chat link
// cards always have a real destination — the notebook detail view embeds the
// artifact via the `visual-artifact:<id>` marker the auto-write plants.
//
// Stored snake_case on the artifact JSON because the frontend's wire contract
// expects that exact field name; see web/src/api/richArtifacts.ts.
type AttachedNotebookEntry struct {
	OwnerSlug string `json:"owner_slug"`
	EntrySlug string `json:"entry_slug"`
}

var errRichArtifactCaller = errors.New("visual artifact: caller error")

type richArtifactCallerError struct {
	err error
}

func (e richArtifactCallerError) Error() string {
	return e.err.Error()
}

func (e richArtifactCallerError) Unwrap() []error {
	return []error{errRichArtifactCaller, e.err}
}

func newRichArtifactCallerError(format string, args ...any) error {
	return richArtifactCallerError{err: fmt.Errorf(format, args...)}
}

func markRichArtifactCallerError(err error) error {
	if err == nil || errors.Is(err, errRichArtifactCaller) {
		return err
	}
	return richArtifactCallerError{err: err}
}

// RichArtifact is the durable metadata for a visual agent artifact. The HTML
// body is stored separately at HTMLPath so metadata can be listed without
// shipping the full rendered document to every client.
type RichArtifact struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	TrustLevel         string   `json:"trustLevel"`
	Representation     string   `json:"representation"`
	HTMLPath           string   `json:"htmlPath"`
	SourceMarkdownPath string   `json:"sourceMarkdownPath,omitempty"`
	PromotedWikiPath   string   `json:"promotedWikiPath,omitempty"`
	RelatedTaskID      string   `json:"relatedTaskId,omitempty"`
	RelatedMessageID   string   `json:"relatedMessageId,omitempty"`
	RelatedReceiptIDs  []string `json:"relatedReceiptIds,omitempty"`
	CreatedBy          string   `json:"createdBy"`
	// OwnerSlug duplicates CreatedBy under the snake_case name the FE wire
	// contract expects. Persisted at create time so the listing endpoint can
	// return it without re-deriving from CreatedBy. Older artifacts written
	// before this field existed will see it filled in by WithDerivedPromotion.
	OwnerSlug        string `json:"owner_slug"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	ContentHash      string `json:"contentHash"`
	SanitizerVersion string `json:"sanitizerVersion"`
	// Promotion is the canonical surface for the UI's "where does this
	// artifact live now?" decision. Older artifacts written before this
	// field existed may omit it on disk; readers MUST fall back to
	// DerivePromotion (which uses TrustLevel + PromotedWikiPath +
	// SourceMarkdownPath) so the frontend always has something to read.
	Promotion *ArtifactPromotion `json:"promotion,omitempty"`
	// AttachedToNotebookEntry names the canonical notebook home for this
	// artifact. Populated by the auto-create notebook entry path so chat
	// link cards can deep-link to /notebooks/{owner}/{entry} instead of
	// the standalone /articles/{id} viewer. Nil for legacy artifacts and
	// for promoted-to-wiki artifacts where the wiki path is canonical.
	AttachedToNotebookEntry *AttachedNotebookEntry `json:"attached_to_notebook_entry"`
}

// DerivePromotion returns the canonical ArtifactPromotion for an artifact.
// It returns the persisted Promotion field when present, otherwise it
// reconstructs a best-effort promotion from the legacy fields so artifacts
// written before the promotion contract existed keep working.
func (a RichArtifact) DerivePromotion() ArtifactPromotion {
	if a.Promotion != nil && a.Promotion.Status != "" {
		return *a.Promotion
	}
	if strings.TrimSpace(a.PromotedWikiPath) != "" {
		return ArtifactPromotion{
			Status:   ArtifactPromotionStatusPromotedToWiki,
			WikiPath: a.PromotedWikiPath,
		}
	}
	if owner, entry, ok := notebookOwnerAndEntryFromPath(a.SourceMarkdownPath); ok {
		return ArtifactPromotion{
			Status:    ArtifactPromotionStatusPromotedToNotebook,
			OwnerSlug: owner,
			EntrySlug: entry,
		}
	}
	return ArtifactPromotion{Status: ArtifactPromotionStatusDraft}
}

// WithDerivedPromotion returns a copy of the artifact with its Promotion
// pointer populated via DerivePromotion. Use this on the read path so JSON
// responses always carry a non-nil promotion field even for legacy artifacts.
//
// Also backfills OwnerSlug from CreatedBy when missing so the FE wire
// contract holds for artifacts written before OwnerSlug was persisted.
// AttachedToNotebookEntry is left nil for legacy artifacts that never had a
// notebook home written — the chat card falls back to /articles/{id} for
// those, which is the historical behavior.
func (a RichArtifact) WithDerivedPromotion() RichArtifact {
	p := a.DerivePromotion()
	a.Promotion = &p
	if strings.TrimSpace(a.OwnerSlug) == "" {
		a.OwnerSlug = a.CreatedBy
	}
	return a
}

// notebookOwnerAndEntryFromPath splits "agents/{slug}/notebook/{file}.md"
// into ("{slug}", "{file}", true). Returns false for any other shape so
// callers can fall back to a draft promotion safely.
func notebookOwnerAndEntryFromPath(path string) (string, string, bool) {
	rel := strings.TrimSpace(path)
	if rel == "" {
		return "", "", false
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	const prefix = "agents/"
	const middle = "/notebook/"
	if !strings.HasPrefix(rel, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(rel, prefix)
	mid := strings.Index(rest, middle)
	if mid <= 0 {
		return "", "", false
	}
	owner := rest[:mid]
	entry := rest[mid+len(middle):]
	if !strings.HasSuffix(strings.ToLower(entry), ".md") {
		return "", "", false
	}
	entry = entry[:len(entry)-len(".md")]
	if owner == "" || entry == "" {
		return "", "", false
	}
	return owner, entry, true
}

type RichArtifactFilter struct {
	CreatedBy          string
	SourceMarkdownPath string
	PromotedWikiPath   string
}

type RichArtifactCreateRequest struct {
	Slug               string   `json:"slug"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	HTML               string   `json:"html"`
	SourceMarkdownPath string   `json:"source_markdown_path"`
	RelatedTaskID      string   `json:"related_task_id"`
	RelatedMessageID   string   `json:"related_message_id"`
	RelatedReceiptIDs  []string `json:"related_receipt_ids"`
	CommitMessage      string   `json:"commit_message"`
}

type RichArtifactPromoteRequest struct {
	ActorSlug       string `json:"actor_slug"`
	TargetWikiPath  string `json:"target_wiki_path"`
	MarkdownSummary string `json:"markdown_summary"`
	Mode            string `json:"mode"`
	CommitMessage   string `json:"commit_message"`
}

type wikiRichArtifactWork struct {
	Artifact RichArtifact
	ID       string
	HTML     string
	Markdown string
	Now      time.Time
}

func newRichArtifact(req RichArtifactCreateRequest, now time.Time) (RichArtifact, string, error) {
	slug := strings.TrimSpace(req.Slug)
	if err := validateNotebookSlug(slug); err != nil {
		return RichArtifact{}, "", markRichArtifactCallerError(err)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return RichArtifact{}, "", newRichArtifactCallerError("visual artifact: title is required")
	}
	if strings.ContainsRune(title, '\x00') {
		return RichArtifact{}, "", newRichArtifactCallerError("visual artifact: title must not contain NUL bytes")
	}
	summary := strings.TrimSpace(req.Summary)
	html := req.HTML
	if err := validateRichArtifactHTMLPolicy(html); err != nil {
		return RichArtifact{}, "", err
	}
	sourcePath := strings.TrimSpace(req.SourceMarkdownPath)
	if sourcePath != "" {
		if err := validateNotebookWritePath(slug, sourcePath); err != nil {
			return RichArtifact{}, "", markRichArtifactCallerError(err)
		}
	}
	relatedReceiptIDs := cleanRichArtifactStringList(req.RelatedReceiptIDs)
	createdAt := now.UTC().Format(time.RFC3339Nano)
	contentHash := richArtifactContentHash(html)
	id := richArtifactID(slug, title, createdAt, html, summary, sourcePath, strings.TrimSpace(req.RelatedTaskID), strings.TrimSpace(req.RelatedMessageID), relatedReceiptIDs)
	artifact := RichArtifact{
		ID:                 id,
		Kind:               richArtifactKindNotebookHTML,
		Title:              title,
		Summary:            summary,
		TrustLevel:         richArtifactTrustDraft,
		Representation:     richArtifactRepresentation,
		HTMLPath:           richArtifactHTMLPath(id),
		SourceMarkdownPath: sourcePath,
		RelatedTaskID:      strings.TrimSpace(req.RelatedTaskID),
		RelatedMessageID:   strings.TrimSpace(req.RelatedMessageID),
		RelatedReceiptIDs:  relatedReceiptIDs,
		CreatedBy:          slug,
		// OwnerSlug mirrors CreatedBy under the FE wire-contract name so the
		// list/get responses carry it without a per-request derivation.
		OwnerSlug:        slug,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
		ContentHash:      contentHash,
		SanitizerVersion: richArtifactSanitizerVersion,
	}
	// New artifacts start as drafts unless they are immediately attached to
	// a notebook entry via SourceMarkdownPath. The promote tool flips this to
	// promoted_to_wiki on success.
	if owner, entry, ok := notebookOwnerAndEntryFromPath(sourcePath); ok {
		artifact.Promotion = &ArtifactPromotion{
			Status:    ArtifactPromotionStatusPromotedToNotebook,
			OwnerSlug: owner,
			EntrySlug: entry,
		}
	} else {
		artifact.Promotion = &ArtifactPromotion{Status: ArtifactPromotionStatusDraft}
	}
	return artifact, html, nil
}

func richArtifactID(slug, title, createdAt, html, summary, sourcePath, relatedTaskID, relatedMessageID string, relatedReceiptIDs []string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		slug,
		title,
		createdAt,
		html,
		summary,
		sourcePath,
		relatedTaskID,
		relatedMessageID,
		strings.Join(relatedReceiptIDs, "\x00"),
	}, "\x00")))
	return "ra_" + hex.EncodeToString(sum[:])[:16]
}

func richArtifactContentHash(html string) string {
	sum := sha256.Sum256([]byte(html))
	return hex.EncodeToString(sum[:])
}

func richArtifactMetaPath(id string) string {
	return filepath.ToSlash(filepath.Join(richArtifactRoot, id+".json"))
}

func richArtifactHTMLPath(id string) string {
	return filepath.ToSlash(filepath.Join(richArtifactRoot, id+".html"))
}

func validateRichArtifactID(id string) error {
	id = strings.TrimSpace(id)
	if len(id) != len("ra_")+16 || !strings.HasPrefix(id, "ra_") {
		return newRichArtifactCallerError("visual artifact: invalid id %q", id)
	}
	for _, ch := range strings.TrimPrefix(id, "ra_") {
		if !((ch >= 'a' && ch <= 'f') || (ch >= '0' && ch <= '9')) {
			return newRichArtifactCallerError("visual artifact: invalid id %q", id)
		}
	}
	return nil
}

func validateRichArtifactHTML(html string) error {
	return validateRichArtifactHTMLPolicy(html)
}

func validateRichArtifactHTMLPolicy(html string) error {
	if strings.TrimSpace(html) == "" {
		return newRichArtifactCallerError("visual artifact: html is required")
	}
	if len([]byte(html)) > richArtifactMaxHTMLBytes {
		return newRichArtifactCallerError("visual artifact: html exceeds %d bytes", richArtifactMaxHTMLBytes)
	}
	if strings.ContainsRune(html, '\x00') {
		return newRichArtifactCallerError("visual artifact: html must not contain NUL bytes")
	}
	if err := validateRichArtifactSandboxPolicy(html); err != nil {
		return err
	}
	return nil
}

func validateRichArtifactSandboxPolicy(raw string) error {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	styleDepth := 0
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(tokenizer.Err(), io.EOF) {
				return nil
			}
			return newRichArtifactCallerError("visual artifact: html parse failed: %v", tokenizer.Err())
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if reason, blocked := richArtifactBlockedElementReason(tag); blocked {
				return newRichArtifactCallerError("visual artifact: html element <%s> is not allowed: %s", tag, reason)
			}
			if tag == "style" && tt == html.StartTagToken {
				styleDepth++
			}
			if err := validateRichArtifactAttributes(tag, token.Attr); err != nil {
				return err
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if strings.EqualFold(token.Data, "style") && styleDepth > 0 {
				styleDepth--
			}
		case html.TextToken:
			if styleDepth > 0 {
				if err := validateRichArtifactCSS(tokenizer.Token().Data); err != nil {
					return err
				}
			}
		}
	}
}

func richArtifactBlockedElementReason(tag string) (string, bool) {
	switch tag {
	case "base":
		return "base URLs can rewrite link targets inside the sandbox", true
	case "embed", "iframe", "object":
		return "nested browsing contexts and plugins are not part of the artifact sandbox", true
	case "form":
		return "forms are blocked by the artifact sandbox", true
	case "link":
		return "external stylesheets and preloads are not allowed", true
	default:
		return "", false
	}
}

func validateRichArtifactAttributes(tag string, attrs []html.Attribute) error {
	metaRefresh := false
	for _, attr := range attrs {
		key := strings.ToLower(strings.TrimSpace(attr.Key))
		value := strings.TrimSpace(attr.Val)
		if strings.HasPrefix(key, "on") && len(key) > 2 {
			return newRichArtifactCallerError("visual artifact: html attribute %s on <%s> is not allowed", key, tag)
		}
		switch key {
		case "srcset":
			return newRichArtifactCallerError("visual artifact: html attribute %s on <%s> is not allowed", key, tag)
		case "style":
			if err := validateRichArtifactCSS(value); err != nil {
				return err
			}
		case "http-equiv":
			metaRefresh = tag == "meta" && strings.EqualFold(value, "refresh")
		}
		if tag == "script" && key == "src" {
			return newRichArtifactCallerError("visual artifact: external script src is not allowed")
		}
		if richArtifactURLAttribute(key) {
			if err := validateRichArtifactURL(tag, key, value); err != nil {
				return err
			}
		}
	}
	if metaRefresh {
		return newRichArtifactCallerError("visual artifact: meta refresh is not allowed")
	}
	return nil
}

func richArtifactURLAttribute(attr string) bool {
	switch attr {
	case "action", "formaction", "href", "poster", "src", "xlink:href":
		return true
	default:
		return false
	}
}

func validateRichArtifactURL(tag, attr, value string) error {
	if value == "" || strings.HasPrefix(value, "#") {
		return nil
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") {
		return nil
	}
	return newRichArtifactCallerError("visual artifact: <%s> %s must use a data/blob URL or fragment reference", tag, attr)
}

func validateRichArtifactCSS(css string) error {
	if containsASCIIFold(css, "@import") {
		return newRichArtifactCallerError("visual artifact: css @import is not allowed (including empty data: URLs); use system fonts like Georgia, Times, Cambria, or Courier directly in font-family")
	}
	if containsASCIIFold(css, "expression(") {
		return newRichArtifactCallerError("visual artifact: css expression() is not allowed")
	}
	for offset := 0; ; {
		idx := indexASCIIFold(css[offset:], "url(")
		if idx < 0 {
			return nil
		}
		start := offset + idx + len("url(")
		endRel := strings.Index(css[start:], ")")
		if endRel < 0 {
			return newRichArtifactCallerError("visual artifact: css url() is malformed")
		}
		end := start + endRel
		value := strings.Trim(strings.TrimSpace(css[start:end]), `"'`)
		if err := validateRichArtifactURL("style", "url()", value); err != nil {
			return err
		}
		offset = end + 1
	}
}

func containsASCIIFold(s, token string) bool {
	return indexASCIIFold(s, token) >= 0
}

func indexASCIIFold(s, token string) int {
	if token == "" {
		return 0
	}
	if len(token) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(token); i++ {
		matched := true
		for j := 0; j < len(token); j++ {
			if !equalASCIIFold(s[i+j], token[j]) {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

func equalASCIIFold(a, b byte) bool {
	if 'A' <= a && a <= 'Z' {
		a += 'a' - 'A'
	}
	if 'A' <= b && b <= 'Z' {
		b += 'a' - 'A'
	}
	return a == b
}

func cleanRichArtifactStringList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (r *Repo) CommitRichArtifact(ctx context.Context, slug string, artifact RichArtifact, html, message string) (RichArtifact, string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.TrimSpace(slug) == "" {
		return RichArtifact{}, "", 0, newRichArtifactCallerError("visual artifact: author slug is required")
	}
	// Derive (and create) the canonical notebook home BEFORE marshalling the
	// metadata so the persisted JSON carries attached_to_notebook_entry from
	// the very first read. ensureRichArtifactNotebookHomeLocked is a no-op
	// when the agent already provided a SourceMarkdownPath, treating that
	// existing entry as the home.
	notebookPath, notebookContent, attached, err := r.ensureRichArtifactNotebookHomeLocked(slug, artifact)
	if err != nil {
		return RichArtifact{}, "", 0, err
	}
	if attached != nil {
		artifact.AttachedToNotebookEntry = attached
	}
	// Mirror the auto-created notebook home into SourceMarkdownPath so the
	// existing notebook detail view (NotebookVisualArtifacts) — which looks
	// artifacts up via fetchRichArtifacts({sourceMarkdownPath}) — finds and
	// embeds this artifact inline. Without this, the entry shows the
	// `visual-artifact:<id>` marker as text but no embed renders.
	if notebookPath != "" && strings.TrimSpace(artifact.SourceMarkdownPath) == "" {
		artifact.SourceMarkdownPath = notebookPath
	}

	if err := validateRichArtifactForWrite(artifact, html); err != nil {
		return RichArtifact{}, "", 0, err
	}
	metaPath := richArtifactMetaPath(artifact.ID)
	if artifact.HTMLPath != richArtifactHTMLPath(artifact.ID) {
		return RichArtifact{}, "", 0, newRichArtifactCallerError("visual artifact: htmlPath must be %q", richArtifactHTMLPath(artifact.ID))
	}
	metaBytes, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: marshal metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')
	if err := os.MkdirAll(filepath.Join(r.root, richArtifactRoot), 0o700); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: mkdir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.root, artifact.HTMLPath), []byte(html), 0o600); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: write html: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.root, metaPath), metaBytes, 0o600); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: write metadata: %w", err)
	}
	addArgs := []string{"add", "--", artifact.HTMLPath, metaPath}
	if notebookPath != "" {
		fullNotebook := filepath.Join(r.root, filepath.FromSlash(notebookPath))
		if err := os.MkdirAll(filepath.Dir(fullNotebook), 0o700); err != nil {
			return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: mkdir notebook home: %w", err)
		}
		if err := os.WriteFile(fullNotebook, []byte(notebookContent), 0o600); err != nil {
			return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: write notebook home: %w", err)
		}
		addArgs = append(addArgs, notebookPath)
	}
	if out, err := r.runGitLocked(ctx, slug, addArgs...); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: git add: %w: %s", err, out)
	}
	if empty, err := r.cachedDiffEmptyLocked(ctx, slug); err != nil {
		return RichArtifact{}, "", 0, err
	} else if empty {
		head, err := r.currentHeadLocked(ctx)
		return artifact, head, len(html) + len(metaBytes), err
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("artifact: create visual artifact %s", artifact.ID)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: git commit: %w: %s", err, out)
	}
	sha, err := r.currentHeadLocked(ctx)
	return artifact, sha, len(html) + len(metaBytes), err
}

// ensureRichArtifactNotebookHomeLocked decides the canonical notebook home
// for a freshly-created visual artifact and returns the (relPath, content,
// attached) tuple. Caller must hold r.mu.
//
//   - When the artifact already carries a SourceMarkdownPath (legacy companion
//     mode), the existing notebook entry IS the home — no new file is written
//     and the returned relPath is empty.
//   - Otherwise a minimal companion entry is materialized under
//     agents/{owner}/notebook/{entry_slug}.md. The body plants a
//     `visual-artifact:<id>` marker so the notebook detail view renders the
//     artifact via the existing NotebookVisualArtifacts component.
//   - Entry slug is derived from the title (kebab-cased, sanitized) with
//     -2/-3/... suffixes to break collisions on disk. We never touch an
//     existing file with a stranger's content.
func (r *Repo) ensureRichArtifactNotebookHomeLocked(slug string, artifact RichArtifact) (string, string, *AttachedNotebookEntry, error) {
	if existing := strings.TrimSpace(artifact.SourceMarkdownPath); existing != "" {
		owner, entry, ok := notebookOwnerAndEntryFromPath(existing)
		if !ok {
			return "", "", nil, nil
		}
		return "", "", &AttachedNotebookEntry{OwnerSlug: owner, EntrySlug: entry}, nil
	}
	if err := validateNotebookSlug(slug); err != nil {
		return "", "", nil, markRichArtifactCallerError(err)
	}
	baseSlug := slugifyNotebookEntry(artifact.Title)
	if baseSlug == "" {
		// Fall back to the artifact ID so we always have a deterministic,
		// collision-free filename — empty/all-punctuation titles still get a
		// notebook home, just an opaquely-named one.
		baseSlug = strings.TrimPrefix(artifact.ID, "ra_")
		if baseSlug == "" {
			baseSlug = "visual-artifact"
		}
	}
	chosenSlug, relPath := r.pickAvailableNotebookEntrySlug(slug, baseSlug)
	body := renderNotebookHomeBody(artifact)
	return relPath, body, &AttachedNotebookEntry{OwnerSlug: slug, EntrySlug: chosenSlug}, nil
}

// pickAvailableNotebookEntrySlug returns (entrySlug, relPath) for the first
// slug in the {base, base-2, base-3, ...} sequence whose target file does not
// already exist. Caller must hold r.mu. The 100-attempt cap is defensive:
// hitting it would require 100 colliding titles for the same owner, which
// signals a bug rather than a real collision.
func (r *Repo) pickAvailableNotebookEntrySlug(owner, base string) (string, string) {
	for attempt := 0; attempt < 100; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", base, attempt+1)
		}
		rel := filepath.ToSlash(filepath.Join("agents", owner, "notebook", candidate+".md"))
		full := filepath.Join(r.root, filepath.FromSlash(rel))
		if _, err := os.Stat(full); errors.Is(err, os.ErrNotExist) {
			return candidate, rel
		}
	}
	// Defensive fallback — exhausted suffix budget. Stamp the artifact ID
	// into the slug so the caller still gets a unique filename rather than
	// silently overwriting somebody else's notebook entry.
	candidate := fmt.Sprintf("%s-%s", base, "fallback")
	rel := filepath.ToSlash(filepath.Join("agents", owner, "notebook", candidate+".md"))
	return candidate, rel
}

// renderNotebookHomeBody returns the markdown body for the auto-created
// notebook entry. The frontmatter stays minimal (title, created_at,
// attached_artifact_id) and the body plants the `visual-artifact:<id>`
// marker the notebook detail view already knows how to render.
func renderNotebookHomeBody(artifact RichArtifact) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: ")
	b.WriteString(escapeYAMLScalar(artifact.Title))
	b.WriteByte('\n')
	b.WriteString("created_at: ")
	b.WriteString(artifact.CreatedAt)
	b.WriteByte('\n')
	b.WriteString("attached_artifact_id: ")
	b.WriteString(artifact.ID)
	b.WriteByte('\n')
	b.WriteString("---\n\n")
	b.WriteString("# ")
	b.WriteString(artifact.Title)
	b.WriteString("\n\n")
	if summary := strings.TrimSpace(artifact.Summary); summary != "" {
		b.WriteString(summary)
		b.WriteString("\n\n")
	}
	b.WriteString("visual-artifact:")
	b.WriteString(artifact.ID)
	b.WriteByte('\n')
	return b.String()
}

// escapeYAMLScalar wraps a YAML scalar in double quotes when it contains
// characters that would otherwise confuse the parser. Keeps the frontmatter
// readable for plain-text titles and safe for titles with colons, hashes, or
// leading whitespace.
func escapeYAMLScalar(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#\"'\n\r\t") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}

// slugifyNotebookEntry turns a human title into a filesystem-safe kebab slug.
// Same logic the seed/dev scripts use for derived filenames: ASCII letters
// and digits stay, every other rune collapses into a hyphen, and runs of
// hyphens are squashed. Empty result means "title was all punctuation".
func slugifyNotebookEntry(title string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	// Keep filenames sane — git tools and shells get sad past a few hundred
	// bytes. 80 chars is generous for a derived notebook slug.
	if len(out) > 80 {
		out = strings.TrimRight(out[:80], "-")
	}
	return out
}

func validateRichArtifactForWrite(artifact RichArtifact, html string) error {
	if err := validateRichArtifactID(artifact.ID); err != nil {
		return err
	}
	if artifact.Kind != richArtifactKindNotebookHTML && artifact.Kind != richArtifactKindWikiVisual {
		return newRichArtifactCallerError("visual artifact: unsupported kind %q", artifact.Kind)
	}
	if strings.TrimSpace(artifact.Title) == "" {
		return newRichArtifactCallerError("visual artifact: title is required")
	}
	if strings.ContainsRune(artifact.Title, '\x00') {
		return newRichArtifactCallerError("visual artifact: title must not contain NUL bytes")
	}
	if artifact.Representation != richArtifactRepresentation {
		return newRichArtifactCallerError("visual artifact: unsupported representation %q", artifact.Representation)
	}
	if artifact.TrustLevel != richArtifactTrustDraft && artifact.TrustLevel != richArtifactTrustPromoted {
		return newRichArtifactCallerError("visual artifact: unsupported trust level %q", artifact.TrustLevel)
	}
	if artifact.CreatedBy == "" {
		return newRichArtifactCallerError("visual artifact: createdBy is required")
	}
	if artifact.CreatedAt == "" || artifact.UpdatedAt == "" {
		return newRichArtifactCallerError("visual artifact: timestamps are required")
	}
	if artifact.ContentHash != richArtifactContentHash(html) {
		return newRichArtifactCallerError("visual artifact: content hash mismatch")
	}
	if artifact.SanitizerVersion != richArtifactSanitizerVersion {
		return newRichArtifactCallerError("visual artifact: unsupported sanitizer version %q", artifact.SanitizerVersion)
	}
	return validateRichArtifactHTML(html)
}

func (r *Repo) PromoteRichArtifact(ctx context.Context, actorSlug, id, targetPath, markdown, mode, message string, now time.Time) (RichArtifact, string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	actorSlug = strings.TrimSpace(actorSlug)
	if actorSlug == "" {
		return RichArtifact{}, "", 0, newRichArtifactCallerError("visual artifact: actor slug is required")
	}
	if err := validateRichArtifactID(id); err != nil {
		return RichArtifact{}, "", 0, err
	}
	if err := validateArticlePath(targetPath); err != nil {
		return RichArtifact{}, "", 0, markRichArtifactCallerError(err)
	}
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return RichArtifact{}, "", 0, newRichArtifactCallerError("visual artifact: markdown_summary is required")
	}
	if mode == "" {
		mode = "create"
	}
	fullTarget := filepath.Join(r.root, filepath.FromSlash(targetPath))
	switch mode {
	case "create":
		if _, err := os.Stat(fullTarget); err == nil {
			return RichArtifact{}, "", 0, newRichArtifactCallerError("wiki: article already exists at %q; use replace or append_section", targetPath)
		}
	case "replace", "append_section":
		// Valid modes; the actual file operation happens after the artifact
		// metadata has been loaded and promoted.
	default:
		return RichArtifact{}, "", 0, newRichArtifactCallerError("wiki: unknown write mode %q; expected create|replace|append_section", mode)
	}
	artifact, _, err := r.readRichArtifactLocked(id)
	if err != nil {
		return RichArtifact{}, "", 0, err
	}
	artifact.Kind = richArtifactKindWikiVisual
	artifact.TrustLevel = richArtifactTrustPromoted
	artifact.PromotedWikiPath = targetPath
	artifact.UpdatedAt = now.UTC().Format(time.RFC3339)
	artifact.Promotion = &ArtifactPromotion{
		Status:   ArtifactPromotionStatusPromotedToWiki,
		WikiPath: targetPath,
	}
	content := markdownWithRichArtifactProvenance(markdown, artifact)
	if err := os.MkdirAll(filepath.Dir(fullTarget), 0o700); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("wiki: mkdir %s: %w", filepath.Dir(fullTarget), err)
	}
	bytesWritten := len(content)
	switch mode {
	case "create", "replace":
		if err := os.WriteFile(fullTarget, []byte(content), 0o600); err != nil {
			return RichArtifact{}, "", 0, fmt.Errorf("wiki: write article: %w", err)
		}
	case "append_section":
		existing, err := os.ReadFile(fullTarget)
		if err != nil && !os.IsNotExist(err) {
			return RichArtifact{}, "", 0, fmt.Errorf("wiki: read for append: %w", err)
		}
		var buf []byte
		if len(existing) > 0 {
			buf = append(buf, existing...)
			if !strings.HasSuffix(string(existing), "\n") {
				buf = append(buf, '\n')
			}
			buf = append(buf, '\n')
		}
		buf = append(buf, []byte(content)...)
		if err := os.WriteFile(fullTarget, buf, 0o600); err != nil {
			return RichArtifact{}, "", 0, fmt.Errorf("wiki: write article: %w", err)
		}
	}
	metaBytes, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: marshal metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')
	metaPath := richArtifactMetaPath(id)
	if err := os.WriteFile(filepath.Join(r.root, metaPath), metaBytes, 0o600); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: write metadata: %w", err)
	}
	if err := r.regenerateIndexLocked(); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("wiki: index regen: %w", err)
	}
	if out, err := r.runGitLocked(ctx, actorSlug, "add", "--", targetPath, "index/all.md", metaPath); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: git add promotion: %w: %s", err, out)
	}
	if empty, err := r.cachedDiffEmptyLocked(ctx, actorSlug); err != nil {
		return RichArtifact{}, "", 0, err
	} else if empty {
		head, err := r.currentHeadLocked(ctx)
		return artifact, head, bytesWritten + len(metaBytes), err
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("wiki: promote visual artifact %s", id)
	}
	if out, err := r.runGitLocked(ctx, actorSlug, "commit", "-q", "-m", commitMsg); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: git commit promotion: %w: %s", err, out)
	}
	sha, err := r.currentHeadLocked(ctx)
	return artifact, sha, bytesWritten + len(metaBytes), err
}

func markdownWithRichArtifactProvenance(markdown string, artifact RichArtifact) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(markdown))
	b.WriteString("\n\n---\n\n")
	b.WriteString("## Visual Artifact Provenance\n\n")
	b.WriteString("- Artifact ID: `")
	b.WriteString(artifact.ID)
	b.WriteString("`\n")
	b.WriteString("- Created by: `")
	b.WriteString(artifact.CreatedBy)
	b.WriteString("`\n")
	if artifact.SourceMarkdownPath != "" {
		b.WriteString("- Source notebook: `")
		b.WriteString(artifact.SourceMarkdownPath)
		b.WriteString("`\n")
	}
	if len(artifact.RelatedReceiptIDs) > 0 {
		b.WriteString("- Receipts: `")
		b.WriteString(strings.Join(artifact.RelatedReceiptIDs, "`, `"))
		b.WriteString("`\n")
	}
	b.WriteString("- Visual view: `")
	b.WriteString(artifact.HTMLPath)
	b.WriteString("`\n")
	return b.String()
}

func (r *Repo) RichArtifact(id string) (RichArtifact, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readRichArtifactLocked(id)
}

func (r *Repo) readRichArtifactLocked(id string) (RichArtifact, string, error) {
	if err := validateRichArtifactID(id); err != nil {
		return RichArtifact{}, "", err
	}
	metaBytes, err := os.ReadFile(filepath.Join(r.root, richArtifactMetaPath(id)))
	if err != nil {
		return RichArtifact{}, "", fmt.Errorf("visual artifact: read metadata: %w", err)
	}
	var artifact RichArtifact
	if err := json.Unmarshal(metaBytes, &artifact); err != nil {
		return RichArtifact{}, "", fmt.Errorf("visual artifact: decode metadata: %w", err)
	}
	if artifact.ID != id {
		return RichArtifact{}, "", fmt.Errorf("visual artifact: metadata id mismatch")
	}
	if artifact.HTMLPath != richArtifactHTMLPath(id) {
		return RichArtifact{}, "", fmt.Errorf("visual artifact: htmlPath must be %q", richArtifactHTMLPath(id))
	}
	html, err := os.ReadFile(filepath.Join(r.root, artifact.HTMLPath))
	if err != nil {
		return RichArtifact{}, "", fmt.Errorf("visual artifact: read html: %w", err)
	}
	if err := validateRichArtifactForWrite(artifact, string(html)); err != nil {
		return RichArtifact{}, "", err
	}
	return artifact.WithDerivedPromotion(), string(html), nil
}

func (r *Repo) ListRichArtifacts(filter RichArtifactFilter) ([]RichArtifact, error) {
	r.mu.Lock()
	dir := filepath.Join(r.root, richArtifactRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		r.mu.Unlock()
		if os.IsNotExist(err) {
			return []RichArtifact{}, nil
		}
		return nil, fmt.Errorf("visual artifact: read registry: %w", err)
	}
	metaPaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		metaPaths = append(metaPaths, filepath.Join(dir, entry.Name()))
	}
	r.mu.Unlock()

	out := make([]RichArtifact, 0, len(metaPaths))
	for _, metaPath := range metaPaths {
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var artifact RichArtifact
		if err := json.Unmarshal(raw, &artifact); err != nil {
			continue
		}
		if !richArtifactMatchesFilter(artifact, filter) {
			continue
		}
		out = append(out, artifact.WithDerivedPromotion())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

func richArtifactMatchesFilter(artifact RichArtifact, filter RichArtifactFilter) bool {
	if filter.CreatedBy != "" && artifact.CreatedBy != filter.CreatedBy {
		return false
	}
	if filter.SourceMarkdownPath != "" && artifact.SourceMarkdownPath != filter.SourceMarkdownPath {
		return false
	}
	if filter.PromotedWikiPath != "" && artifact.PromotedWikiPath != filter.PromotedWikiPath {
		return false
	}
	return true
}

func (r *Repo) cachedDiffEmptyLocked(ctx context.Context, slug string) (bool, error) {
	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return false, fmt.Errorf("visual artifact: git diff --cached: %w", err)
	}
	return strings.TrimSpace(cachedDiff) == "", nil
}

func (r *Repo) currentHeadLocked(ctx context.Context) (string, error) {
	headSha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("visual artifact: resolve HEAD: %w", err)
	}
	return strings.TrimSpace(headSha), nil
}

func (w *WikiWorker) CreateRichArtifact(ctx context.Context, artifact RichArtifact, html, commitMsg string) (RichArtifact, string, int, error) {
	if w == nil || w.repo == nil || !w.running.Load() {
		return RichArtifact{}, "", 0, ErrWorkerStopped
	}
	waitCtx, cancel := context.WithTimeout(ctx, wikiWriteTimeout)
	defer cancel()
	req := wikiWriteRequest{
		Context:        waitCtx,
		Slug:           artifact.CreatedBy,
		IsRichArtifact: true,
		RichArtifact: wikiRichArtifactWork{
			Artifact: artifact,
			HTML:     html,
		},
		CommitMsg: commitMsg,
		ReplyCh:   make(chan wikiWriteResult, 1),
	}
	select {
	case w.requests <- req:
	default:
		return RichArtifact{}, "", 0, ErrQueueSaturated
	}
	select {
	case result := <-req.ReplyCh:
		// processRichArtifactRequest stamps the (possibly updated) artifact
		// onto the result envelope — propagate it so the broker handler can
		// return AttachedToNotebookEntry to the caller without a re-read.
		return result.RichArtifact, result.SHA, result.BytesWritten, result.Err
	case <-waitCtx.Done():
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: write timed out after %s; operation may still complete", wikiWriteTimeout)
	}
}

func (w *WikiWorker) PromoteRichArtifact(ctx context.Context, actorSlug, id, targetPath, markdown, mode, commitMsg string) (RichArtifact, string, int, error) {
	if w == nil || w.repo == nil || !w.running.Load() {
		return RichArtifact{}, "", 0, ErrWorkerStopped
	}
	waitCtx, cancel := context.WithTimeout(ctx, wikiWriteTimeout)
	defer cancel()
	req := wikiWriteRequest{
		Context:                 waitCtx,
		Slug:                    actorSlug,
		Path:                    targetPath,
		IsRichArtifactPromotion: true,
		RichArtifact: wikiRichArtifactWork{
			ID:       id,
			Markdown: markdown,
			Now:      time.Now().UTC(),
		},
		Mode:      mode,
		CommitMsg: commitMsg,
		ReplyCh:   make(chan wikiWriteResult, 1),
	}
	select {
	case w.requests <- req:
	default:
		return RichArtifact{}, "", 0, ErrQueueSaturated
	}
	select {
	case result := <-req.ReplyCh:
		return result.RichArtifact, result.SHA, result.BytesWritten, result.Err
	case <-waitCtx.Done():
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: promote timed out after %s; operation may still complete", wikiWriteTimeout)
	}
}

func (w *WikiWorker) RichArtifact(id string) (RichArtifact, string, error) {
	if w == nil || w.repo == nil {
		return RichArtifact{}, "", ErrWorkerStopped
	}
	return w.repo.RichArtifact(id)
}

func (w *WikiWorker) ListRichArtifacts(filter RichArtifactFilter) ([]RichArtifact, error) {
	if w == nil || w.repo == nil {
		return nil, ErrWorkerStopped
	}
	return w.repo.ListRichArtifacts(filter)
}

func (w *WikiWorker) processRichArtifactRequest(ctx context.Context, req wikiWriteRequest) (RichArtifact, string, int, error) {
	if err := ctx.Err(); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: request cancelled before write: %w", err)
	}
	if req.IsRichArtifact {
		return w.repo.CommitRichArtifact(ctx, req.Slug, req.RichArtifact.Artifact, req.RichArtifact.HTML, req.CommitMsg)
	}
	return w.repo.PromoteRichArtifact(ctx, req.Slug, req.RichArtifact.ID, req.Path, req.RichArtifact.Markdown, req.Mode, req.CommitMsg, req.RichArtifact.Now)
}
