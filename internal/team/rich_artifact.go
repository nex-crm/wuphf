package team

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	richArtifactKindNotebookHTML = "notebook_html"
	richArtifactKindWikiVisual   = "wiki_visual"
	richArtifactRepresentation   = "html"
	richArtifactTrustDraft       = "draft"
	richArtifactTrustPromoted    = "promoted"
	richArtifactSanitizerVersion = "sandbox-v1"
	richArtifactRoot             = "wiki/visual-artifacts"
	richArtifactMaxHTMLBytes     = 1024 * 1024
)

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
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
	ContentHash        string   `json:"contentHash"`
	SanitizerVersion   string   `json:"sanitizerVersion"`
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
		return RichArtifact{}, "", err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return RichArtifact{}, "", fmt.Errorf("visual artifact: title is required")
	}
	summary := strings.TrimSpace(req.Summary)
	html := req.HTML
	if err := validateRichArtifactHTML(html); err != nil {
		return RichArtifact{}, "", err
	}
	sourcePath := strings.TrimSpace(req.SourceMarkdownPath)
	if sourcePath != "" {
		if err := validateNotebookWritePath(slug, sourcePath); err != nil {
			return RichArtifact{}, "", err
		}
	}
	createdAt := now.UTC().Format(time.RFC3339)
	contentHash := richArtifactContentHash(html)
	id := richArtifactID(slug, title, html, createdAt)
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
		RelatedReceiptIDs:  cleanRichArtifactStringList(req.RelatedReceiptIDs),
		CreatedBy:          slug,
		CreatedAt:          createdAt,
		UpdatedAt:          createdAt,
		ContentHash:        contentHash,
		SanitizerVersion:   richArtifactSanitizerVersion,
	}
	return artifact, html, nil
}

func richArtifactID(slug, title, html, createdAt string) string {
	sum := sha256.Sum256([]byte(slug + "\x00" + title + "\x00" + createdAt + "\x00" + html))
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
		return fmt.Errorf("visual artifact: invalid id %q", id)
	}
	for _, ch := range strings.TrimPrefix(id, "ra_") {
		if !((ch >= 'a' && ch <= 'f') || (ch >= '0' && ch <= '9')) {
			return fmt.Errorf("visual artifact: invalid id %q", id)
		}
	}
	return nil
}

func validateRichArtifactHTML(html string) error {
	if strings.TrimSpace(html) == "" {
		return fmt.Errorf("visual artifact: html is required")
	}
	if len([]byte(html)) > richArtifactMaxHTMLBytes {
		return fmt.Errorf("visual artifact: html exceeds %d bytes", richArtifactMaxHTMLBytes)
	}
	if strings.ContainsRune(html, '\x00') {
		return fmt.Errorf("visual artifact: html must not contain NUL bytes")
	}
	return nil
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

func (r *Repo) CommitRichArtifact(ctx context.Context, slug string, artifact RichArtifact, html, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.TrimSpace(slug) == "" {
		return "", 0, fmt.Errorf("visual artifact: author slug is required")
	}
	if err := validateRichArtifactForWrite(artifact, html); err != nil {
		return "", 0, err
	}
	metaPath := richArtifactMetaPath(artifact.ID)
	if artifact.HTMLPath != richArtifactHTMLPath(artifact.ID) {
		return "", 0, fmt.Errorf("visual artifact: htmlPath must be %q", richArtifactHTMLPath(artifact.ID))
	}
	metaBytes, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", 0, fmt.Errorf("visual artifact: marshal metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')
	if err := os.MkdirAll(filepath.Join(r.root, richArtifactRoot), 0o700); err != nil {
		return "", 0, fmt.Errorf("visual artifact: mkdir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.root, artifact.HTMLPath), []byte(html), 0o600); err != nil {
		return "", 0, fmt.Errorf("visual artifact: write html: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.root, metaPath), metaBytes, 0o600); err != nil {
		return "", 0, fmt.Errorf("visual artifact: write metadata: %w", err)
	}
	if out, err := r.runGitLocked(ctx, slug, "add", "--", artifact.HTMLPath, metaPath); err != nil {
		return "", 0, fmt.Errorf("visual artifact: git add: %w: %s", err, out)
	}
	if empty, err := r.cachedDiffEmptyLocked(ctx, slug); err != nil {
		return "", 0, err
	} else if empty {
		head, err := r.currentHeadLocked(ctx)
		return head, len(html) + len(metaBytes), err
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("artifact: create visual artifact %s", artifact.ID)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("visual artifact: git commit: %w: %s", err, out)
	}
	sha, err := r.currentHeadLocked(ctx)
	return sha, len(html) + len(metaBytes), err
}

func validateRichArtifactForWrite(artifact RichArtifact, html string) error {
	if err := validateRichArtifactID(artifact.ID); err != nil {
		return err
	}
	if artifact.Kind != richArtifactKindNotebookHTML && artifact.Kind != richArtifactKindWikiVisual {
		return fmt.Errorf("visual artifact: unsupported kind %q", artifact.Kind)
	}
	if strings.TrimSpace(artifact.Title) == "" {
		return fmt.Errorf("visual artifact: title is required")
	}
	if artifact.Representation != richArtifactRepresentation {
		return fmt.Errorf("visual artifact: unsupported representation %q", artifact.Representation)
	}
	if artifact.TrustLevel != richArtifactTrustDraft && artifact.TrustLevel != richArtifactTrustPromoted {
		return fmt.Errorf("visual artifact: unsupported trust level %q", artifact.TrustLevel)
	}
	if artifact.CreatedBy == "" {
		return fmt.Errorf("visual artifact: createdBy is required")
	}
	if artifact.CreatedAt == "" || artifact.UpdatedAt == "" {
		return fmt.Errorf("visual artifact: timestamps are required")
	}
	if artifact.ContentHash != richArtifactContentHash(html) {
		return fmt.Errorf("visual artifact: content hash mismatch")
	}
	if artifact.SanitizerVersion != richArtifactSanitizerVersion {
		return fmt.Errorf("visual artifact: unsupported sanitizer version %q", artifact.SanitizerVersion)
	}
	return validateRichArtifactHTML(html)
}

func (r *Repo) PromoteRichArtifact(ctx context.Context, actorSlug, id, targetPath, markdown, mode, message string, now time.Time) (RichArtifact, string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	actorSlug = strings.TrimSpace(actorSlug)
	if actorSlug == "" {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: actor slug is required")
	}
	if err := validateRichArtifactID(id); err != nil {
		return RichArtifact{}, "", 0, err
	}
	if err := validateArticlePath(targetPath); err != nil {
		return RichArtifact{}, "", 0, err
	}
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: markdown_summary is required")
	}
	if mode == "" {
		mode = "create"
	}
	fullTarget := filepath.Join(r.root, filepath.FromSlash(targetPath))
	switch mode {
	case "create":
		if _, err := os.Stat(fullTarget); err == nil {
			return RichArtifact{}, "", 0, fmt.Errorf("wiki: article already exists at %q; use replace or append_section", targetPath)
		}
	case "replace":
	case "append_section":
	default:
		return RichArtifact{}, "", 0, fmt.Errorf("wiki: unknown write mode %q; expected create|replace|append_section", mode)
	}
	artifact, html, err := r.readRichArtifactLocked(id)
	if err != nil {
		return RichArtifact{}, "", 0, err
	}
	artifact.Kind = richArtifactKindWikiVisual
	artifact.TrustLevel = richArtifactTrustPromoted
	artifact.PromotedWikiPath = targetPath
	artifact.UpdatedAt = now.UTC().Format(time.RFC3339)
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
		return artifact, head, bytesWritten + len(metaBytes) + len(html), err
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("wiki: promote visual artifact %s", id)
	}
	if out, err := r.runGitLocked(ctx, actorSlug, "commit", "-q", "-m", commitMsg); err != nil {
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: git commit promotion: %w: %s", err, out)
	}
	sha, err := r.currentHeadLocked(ctx)
	return artifact, sha, bytesWritten + len(metaBytes) + len(html), err
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
	if artifact.ContentHash != richArtifactContentHash(string(html)) {
		return RichArtifact{}, "", fmt.Errorf("visual artifact: content hash mismatch")
	}
	return artifact, string(html), nil
}

func (r *Repo) ListRichArtifacts(filter RichArtifactFilter) ([]RichArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := filepath.Join(r.root, richArtifactRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []RichArtifact{}, nil
		}
		return nil, fmt.Errorf("visual artifact: read registry: %w", err)
	}
	out := make([]RichArtifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
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
		out = append(out, artifact)
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

func (w *WikiWorker) CreateRichArtifact(ctx context.Context, artifact RichArtifact, html, commitMsg string) (string, int, error) {
	if w == nil || w.repo == nil || !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	req := wikiWriteRequest{
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
		return "", 0, ErrQueueSaturated
	}
	waitCtx, cancel := context.WithTimeout(ctx, wikiWriteTimeout)
	defer cancel()
	select {
	case result := <-req.ReplyCh:
		return result.SHA, result.BytesWritten, result.Err
	case <-waitCtx.Done():
		return "", 0, fmt.Errorf("visual artifact: write timed out after %s", wikiWriteTimeout)
	}
}

func (w *WikiWorker) PromoteRichArtifact(ctx context.Context, actorSlug, id, targetPath, markdown, mode, commitMsg string) (RichArtifact, string, int, error) {
	if w == nil || w.repo == nil || !w.running.Load() {
		return RichArtifact{}, "", 0, ErrWorkerStopped
	}
	req := wikiWriteRequest{
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
	waitCtx, cancel := context.WithTimeout(ctx, wikiWriteTimeout)
	defer cancel()
	select {
	case result := <-req.ReplyCh:
		return result.RichArtifact, result.SHA, result.BytesWritten, result.Err
	case <-waitCtx.Done():
		return RichArtifact{}, "", 0, fmt.Errorf("visual artifact: promote timed out after %s", wikiWriteTimeout)
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
	if req.IsRichArtifact {
		sha, n, err := w.repo.CommitRichArtifact(ctx, req.Slug, req.RichArtifact.Artifact, req.RichArtifact.HTML, req.CommitMsg)
		return req.RichArtifact.Artifact, sha, n, err
	}
	return w.repo.PromoteRichArtifact(ctx, req.Slug, req.RichArtifact.ID, req.Path, req.RichArtifact.Markdown, req.Mode, req.CommitMsg, req.RichArtifact.Now)
}
