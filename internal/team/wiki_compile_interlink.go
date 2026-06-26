package team

// wiki_compile_interlink.go — deterministic, no-LLM cross-linking of compiled
// pages. After Phase-2 writes the article bodies, this pass scans each page for
// mentions of OTHER pages' titles and wraps the FIRST occurrence of each target
// as a [[slug|Title]] wikilink. A single uniform pass over every live page
// against every other live title subsumes both the "outbound" direction (a
// newly written page links out to existing titles) and the "inbound" direction
// (an existing page links to a newly created title).
//
// The wrapping is strictly bounded by skip rules so it never corrupts content:
// text already inside [[...]], inside a ^[...] citation marker, inside a
// markdown heading line, or inside a fenced code block is never touched. The
// pass is idempotent — an already-wrapped link sits inside a protected [[...]]
// span, so a re-run wraps nothing new and the byte-identical rewrite folds to a
// no-op at the commit layer.

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// linkTarget is one page another page may link to. The compiled regexp matches
// the title case-insensitively at word boundaries.
type linkTarget struct {
	Slug  string
	Title string
	re    *regexp.Regexp
}

// inlineProtectRe matches inline spans that must never be linked into: an
// existing [[wikilink]] or a ^[citation] marker.
var inlineProtectRe = regexp.MustCompile(`\[\[[^\]]*\]\]|\^\[[^\]]*\]`)

// byteRange is a half-open [start,end) span of the body that is off-limits to
// linking.
type byteRange struct{ start, end int }

// buildLinkTargets compiles a deterministic, dedup-by-slug target list from the
// live pages, sorted longest-title-first (then alphabetically) so a longer
// title wins over a shorter one it contains (e.g. "Reciprocal Rank Fusion"
// before "Rank Fusion"). Pages with an empty title are skipped.
func buildLinkTargets(pages []compiledPageRef) []linkTarget {
	seen := make(map[string]struct{}, len(pages))
	targets := make([]linkTarget, 0, len(pages))
	for _, p := range pages {
		title := strings.TrimSpace(p.Title)
		if title == "" || p.Slug == "" {
			continue
		}
		if _, ok := seen[p.Slug]; ok {
			continue
		}
		seen[p.Slug] = struct{}{}
		targets = append(targets, linkTarget{
			Slug:  p.Slug,
			Title: title,
			re:    regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(title) + `\b`),
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if len(targets[i].Title) != len(targets[j].Title) {
			return len(targets[i].Title) > len(targets[j].Title)
		}
		return targets[i].Title < targets[j].Title
	})
	return targets
}

// interlinkPages rewrites every live page so the first mention of any other
// page's title becomes a wikilink. Returns the number of pages actually
// rewritten plus any per-page error strings. Each rewrite goes through the
// single-writer worker in "replace" mode; an unchanged page is not re-enqueued.
func interlinkPages(ctx context.Context, worker *WikiWorker, pages []compiledPageRef) (int, []string) {
	targets := buildLinkTargets(pages)
	if len(targets) < 2 {
		// With fewer than two titles there is nothing any page can link to.
		return 0, nil
	}

	var (
		linked int
		errs   []string
	)
	for _, p := range pages {
		body, err := worker.ReadArticle(p.RelPath)
		if err != nil {
			errs = append(errs, "interlink "+p.Slug+": read: "+err.Error())
			continue
		}
		newBody := linkifyBody(string(body), targets, p.Slug)
		if newBody == string(body) {
			continue
		}
		if _, _, err := worker.Enqueue(ctx, ArchivistAuthor, p.RelPath, newBody, "replace", "compile: interlink "+p.Slug); err != nil {
			errs = append(errs, "interlink "+p.Slug+": write: "+err.Error())
			continue
		}
		linked++
	}
	sort.Strings(errs)
	return linked, errs
}

// linkifyBody wraps the first unprotected, whole-word occurrence of each target
// title (other than the page's own) as [[slug|Title]]. Targets are applied in
// the caller's order (longest title first); protected ranges are recomputed
// after each splice so a freshly inserted link is itself protected from later
// targets.
func linkifyBody(body string, targets []linkTarget, selfSlug string) string {
	for _, t := range targets {
		if t.Slug == selfSlug {
			continue
		}
		protected := protectedRanges(body)
		loc := firstUnprotectedMatch(body, t.re, protected)
		if loc == nil {
			continue
		}
		replacement := "[[" + t.Slug + "|" + t.Title + "]]"
		body = body[:loc[0]] + replacement + body[loc[1]:]
	}
	return body
}

// firstUnprotectedMatch returns the [start,end) of the first regexp match that
// does not overlap any protected range, or nil when none qualifies.
func firstUnprotectedMatch(body string, re *regexp.Regexp, protected []byteRange) []int {
	for _, loc := range re.FindAllStringIndex(body, -1) {
		if !overlapsAny(loc[0], loc[1], protected) {
			return loc
		}
	}
	return nil
}

// overlapsAny reports whether [start,end) intersects any protected range.
func overlapsAny(start, end int, ranges []byteRange) bool {
	for _, r := range ranges {
		if start < r.end && r.start < end {
			return true
		}
	}
	return false
}

// protectedRanges computes the spans that linking must skip: fenced code blocks
// (including the ``` fence lines), markdown heading lines, and inline
// [[wikilink]] / ^[citation] spans.
func protectedRanges(body string) []byteRange {
	var ranges []byteRange

	// Line-based protection: code fences and headings. SplitAfter keeps the
	// trailing newline on each line so byte offsets stay contiguous.
	offset := 0
	inFence := false
	for _, line := range strings.SplitAfter(body, "\n") {
		start := offset
		end := offset + len(line)
		offset = end
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			ranges = append(ranges, byteRange{start, end})
			inFence = !inFence
		case inFence:
			ranges = append(ranges, byteRange{start, end})
		case strings.HasPrefix(trimmed, "#"):
			ranges = append(ranges, byteRange{start, end})
		}
	}

	// Inline protection: existing wikilinks and citation markers.
	for _, loc := range inlineProtectRe.FindAllStringIndex(body, -1) {
		ranges = append(ranges, byteRange{loc[0], loc[1]})
	}
	return ranges
}
