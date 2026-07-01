package team

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

// broker_apps_knowledge.go — the app's KNOWLEDGE tab, made real via gbrain.
//
// The Knowledge tab is a Wikipedia-style reader where every claim carries a
// citation back to a real source, with an "explain why this source" note. We do
// NOT fabricate that: we SYNTHESIZE cited pages from the app's REAL artifacts —
// its spec, its data model (the DB primitive), its source, the office roster,
// and the workspace brain (wiki) — via one grounded LLM pass. Each [[n]] maps to
// a real source in the pack, and the "why" explains why that source backs the
// claim. The result is cached to <app_dir>/knowledge.json so the tab is fast and
// stable after first synthesis; ?refresh=1 rebuilds it.
//
//	GET /apps/{id}/knowledge          -> { pages: KnowledgePage[] } (cached or synth)
//	GET /apps/{id}/knowledge?refresh=1 -> re-synthesize, then serve

const customAppKnowledgeFile = "knowledge.json"

// knowledgeSynthTimeout bounds one knowledge synthesis (several cited pages is a
// larger completion than a single ai() call).
const knowledgeSynthTimeout = 90 * time.Second

// The closed set of source kinds the reader renders (matches the FE
// KnowledgeSourceKind). An unknown kind normalizes to "document".
var knowledgeKinds = map[string]bool{
	"chat":     true,
	"document": true,
	"crm":      true,
	"decision": true,
	"roster":   true,
}

// ── Wire shape (mirrors web/src/operator/mock/data.ts KnowledgePage) ──────────

type appKnowledgeRef struct {
	N       int    `json:"n"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	Kind    string `json:"kind"`
	Snippet string `json:"snippet"`
	Why     string `json:"why"`
}

type appKnowledgeSection struct {
	Heading string   `json:"heading,omitempty"`
	Paras   []string `json:"paras"`
}

type appKnowledgeInfoRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type appKnowledgePage struct {
	ID         string                `json:"id"`
	Title      string                `json:"title"`
	Category   string                `json:"category"`
	UpdatedAt  string                `json:"updatedAt"`
	Summary    string                `json:"summary"`
	Infobox    []appKnowledgeInfoRow `json:"infobox"`
	Lead       string                `json:"lead"`
	Sections   []appKnowledgeSection `json:"sections"`
	References []appKnowledgeRef     `json:"references"`
	Categories []string              `json:"categories"`
	SeeAlso    []string              `json:"seeAlso"`
}

// knowledgeSource is one real artifact the synthesis may cite. N is its citation
// number in the pack; Snippet is the real excerpt the model draws from.
type knowledgeSource struct {
	N       int
	Kind    string
	Title   string
	Detail  string
	Snippet string
}

// ── HTTP ─────────────────────────────────────────────────────────────────────

func (b *Broker) handleAppKnowledge(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store := b.appStore()
	if _, _, err := store.Get(id); err != nil {
		writeAppError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), knowledgeSynthTimeout)
	defer cancel()

	refresh := r.URL.Query().Get("refresh") == "1"
	gbClient, gbBacked := b.gbrainKnowledgeClient()

	// Read the cache first — gbrain IS the store when it backs the workspace;
	// otherwise the per-app knowledge.json file.
	if !refresh {
		if gbBacked {
			if pages, err := b.readAppKnowledgeFromGBrain(ctx, gbClient, id); err == nil && len(pages) > 0 {
				writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
				return
			}
		} else if pages, ok, err := store.ReadAppKnowledge(id); err == nil && ok {
			writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
			return
		}
	}

	// Budget the synthesis per-app like ai() — it is an LLM completion.
	if _, limited := b.consumeAppAIBudget(appBudgetKey(id, r)); limited {
		writeJSON(w, http.StatusOK, map[string]any{"pages": []appKnowledgePage{}, "error": "rate_limited"})
		return
	}

	pages, err := b.synthesizeAppKnowledge(ctx, id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "broker: app knowledge synth failed: %v\n", err)
		// Expected product state (no provider / empty brain): let the FE render a
		// graceful "no knowledge yet" rather than an error toast.
		writeJSON(w, http.StatusOK, map[string]any{"pages": []appKnowledgePage{}, "error": "ai_unavailable"})
		return
	}

	// Persist: write pages INTO gbrain (the shared brain) when it backs the
	// workspace, so they are durable, searchable, linked, and reusable across
	// apps; otherwise fall back to the per-app file cache.
	if gbBacked {
		if err := b.writeAppKnowledgeToGBrain(ctx, gbClient, id, pages); err != nil {
			fmt.Fprintf(os.Stderr, "broker: app knowledge gbrain write failed: %v\n", err)
		}
	} else if err := store.WriteAppKnowledge(id, pages); err != nil {
		fmt.Fprintf(os.Stderr, "broker: app knowledge cache write failed: %v\n", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

// ── gbrain-backed store: knowledge pages live in the shared brain ─────────────

const (
	// appKnowledgeTag marks every app-knowledge page in the brain.
	appKnowledgeTag = "wuphf-app-knowledge"
	// appKnowledgeSourceKind / appKnowledgeIngestedVia are the gbrain provenance
	// stamps for pages this surface writes.
	appKnowledgeSourceKind  = "wuphf_app_knowledge"
	appKnowledgeIngestedVia = "wuphf-app-knowledge"
)

// knowledgeB64Re extracts the exact structured page from a gbrain page body. We
// keep the machine-readable page as base64 in an HTML comment (opaque, preserved
// verbatim) so the cited/explain-why render reconstructs faithfully, while the
// visible body stays readable markdown that gbrain search + embeddings index.
var knowledgeB64Re = regexp.MustCompile(`<!--wuphf-knowledge-b64:([A-Za-z0-9+/=]+)-->`)

// gbrainKnowledgeClient returns the brain client when gbrain backs the workspace
// (WikiWorker nil ⟺ gbrain-backed) and a client exists. Otherwise the file cache
// is used and this reports (nil, false).
func (b *Broker) gbrainKnowledgeClient() (*gbrain.Client, bool) {
	if b.WikiWorker() != nil {
		return nil, false
	}
	b.mu.Lock()
	c := b.gbrainClient
	b.mu.Unlock()
	if c == nil {
		return nil, false
	}
	return c, true
}

// appKnowledgeScopeTag is the per-app tag that scopes a knowledge page to an app.
// A page tagged with several of these belongs to several apps (cross-app share).
func appKnowledgeScopeTag(appID string) string {
	return "wuphf-app-" + strings.TrimPrefix(appID, "app_")
}

// appKnowledgeSlug is the stable brain slug for one app's knowledge page.
func appKnowledgeSlug(appID, pageID string) string {
	return "k-" + strings.TrimPrefix(appID, "app_") + "-" + pageID
}

func stripCites(s string) string {
	return strings.TrimSpace(knowledgeCiteRe.ReplaceAllString(s, ""))
}

// renderKnowledgePageForGBrain builds the gbrain page: YAML frontmatter (title,
// type, app-scope tags), a readable markdown body (indexed by gbrain), and the
// exact structured page as base64 for faithful reconstruction.
func renderKnowledgePageForGBrain(appID string, page appKnowledgePage) (string, error) {
	raw, err := json.Marshal(page)
	if err != nil {
		return "", fmt.Errorf("marshal knowledge page: %w", err)
	}
	fm := map[string]any{
		"title": page.Title,
		"type":  appKnowledgeTag,
		"tags":  []string{appKnowledgeScopeTag(appID), appKnowledgeTag},
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return "", fmt.Errorf("yaml encode: %w", err)
	}
	_ = enc.Close()
	buf.WriteString("---\n\n")
	buf.WriteString("# ")
	buf.WriteString(page.Title)
	buf.WriteString("\n\n")
	if lead := stripCites(page.Lead); lead != "" {
		buf.WriteString(lead)
		buf.WriteString("\n\n")
	}
	for _, s := range page.Sections {
		if h := strings.TrimSpace(s.Heading); h != "" {
			buf.WriteString("## ")
			buf.WriteString(h)
			buf.WriteString("\n\n")
		}
		for _, p := range s.Paras {
			if para := stripCites(p); para != "" {
				buf.WriteString(para)
				buf.WriteString("\n\n")
			}
		}
	}
	buf.WriteString("<!--wuphf-knowledge-b64:")
	buf.WriteString(base64.StdEncoding.EncodeToString(raw))
	buf.WriteString("-->\n")
	return buf.String(), nil
}

// writeAppKnowledgeToGBrain upserts each page into the brain (tagged for the app)
// and cross-links pages via the brain's link graph (seeAlso becomes real edges).
func (b *Broker) writeAppKnowledgeToGBrain(ctx context.Context, client *gbrain.Client, appID string, pages []appKnowledgePage) error {
	slugByPageID := make(map[string]string, len(pages))
	for _, p := range pages {
		content, err := renderKnowledgePageForGBrain(appID, p)
		if err != nil {
			return err
		}
		slug := appKnowledgeSlug(appID, p.ID)
		if _, err := client.PutPage(ctx, content, gbrain.PutOptions{
			Slug:        slug,
			SourceKind:  appKnowledgeSourceKind,
			IngestedVia: appKnowledgeIngestedVia,
		}); err != nil {
			return fmt.Errorf("put page %q: %w", slug, err)
		}
		slugByPageID[p.ID] = slug
	}
	// Cross-link seeAlso → real graph edges (best-effort; a failed edge is not
	// fatal to persistence).
	for _, p := range pages {
		from := slugByPageID[p.ID]
		for _, target := range p.SeeAlso {
			to := slugByPageID[target]
			if to == "" || to == from {
				continue
			}
			_ = client.AddLink(ctx, from, to, "related", appKnowledgeIngestedVia, "knowledge see-also")
		}
	}
	return nil
}

// readAppKnowledgeFromGBrain reads the app's pages back from the brain: list by
// the app-scope tag, decode each page's structured form, and merge the brain's
// link graph into seeAlso so cross-links (including future cross-app ones)
// surface in the reader.
func (b *Broker) readAppKnowledgeFromGBrain(ctx context.Context, client *gbrain.Client, appID string) ([]appKnowledgePage, error) {
	metas, err := client.ListPages(ctx, gbrain.ListOptions{Tag: appKnowledgeScopeTag(appID), Limit: 50})
	if err != nil {
		return nil, err
	}
	pages := make([]appKnowledgePage, 0, len(metas))
	pageIDBySlug := make(map[string]string, len(metas))
	for _, m := range metas {
		page, err := client.GetPage(ctx, m.Slug)
		if err != nil {
			continue
		}
		kp, ok := decodeKnowledgePageFromBody(page.Content)
		if !ok {
			continue
		}
		pages = append(pages, kp)
		pageIDBySlug[m.Slug] = kp.ID
	}
	for i := range pages {
		slug := appKnowledgeSlug(appID, pages[i].ID)
		links, err := client.GetLinks(ctx, slug)
		if err != nil {
			continue
		}
		seen := make(map[string]bool, len(pages[i].SeeAlso))
		for _, s := range pages[i].SeeAlso {
			seen[s] = true
		}
		for _, l := range links {
			if pid := pageIDBySlug[l.To]; pid != "" && pid != pages[i].ID && !seen[pid] {
				pages[i].SeeAlso = append(pages[i].SeeAlso, pid)
				seen[pid] = true
			}
		}
	}
	return pages, nil
}

func decodeKnowledgePageFromBody(body string) (appKnowledgePage, bool) {
	m := knowledgeB64Re.FindStringSubmatch(body)
	if len(m) < 2 {
		return appKnowledgePage{}, false
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return appKnowledgePage{}, false
	}
	var p appKnowledgePage
	if err := json.Unmarshal(raw, &p); err != nil {
		return appKnowledgePage{}, false
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Title) == "" {
		return appKnowledgePage{}, false
	}
	return p, true
}

// ── Synthesis ────────────────────────────────────────────────────────────────

func (b *Broker) synthesizeAppKnowledge(ctx context.Context, id string) ([]appKnowledgePage, error) {
	sources := b.gatherKnowledgeSources(id)
	if len(sources) == 0 {
		return nil, fmt.Errorf("no sources to synthesize from")
	}
	system, user := buildKnowledgePrompt(sources)
	out, err := currentAppsLLMCompleter()(ctx, system, user)
	if err != nil {
		return nil, err
	}
	raw, ok := extractFirstJSON(out)
	if !ok {
		return nil, fmt.Errorf("synthesis returned no JSON")
	}
	var parsed struct {
		Pages []appKnowledgePage `json:"pages"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode synthesis: %w", err)
	}
	// Empty is a VALID outcome: "if there is nothing worth writing, don't write
	// it." Only a provider/parse failure (above) is an error. An empty result is
	// cached like any other, so we don't re-synthesize a genuinely-empty app.
	return sanitizeKnowledgePages(parsed.Pages, sources), nil
}

// gatherKnowledgeSources builds the real source pack for USER-FACING knowledge.
// Knowledge is operating context that helps someone USE the app — the data
// source, unit/display preferences, domain rules, notable choices, limitations —
// NOT how the app is built. So the source is the app's BRIEF (what it is for and
// the choices in it), never its code, data model, or the office roster. Only a
// real artifact; nothing invented.
func (b *Broker) gatherKnowledgeSources(id string) []knowledgeSource {
	store := b.appStore()
	var sources []knowledgeSource
	add := func(kind, title, detail, snippet string) {
		snippet = strings.TrimSpace(snippet)
		if snippet == "" {
			return
		}
		sources = append(sources, knowledgeSource{
			N:       len(sources) + 1,
			Kind:    kind,
			Title:   title,
			Detail:  detail,
			Snippet: clampRunes(snippet, 1200),
		})
	}

	if app, _, err := store.Get(id); err == nil {
		brief := strings.TrimSpace(strings.Join([]string{app.Summary, app.Description}, "\n"))
		add("document", fmt.Sprintf("%s — app brief", app.Name), "What this app is for and the choices in it", brief)
	}

	// Deliberately NOT sourcing the code, data model, or roster: those describe
	// how the app was built, which is not knowledge the user needs to operate it.
	return sources
}

func buildKnowledgePrompt(sources []knowledgeSource) (system, user string) {
	system = strings.TrimSpace(`
You maintain a short KNOWLEDGE BASE that helps a user OPERATE an app. It captures only context that is genuinely useful for USING the app day to day — never how the app was built.

INCLUDE, only when the sources support it:
- the data source the app draws on,
- display or unit preferences (for example Celsius vs Fahrenheit),
- domain rules or special cases the user needs to be aware of,
- notable choices about the app's scope or behaviour,
- limitations the user should know about.

NEVER include how the app is built: no architecture, no data model, no database, no code, no frameworks, no libraries, no implementation details. The user does not care how it was built. If a source describes implementation, ignore that part and use only the user-facing meaning.

Only write facts that are genuinely useful as operating context for THIS app. Quality over volume. If nothing in the sources is worth writing as user-facing knowledge, return exactly {"pages":[]}. Do not pad.

Prefer few, high-signal pages. Usually ONE page is right; split a topic onto its own page only when it is genuinely distinct and substantial enough to stand alone. When you write more than one page, CONNECT them: list each related page in the other's "seeAlso" (using its id), and when a page mentions a concept that has its own page, phrase it so the reader knows to look there. A knowledge base is a web of linked pages, not islands.

Every claim MUST carry a [[n]] citation to the source that supports it. Never invent facts, sources, numbers, or names. Output ONLY strict JSON of the schema below — no prose outside the JSON, no markdown fences.

Schema:
{"pages":[{
  "id": "kebab-case-id",
  "title": "Page Title",
  "category": "one short category",
  "summary": "one sentence",
  "infobox": [{"label":"Label","value":"Value"}],
  "lead": "Intro paragraph with [[n]] citations.",
  "sections": [{"heading":"Section heading","paras":["Paragraph with [[n]]."]}],
  "references": [{"n":1,"title":"<source title>","detail":"<where/what>","kind":"<source kind>","snippet":"<short real excerpt from that source>","why":"why this source backs the claim it is cited for"}],
  "categories": ["Category"],
  "seeAlso": ["other-page-id"]
}]}

For each reference: n and kind and title MUST match the SOURCE you cite; snippet MUST be a short excerpt drawn from that source's text; why is one sentence explaining why it supports the claim. Only list references you actually cite with [[n]]. seeAlso may only reference ids of pages you produce.`)

	var sb strings.Builder
	sb.WriteString("SOURCES (cite by number):\n")
	for _, s := range sources {
		sb.WriteString(fmt.Sprintf("[%d] (%s) %s — %s\n    \"%s\"\n", s.N, s.Kind, s.Title, s.Detail, s.Snippet))
	}
	sb.WriteString("\nWrite the user-facing knowledge base for this app — only the operating context worth knowing, grounded ONLY in the sources above. If there is nothing worth writing, return {\"pages\":[]}. Return the JSON now.")
	return system, sb.String()
}

// ── Sanitize / validate the model output ─────────────────────────────────────

var knowledgeCiteRe = regexp.MustCompile(`\[\[(\d+)\]\]`)

// sanitizeKnowledgePages enforces the contract on model output: real kinds,
// citations that map to sources actually cited in the prose, contiguous
// reference numbering, and seeAlso that only points at produced pages. It drops
// pages with no usable content.
func sanitizeKnowledgePages(pages []appKnowledgePage, sources []knowledgeSource) []appKnowledgePage {
	if len(pages) > 3 {
		pages = pages[:3]
	}
	sourceByN := make(map[int]knowledgeSource, len(sources))
	for _, s := range sources {
		sourceByN[s.N] = s
	}

	// First pass: keep valid pages, collect their ids for seeAlso validation.
	out := make([]appKnowledgePage, 0, len(pages))
	ids := make(map[string]bool)
	for _, p := range pages {
		p.Title = strings.TrimSpace(p.Title)
		if p.Title == "" {
			continue
		}
		if strings.TrimSpace(p.ID) == "" {
			p.ID = slugifyKnowledgeID(p.Title)
		}
		if ids[p.ID] {
			continue // drop duplicate id
		}
		p.UpdatedAt = "Synthesized from your workspace by your AI"
		p.References = sanitizeRefs(p.References, sourceByN)
		if len(p.References) == 0 {
			// A page with no grounded citation is exactly what we refuse to ship.
			continue
		}
		ids[p.ID] = true
		out = append(out, p)
	}

	// Second pass: drop seeAlso ids that do not resolve to a produced page.
	for i := range out {
		kept := out[i].SeeAlso[:0]
		for _, id := range out[i].SeeAlso {
			if ids[id] && id != out[i].ID {
				kept = append(kept, id)
			}
		}
		out[i].SeeAlso = kept
		out[i].Categories = trimStrings(out[i].Categories)
	}
	return out
}

// sanitizeRefs keeps only references that correspond to a real source, normalizes
// the kind, and fills a missing title/kind from the source pack.
func sanitizeRefs(refs []appKnowledgeRef, sourceByN map[int]knowledgeSource) []appKnowledgeRef {
	out := make([]appKnowledgeRef, 0, len(refs))
	seen := make(map[int]bool)
	for _, ref := range refs {
		src, ok := sourceByN[ref.N]
		if !ok || seen[ref.N] {
			continue // cites a source that does not exist, or a duplicate
		}
		seen[ref.N] = true
		if strings.TrimSpace(ref.Title) == "" {
			ref.Title = src.Title
		}
		if strings.TrimSpace(ref.Detail) == "" {
			ref.Detail = src.Detail
		}
		if !knowledgeKinds[ref.Kind] {
			ref.Kind = src.Kind
			if !knowledgeKinds[ref.Kind] {
				ref.Kind = "document"
			}
		}
		if strings.TrimSpace(ref.Snippet) == "" {
			ref.Snippet = clampRunes(src.Snippet, 240)
		}
		out = append(out, ref)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out
}

func slugifyKnowledgeID(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "page"
	}
	return clampRunes(s, 48)
}

func trimStrings(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func clampRunes(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// ── Per-app knowledge cache (knowledge.json under the app dir) ────────────────

func (s *customAppStore) ReadAppKnowledge(id string) ([]appKnowledgePage, bool, error) {
	if err := validateCustomAppID(id); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(filepath.Join(s.appDir(id), customAppKnowledgeFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("app knowledge: read: %w", err)
	}
	var wrap struct {
		Pages []appKnowledgePage `json:"pages"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, false, fmt.Errorf("app knowledge: decode: %w", err)
	}
	return wrap.Pages, true, nil
}

func (s *customAppStore) WriteAppKnowledge(id string, pages []appKnowledgePage) error {
	if err := validateCustomAppID(id); err != nil {
		return err
	}
	body, err := json.MarshalIndent(map[string]any{"pages": pages}, "", "  ")
	if err != nil {
		return fmt.Errorf("app knowledge: encode: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileAtomic(filepath.Join(s.appDir(id), customAppKnowledgeFile), body, 0o600); err != nil {
		return fmt.Errorf("app knowledge: write: %w", err)
	}
	return nil
}
