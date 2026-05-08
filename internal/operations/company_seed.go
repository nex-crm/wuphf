package operations

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Completer is the minimal interface for one LLM completion call.
// Defined locally to avoid import cycle with internal/provider.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// CompanySeedInput configures a SeedCompanyContext run.
type CompanySeedInput struct {
	WebsiteURL string
	FilePaths  []string
	OwnerName  string
	OwnerRole  string
	Completer  Completer
	WikiRoot   string
}

// CompanySeedResult summarizes what SeedCompanyContext did.
type CompanySeedResult struct {
	Profile         CompanyProfile
	ArticlesWritten []string
	Facts           []string
	Warnings        []string
	// NeedsRetry is true when a transient external step (URL fetch, LLM
	// extraction, JSON parse) failed. Callers that track retry intent
	// (e.g. PendingCompanySeed) should re-arm when this is set.
	NeedsRetry bool
}

// SeedCompanyContext fetches or reads content, runs LLM extraction, and
// writes wiki articles under WikiRoot/team/about/.
func SeedCompanyContext(ctx context.Context, input CompanySeedInput) (*CompanySeedResult, error) {
	result := &CompanySeedResult{}
	var contentBuf strings.Builder

	// 1. Fetch URL
	if input.WebsiteURL != "" {
		u, err := url.Parse(input.WebsiteURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			result.Warnings = append(result.Warnings, "skipped URL: must be http or https")
		} else {
			text, err := fetchURL(ctx, input.WebsiteURL)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("URL fetch failed: %v", err))
				result.NeedsRetry = true
			} else {
				contentBuf.WriteString(text)
				contentBuf.WriteString("\n")
			}
		}
	}

	// 2. Extract file content
	for _, path := range input.FilePaths {
		var (
			text string
			err  error
		)
		if strings.HasSuffix(strings.ToLower(path), ".pdf") {
			text, err = extractPDF(ctx, path)
			if err != nil {
				result.Warnings = append(result.Warnings, err.Error())
				continue
			}
		} else {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("read file %s: %v", path, readErr))
				continue
			}
			text = string(data)
		}
		if len(text) > 8*1024 {
			text = text[:8*1024]
		}
		contentBuf.WriteString(text)
		contentBuf.WriteString("\n")
	}

	// 3. Build wiki dirs
	if err := os.MkdirAll(filepath.Join(input.WikiRoot, "team", "about"), 0o755); err != nil {
		return nil, fmt.Errorf("operations: mkdir team/about: %w", err)
	}

	// 4. Write README (skip if exists)
	readmePath := filepath.Join(input.WikiRoot, "team", "about", "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		if err := atomicWrite(input.WikiRoot, "team/about/README.md", []byte(aboutReadmeContent)); err != nil {
			return nil, err
		}
	}

	// 5. Write owner.md (skip if both empty) — does not depend on content.
	if strings.TrimSpace(input.OwnerName) != "" || strings.TrimSpace(input.OwnerRole) != "" {
		ownerMD := buildOwnerMD(input.OwnerName, input.OwnerRole)
		if err := atomicWrite(input.WikiRoot, "team/about/owner.md", []byte(ownerMD)); err != nil {
			return nil, err
		}
		result.ArticlesWritten = append(result.ArticlesWritten, "team/about/owner.md")
	}

	// 6. LLM extraction (skip if no completer or no content)
	content := contentBuf.String()
	if input.Completer != nil && strings.TrimSpace(content) != "" {
		if len(content) > 32*1024 {
			content = content[:32*1024]
		}
		raw, err := runExtraction(ctx, input.Completer, content)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("LLM extraction failed: %v", err))
			result.NeedsRetry = true
		} else {
			profile, facts, err := parseExtraction(raw)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("LLM parse failed: %v", err))
				result.NeedsRetry = true
			} else {
				result.Profile = profile
				result.Facts = facts
				if input.WebsiteURL != "" {
					result.Profile.Website = input.WebsiteURL
				}
			}
		}
	}

	// 7. Write company.md
	if result.Profile.Name != "" || result.Profile.Description != "" {
		companyMD := buildCompanyMD(result.Profile)
		if err := atomicWrite(input.WikiRoot, "team/about/company.md", []byte(companyMD)); err != nil {
			return nil, err
		}
		result.ArticlesWritten = append(result.ArticlesWritten, "team/about/company.md")
	}

	return result, nil
}

// assertPublicHost resolves host and rejects loopback, private, and
// link-local addresses to prevent SSRF against local services or metadata APIs.
func assertPublicHost(ctx context.Context, host string) error {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", hostname, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.Equal(net.ParseIP("169.254.169.254")) {
			return fmt.Errorf("host %q resolves to non-public address %s", hostname, addr)
		}
	}
	return nil
}

// fetchURL retrieves the text content of the given URL by walking the HTML
// parse tree and collecting text from block-level nodes. Truncates to 4096 bytes.
func fetchURL(ctx context.Context, urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("fetch URL parse: %w", err)
	}
	if err := assertPublicHost(ctx, u.Host); err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return assertPublicHost(req.Context(), req.URL.Host)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("fetch URL build request: %w", err)
	}
	req.Header.Set("User-Agent", "wuphf-seed/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch URL: status %d", resp.StatusCode)
	}

	doc, err := html.Parse(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", fmt.Errorf("parse HTML: %w", err)
	}

	var buf bytes.Buffer
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "h1", "h2", "h3", "h4", "h5", "h6", "li":
				var textBuf bytes.Buffer
				collectText(n, &textBuf)
				line := strings.TrimSpace(textBuf.String())
				if line != "" {
					buf.WriteString(line)
					buf.WriteString("\n")
				}
				return
			case "script", "style", "noscript":
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	text := buf.String()
	if len(text) > 4096 {
		text = text[:4096]
	}
	return text, nil
}

// collectText recursively extracts text nodes from the HTML tree.
func collectText(n *html.Node, buf *bytes.Buffer) {
	if n.Type == html.TextNode {
		buf.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, buf)
	}
}

// extractPDF extracts text from a PDF file using pdftotext (poppler).
func extractPDF(ctx context.Context, path string) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("skipped %s: pdftotext not installed; install poppler (brew install poppler on macOS)", path)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext %s: %w", path, err)
	}
	reader := io.LimitReader(bytes.NewReader(out), 8192)
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read pdftotext output: %w", err)
	}
	return string(data), nil
}

// runExtraction calls the LLM completer with a structured extraction prompt.
func runExtraction(ctx context.Context, completer Completer, content string) (string, error) {
	prompt := `Extract company context from the content below. Output ONLY valid JSON with
no markdown fences, no explanation, no commentary before or after:
{"company_name":"...","description":"...","industry":"...","audience":"...",
 "goals":"...","key_facts":["fact 1","fact 2",...]}
key_facts: max 8 short factual bullets. Empty string for unknown fields.

Content:
` + content
	return completer.Complete(ctx, prompt)
}

type extractionPayload struct {
	CompanyName string   `json:"company_name"`
	Description string   `json:"description"`
	Industry    string   `json:"industry"`
	Audience    string   `json:"audience"`
	Goals       string   `json:"goals"`
	KeyFacts    []string `json:"key_facts"`
}

// parseExtraction strips JSON fences from the raw LLM output and unmarshals it.
func parseExtraction(raw string) (CompanyProfile, []string, error) {
	raw = strings.TrimSpace(raw)
	// Strip common LLM JSON fence opening (```json, ```JSON, ``` etc.).
	// Find the first newline to drop the entire opening fence line.
	if strings.HasPrefix(raw, "```") {
		if nl := strings.IndexByte(raw, '\n'); nl != -1 {
			raw = raw[nl+1:]
		} else {
			raw = raw[3:]
		}
		if idx := strings.LastIndex(raw, "```"); idx != -1 {
			raw = raw[:idx]
		}
	}
	raw = strings.TrimSpace(raw)

	var payload extractionPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return CompanyProfile{}, nil, fmt.Errorf("unmarshal extraction: %w", err)
	}

	profile := CompanyProfile{
		Name:        payload.CompanyName,
		Description: payload.Description,
		Industry:    payload.Industry,
		Audience:    payload.Audience,
	}
	if payload.Goals != "" {
		profile.Notes = []string{payload.Goals}
	}

	return profile, payload.KeyFacts, nil
}

// atomicWrite writes data to wikiRoot/relPath using a temp-dir-then-rename
// pattern so partial writes are never visible. Follows the same pattern as
// makeWikiTempDir in wiki_materialize.go.
func atomicWrite(wikiRoot, relPath string, data []byte) error {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("operations: atomicWrite token: %w", err)
	}
	name := fmt.Sprintf(".wiki.tmp.%s", hex.EncodeToString(buf))
	tempDir := filepath.Join(wikiRoot, name)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return fmt.Errorf("operations: atomicWrite tempdir %q: %w", tempDir, err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	stagePath := filepath.Join(tempDir, relPath)
	if err := os.MkdirAll(filepath.Dir(stagePath), 0o755); err != nil {
		return fmt.Errorf("operations: atomicWrite stage dir: %w", err)
	}
	if err := os.WriteFile(stagePath, data, 0o644); err != nil {
		return fmt.Errorf("operations: atomicWrite write stage: %w", err)
	}

	finalPath := filepath.Join(wikiRoot, relPath)
	if err := os.Rename(stagePath, finalPath); err != nil {
		return fmt.Errorf("operations: atomicWrite rename to %q: %w", finalPath, err)
	}
	return nil
}

// buildCompanyMD renders a markdown article for the company profile.
func buildCompanyMD(profile CompanyProfile) string {
	var sb strings.Builder
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = "Company"
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", name))
	if d := strings.TrimSpace(profile.Description); d != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", d))
	}
	if w := strings.TrimSpace(profile.Website); w != "" {
		sb.WriteString(fmt.Sprintf("**Website:** %s\n\n", w))
	}
	if ind := strings.TrimSpace(profile.Industry); ind != "" {
		sb.WriteString(fmt.Sprintf("**Industry:** %s\n\n", ind))
	}
	if aud := strings.TrimSpace(profile.Audience); aud != "" {
		sb.WriteString(fmt.Sprintf("**Audience:** %s\n\n", aud))
	}
	if len(profile.Notes) > 0 {
		for _, n := range profile.Notes {
			if n = strings.TrimSpace(n); n != "" {
				sb.WriteString(fmt.Sprintf("**Goals:** %s\n\n", n))
			}
		}
	}
	return sb.String()
}

// buildOwnerMD renders a markdown article for the workspace owner.
func buildOwnerMD(name, role string) string {
	var sb strings.Builder
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = "Owner"
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", displayName))
	if r := strings.TrimSpace(role); r != "" {
		sb.WriteString(fmt.Sprintf("**Role:** %s\n\n", r))
	}
	return sb.String()
}

// aboutReadmeContent is the README placed in the team/about/ wiki section.
const aboutReadmeContent = `# About This Team

> **For agents reading this section:** The articles here describe the humans
> and company that own and operate this WUPHF workspace. This is not information
> about external clients, customers, or contacts — that context lives elsewhere
> in the wiki. Use this section to understand who you are working for and what
> their goals are.

- [company.md](company.md) — what this company does
- [owner.md](owner.md) — who is running this workspace
`
