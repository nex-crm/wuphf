package team

// wiki_fs.go provides the wiki file experience onto the
// existing wiki git repo. It exposes the wiki content tree (directories,
// markdown pages, raw files, and self-contained HTML apps/websites) and a
// raw-byte file server with Range support, layered on top of the team/
// subtree already owned by *Repo.
//
// Design notes
// ============
//
//   - WUPHF paths are repo-root-relative and always carry the `team/` prefix
//     so the tree path of a markdown page is byte-identical to what
//     /wiki/catalog emits for that same file (e.g. team/people/nazz.md).
//
//   - Security: every path that crosses an HTTP boundary is resolved with
//     resolveTeamRelPath, which rejects traversal and confines the result
//     to r.TeamDir() using a separator-aware containment check (NOT a raw
//     strings.HasPrefix, which would let team/ leak into team-secrets/).
//
//   - This file holds only pure helpers + the tree walker. The thin HTTP
//     handlers live in wiki_fs_handlers.go.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TreeNode is one entry in the /wiki/tree response. The JSON shape is the
// wire contract consumed by the wiki sidebar.
//
//   - Path is RELATIVE TO REPO ROOT, slash-separated, and INCLUDES the
//     `team/` prefix. For a markdown page it is byte-identical to the path
//     /wiki/catalog emits for that file (team/<rel>.md). For directories,
//     raw files, and apps/websites it is team/<rel>.
//   - Children is populated only for "dir" nodes. Apps and websites are
//     leaves even though they are directories on disk.
//   - Ext is populated only for "file" nodes (lowercase, with the dot).
//   - Recursively-empty plain "dir" nodes are pruned from the tree: a folder
//     that holds no pages/files anywhere beneath it is scaffolding, not
//     navigation, so it never reaches the wire. See buildTreeNodes.
type TreeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Type     string     `json:"type"` // dir | page | file | app | website
	Title    string     `json:"title"`
	Ext      string     `json:"ext,omitempty"`
	Children []TreeNode `json:"children,omitempty"`
}

const (
	treeTypeDir     = "dir"
	treeTypePage    = "page"
	treeTypeFile    = "file"
	treeTypeApp     = "app"
	treeTypeWebsite = "website"
)

// errWikiFSBadPath signals a caller-supplied path that is empty, absolute,
// traversing, or otherwise outside the team/ subtree. Handlers map it to 400.
var errWikiFSBadPath = errors.New("wiki fs: invalid path")

// wikiFSMimeTypes maps a lowercase extension (with dot) to a Content-Type.
// Kept deliberately explicit — no os/mime sniffing — so the wire content
// type is deterministic and cannot be influenced by host MIME config.
var wikiFSMimeTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".avif":  "image/avif",
	".ico":   "image/x-icon",
	".pdf":   "application/pdf",
	".csv":   "text/csv; charset=utf-8",
	".md":    "text/markdown; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".mov":   "video/quicktime",
	".mp3":   "audio/mpeg",
	".wav":   "audio/wav",
	".ogg":   "audio/ogg",
	".m4a":   "audio/mp4",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".wasm":  "application/wasm",
	".xlsx":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".docx":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".pptx":  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".ipynb": "application/json",
}

// wikiFSContentType returns the Content-Type for a file extension, falling
// back to application/octet-stream when the extension is unknown. ext should
// be the value from filepath.Ext (with the leading dot); case is normalized.
func wikiFSContentType(ext string) string {
	if mt, ok := wikiFSMimeTypes[strings.ToLower(ext)]; ok {
		return mt
	}
	return "application/octet-stream"
}

// resolveTeamRelPath validates a repo-root-relative path (e.g.
// team/people/nazz.md) and returns the cleaned slash path plus the absolute
// on-disk path, guaranteed to live inside teamDir.
//
// The input path is relative to the REPO ROOT and carries the `team/` prefix,
// matching the path shape /wiki/catalog and /wiki/article speak. The absolute
// path is therefore joined onto repoRoot (NOT teamDir, which already ends in
// `team` — joining there would double the prefix to <root>/team/team/...).
// Containment is then verified against teamDir so nothing outside team/ can be
// reached even though the join happens at repoRoot.
//
// Rejections (all map to errWikiFSBadPath at the boundary):
//   - empty after trim
//   - absolute (leading separator / drive)
//   - contains a NUL or other ASCII control byte
//   - contains ".." segment after Clean, or escapes team/
//   - resolves outside teamDir (separator-aware containment, not raw prefix)
//
// The containment check Cleans the joined path and verifies it equals teamDir
// or sits under teamDir + os.PathSeparator. This is the separator-aware check
// the spec requires: a raw strings.HasPrefix(abs, teamDir) would treat
// "<root>/team-secrets" as inside "<root>/team", which it is not.
func resolveTeamRelPath(repoRoot, relPath string) (cleanRel string, abs string, err error) {
	teamDir := filepath.Join(repoRoot, "team")
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", "", fmt.Errorf("%w: path is required", errWikiFSBadPath)
	}
	if hasControlByte(relPath) {
		return "", "", fmt.Errorf("%w: path contains control characters", errWikiFSBadPath)
	}
	// Normalize to slash form first so Windows-style backslashes cannot
	// smuggle a traversal segment past the checks below.
	slash := filepath.ToSlash(relPath)
	if strings.HasPrefix(slash, "/") {
		return "", "", fmt.Errorf("%w: path must be relative; got %q", errWikiFSBadPath, relPath)
	}
	// filepath.IsAbs catches Windows volume-rooted paths (C:\...) that the
	// leading-slash check above would miss.
	if filepath.IsAbs(relPath) {
		return "", "", fmt.Errorf("%w: path must be relative; got %q", errWikiFSBadPath, relPath)
	}

	clean := filepath.ToSlash(filepath.Clean(slash))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", "", fmt.Errorf("%w: path must not contain ..; got %q", errWikiFSBadPath, relPath)
	}
	if clean != "team" && !strings.HasPrefix(clean, "team/") {
		return "", "", fmt.Errorf("%w: path must be within team/; got %q", errWikiFSBadPath, relPath)
	}

	// Build the absolute path by joining onto the repo root (the path already
	// carries the team/ prefix), Clean the result so any residual `.`/`..`
	// that slipped through is collapsed, then re-verify containment with a
	// separator-aware check against teamDir.
	abs = filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(clean)))
	if !isPathWithin(teamDir, abs) {
		return "", "", fmt.Errorf("%w: path escapes team root; got %q", errWikiFSBadPath, relPath)
	}
	return clean, abs, nil
}

// isPathWithin reports whether candidate is base itself or a descendant of
// base, using a separator boundary so "/a/team" is NOT treated as inside
// "/a/teamx". Both arguments should already be Cleaned.
func isPathWithin(base, candidate string) bool {
	if candidate == base {
		return true
	}
	withSep := base
	if !strings.HasSuffix(withSep, string(os.PathSeparator)) {
		withSep += string(os.PathSeparator)
	}
	return strings.HasPrefix(candidate, withSep)
}

// hasControlByte reports whether s contains a NUL or other ASCII control
// character (< 0x20 or 0x7f). These never appear in legitimate wiki paths
// and are a classic smuggling vector.
func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

// buildWikiTree walks the wiki content under <repoRoot>/team and returns the
// wiki file tree. subPath, when non-empty, is a repo-root-relative subtree root
// (e.g. team/people) that must resolve inside team/; empty means the whole
// team/ tree.
//
// The walk is non-recursive at the package boundary — buildTreeNodes recurses
// internally so app/website classification (which needs to peek at sibling
// files) stays simple.
func buildWikiTree(repoRoot, subPath string) ([]TreeNode, error) {
	startDir := filepath.Join(repoRoot, "team")
	if strings.TrimSpace(subPath) != "" {
		_, abs, err := resolveTeamRelPath(repoRoot, subPath)
		if err != nil {
			return nil, err
		}
		startDir = abs
	}
	info, err := os.Stat(startDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []TreeNode{}, nil
		}
		return nil, fmt.Errorf("wiki fs: stat tree root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: tree root is not a directory", errWikiFSBadPath)
	}
	return buildTreeNodes(repoRoot, startDir)
}

// buildTreeNodes returns the sorted child nodes of dir. It does not follow
// symlinks out of the tree: os.ReadDir reports the entry type without
// following, and we never descend into a symlinked directory.
func buildTreeNodes(repoRoot, dir string) ([]TreeNode, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("wiki fs: read dir %s: %w", dir, err)
	}

	nodes := make([]TreeNode, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden entries (dotfiles, .git, .gitkeep, .app marker, etc.)
		// and never descend into nested .git directories.
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)

		// Resolve the real type without following symlinks. A symlink that
		// points at a directory must not be descended into, so we classify
		// it by its own (non-followed) mode: symlinks are surfaced as plain
		// files, never as dirs/apps/websites.
		info, ierr := entry.Info()
		if ierr != nil {
			// Race with a concurrent delete; skip rather than abort the walk.
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			node := buildFileNode(repoRoot, full, name)
			nodes = append(nodes, node)
			continue
		}

		if entry.IsDir() {
			node, derr := buildDirNode(repoRoot, full, name)
			if derr != nil {
				return nil, derr
			}
			// Prune recursively-empty plain directories. A "dir" whose subtree
			// holds no pages/files (after the same pruning runs on its
			// children) is scaffolding noise, not navigation — the wiki tree
			// should surface folders only once they hold content, and let the
			// Librarian (or any writer) materialize a folder on demand by
			// writing into it. App/website dirs are leaves (real content) and
			// are never pruned. The check works bottom-up: an inner empty dir
			// drops first, which can leave its parent empty, dropped in turn.
			if node.Type == treeTypeDir && len(node.Children) == 0 {
				continue
			}
			nodes = append(nodes, node)
			continue
		}

		// Regular file.
		if strings.EqualFold(filepath.Ext(name), ".md") {
			nodes = append(nodes, buildPageNode(repoRoot, full, name))
			continue
		}
		nodes = append(nodes, buildFileNode(repoRoot, full, name))
	}

	sortTreeNodes(nodes)
	return nodes, nil
}

// buildDirNode classifies a directory as an app, website, or plain dir.
//
//   - index.html present AND index.md absent → leaf. "app" if a `.app`
//     marker file exists in the dir, else "website".
//   - otherwise → "dir" with recursively-built children.
func buildDirNode(repoRoot, full, name string) (TreeNode, error) {
	hasIndexHTML := regularFileExists(filepath.Join(full, "index.html"))
	hasIndexMD := regularFileExists(filepath.Join(full, "index.md"))

	if hasIndexHTML && !hasIndexMD {
		nodeType := treeTypeWebsite
		if regularFileExists(filepath.Join(full, ".app")) {
			nodeType = treeTypeApp
		}
		return TreeNode{
			Name:  name,
			Path:  repoRelSlash(repoRoot, full),
			Type:  nodeType,
			Title: humanizeName(name),
		}, nil
	}

	children, err := buildTreeNodes(repoRoot, full)
	if err != nil {
		return TreeNode{}, err
	}
	return TreeNode{
		Name:     name,
		Path:     repoRelSlash(repoRoot, full),
		Type:     treeTypeDir,
		Title:    humanizeName(name),
		Children: children,
	}, nil
}

// buildPageNode builds a "page" node for a markdown file. Its path matches
// /wiki/catalog exactly (team/<rel>.md), and its title resolves from the
// first H1, then YAML frontmatter title, then the humanized basename.
func buildPageNode(repoRoot, full, name string) TreeNode {
	return TreeNode{
		Name:  name,
		Path:  repoRelSlash(repoRoot, full),
		Type:  treeTypePage,
		Title: extractPageTitle(full, name),
	}
}

// buildFileNode builds a "file" node for any non-markdown, non-directory
// entry (or a symlink). Ext is the lowercase extension with the dot.
func buildFileNode(repoRoot, full, name string) TreeNode {
	return TreeNode{
		Name:  name,
		Path:  repoRelSlash(repoRoot, full),
		Type:  treeTypeFile,
		Title: humanizeName(stripExt(name)),
		Ext:   strings.ToLower(filepath.Ext(name)),
	}
}

// repoRelSlash returns the slash-separated path of full relative to repoRoot.
// For wiki content this always begins with `team/`.
func repoRelSlash(repoRoot, full string) string {
	rel, err := filepath.Rel(repoRoot, full)
	if err != nil {
		// full is always built by joining under repoRoot, so Rel cannot fail
		// in practice. Fall back to the basename rather than panicking.
		return filepath.ToSlash(filepath.Base(full))
	}
	return filepath.ToSlash(rel)
}

// regularFileExists reports whether path exists and is a regular file (not a
// directory or a symlink). Used for index.html / index.md / .app probes. It
// uses os.Lstat (not os.Stat) so a symlinked index.html cannot silently flip a
// directory's classification to app/website — matching the symlink-aware intent
// of the tree walk, which never follows symlinks out of the tree.
func regularFileExists(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// extractPageTitle resolves a markdown page title in priority order:
//  1. first level-1 ATX heading ("# Title")
//  2. YAML frontmatter `title:`
//  3. humanized basename (extension stripped)
//
// It reads the whole file once; at v1 corpus sizes pages are small.
func extractPageTitle(full, name string) string {
	data, err := os.ReadFile(full)
	if err != nil {
		return humanizeName(stripExt(name))
	}
	body := string(data)
	fm := extractFrontmatter(body)

	// 1. First H1 heading. Scan only the body AFTER any leading frontmatter
	//    block so a `# ...` inside frontmatter (e.g. a YAML comment) cannot
	//    win over the real body heading.
	for _, line := range strings.Split(bodyAfterFrontmatter(body, fm), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			if h := strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")); h != "" {
				return h
			}
		}
	}

	// 2. Frontmatter title.
	if fm != "" {
		if title := unquoteYAMLScalar(frontmatterValue(fm, "title")); title != "" {
			return title
		}
	}

	// 3. Basename fallback.
	return humanizeName(stripExt(name))
}

// bodyAfterFrontmatter returns the markdown body with any leading YAML
// frontmatter block stripped. fm is the inner block as returned by
// extractFrontmatter (delimiters excluded); pass "" when there is no
// frontmatter and the whole body is returned unchanged. The on-disk
// frontmatter region is exactly "---\n" + fm + "\n---", so the body begins
// after that region (and its trailing newline, when present).
func bodyAfterFrontmatter(body, fm string) string {
	if fm == "" {
		return body
	}
	region := "---\n" + fm + "\n---"
	if !strings.HasPrefix(body, region) {
		// Defensive: fm did not come from this body. Scan the whole thing.
		return body
	}
	rest := body[len(region):]
	return strings.TrimPrefix(rest, "\n")
}

// unquoteYAMLScalar strips a single pair of matching surrounding quotes from
// a YAML scalar value (title: "Foo" or title: 'Foo'). It leaves unquoted
// values untouched.
func unquoteYAMLScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// stripExt removes the final extension from a filename.
func stripExt(name string) string {
	if ext := filepath.Ext(name); ext != "" {
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// humanizeName turns a slug-ish basename into a display title:
// "customer-success" → "Customer Success", "q3_plan" → "Q3 Plan". It splits
// on hyphens, underscores, and spaces, then title-cases each word.
func humanizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	replacer := strings.NewReplacer("-", " ", "_", " ")
	words := strings.Fields(replacer.Replace(name))
	if len(words) == 0 {
		return name
	}
	for i, w := range words {
		words[i] = titleWord(w)
	}
	return strings.Join(words, " ")
}

// titleWord uppercases the first rune of a word and leaves the rest as-is so
// existing internal capitalization (e.g. acronyms, "iOS") is preserved.
func titleWord(w string) string {
	if w == "" {
		return w
	}
	r := []rune(w)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// sortTreeNodes orders a single level: directories and apps/websites first,
// then pages, then files; alphabetical (case-insensitive by name) within
// each group. This matches the wiki sidebar grouping while keeping
// deterministic output for the wire contract.
func sortTreeNodes(nodes []TreeNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		gi, gj := treeSortGroup(nodes[i].Type), treeSortGroup(nodes[j].Type)
		if gi != gj {
			return gi < gj
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
}

// treeSortGroup buckets a node type into a sort rank: container-like first
// (dir/app/website), then pages, then files.
func treeSortGroup(t string) int {
	switch t {
	case treeTypeDir, treeTypeApp, treeTypeWebsite:
		return 0
	case treeTypePage:
		return 1
	default: // treeTypeFile
		return 2
	}
}
