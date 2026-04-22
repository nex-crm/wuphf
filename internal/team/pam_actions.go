package team

// pam_actions.go is the extensible action registry for Pam the Archivist.
// Actions are the discrete jobs Pam can run from her desk on the wiki — each
// one ships with a prompt template, a set of allowed tools, and a commit
// message pattern. New actions are added by appending to pamActions below.
//
// The registry is intentionally tiny. It exists because Pam will soon have
// more jobs (e.g. "summarize this article", "cross-link to related briefs",
// "generate cover image"), and we want those additions to be a one-file edit
// rather than touching the dispatcher or handler.

import (
	"errors"
	"fmt"
	"strings"
)

// PamActionID is a typed string so callers can't pass arbitrary action names.
type PamActionID string

const (
	// PamActionEnrichArticle: pull fresh info + media from the web and fold it
	// into the article body. First action shipped — v1 of Pam's desk menu.
	PamActionEnrichArticle PamActionID = "enrich_article"
)

// PamAction describes a single job Pam can run. The prompt template is a
// plain fmt.Sprintf template: %s is replaced with the article body.
type PamAction struct {
	ID             PamActionID
	Label          string   // human-facing label for the desk menu
	SystemPrompt   string   // locked system prompt — do not edit without review
	UserPromptTmpl string   // fmt pattern; takes article body
	AllowedTools   []string // informational; wired into the sub-process when we gain tool-config plumbing
	CommitMsgTmpl  string   // fmt pattern; takes article path
}

// ErrUnknownPamAction is returned by LookupPamAction when the id is not
// registered. Callers surface this as a 400.
var ErrUnknownPamAction = errors.New("pam: unknown action")

// pamActions is the source of truth. Ordering here is the ordering rendered
// in the desk menu.
var pamActions = []PamAction{
	{
		ID:    PamActionEnrichArticle,
		Label: "Enrich this article with web data",
		SystemPrompt: `You are Pam, the wiki archivist. You enrich articles by pulling in accurate, up-to-date information and media from the public web. Rules you MUST follow:
1. Preserve the existing frontmatter (the leading --- block) exactly as given. Do not add, remove, or reorder keys.
2. Keep the author's voice and structure. Do not rewrite paragraphs that are already correct.
3. Weave new facts into the body where they belong. Cite the source URL inline as a markdown link the first time you use it.
4. When you add an image, embed it as standard markdown (![alt](url)). Prefer primary sources (company press pages, official sites, Wikipedia Commons). Never embed an image you did not find on the web.
5. Do not invent facts. If the web search turns up nothing new, return the article unchanged and say so in one sentence at the very top as an HTML comment: <!-- pam: no new info found -->.
6. Output ONLY the full updated markdown. No commentary, no code fences.`,
		UserPromptTmpl: `# Existing article

%s

# Your task

Use web search and web fetch to find new, reliable information and relevant media for this article. Update the body with what you find. Output the full updated markdown.`,
		AllowedTools:  []string{"WebSearch", "WebFetch", "Read"},
		CommitMsgTmpl: "archivist: enrich %s with web data",
	},
}

// LookupPamAction returns the registered action for id, or ErrUnknownPamAction.
func LookupPamAction(id PamActionID) (PamAction, error) {
	for _, a := range pamActions {
		if a.ID == id {
			return a, nil
		}
	}
	return PamAction{}, fmt.Errorf("%w: %q", ErrUnknownPamAction, id)
}

// PamActions returns the registry in menu order. The returned slice is a
// copy so callers can't mutate the registry.
func PamActions() []PamAction {
	out := make([]PamAction, len(pamActions))
	copy(out, pamActions)
	return out
}

// renderCommitMsg fills the commit-message template. Keeps the format
// surface in one place so every commit reads "archivist: <verb> <path>".
func (a PamAction) renderCommitMsg(articlePath string) string {
	tmpl := strings.TrimSpace(a.CommitMsgTmpl)
	if tmpl == "" {
		return fmt.Sprintf("archivist: %s on %s", a.ID, articlePath)
	}
	return fmt.Sprintf(tmpl, articlePath)
}

// renderUserPrompt fills the user-prompt template with the article body.
func (a PamAction) renderUserPrompt(articleBody string) string {
	tmpl := a.UserPromptTmpl
	if !strings.Contains(tmpl, "%s") {
		return tmpl + "\n\n" + articleBody
	}
	return fmt.Sprintf(tmpl, articleBody)
}
