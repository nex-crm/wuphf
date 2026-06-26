package team

import (
	"reflect"
	"sort"
	"testing"
)

// resolverFor builds a linkRewriteResolver over a fixed existence set, matching
// the production newLinkResolver wiring used by MovePage.
func resolverFor(existing ...string) linkRewriteResolver {
	set := make(map[string]struct{}, len(existing))
	for _, p := range existing {
		set[p] = struct{}{}
	}
	return newLinkResolver(func(p string) bool {
		_, ok := set[p]
		return ok
	})
}

func TestCandidateRelPathsMatchesWebOrder(t *testing.T) {
	tests := []struct {
		name string
		slug string
		want []string
	}{
		{
			name: "bare slug fans out across candidate dirs in priority order",
			slug: "nazz",
			want: []string{
				"team/people/nazz.md",
				"team/companies/nazz.md",
				"team/playbooks/nazz.md",
				"team/decisions/nazz.md",
				"team/projects/nazz.md",
				"team/nazz.md",
			},
		},
		{
			name: "kinded slug resolves to a single team-prefixed path",
			slug: "people/nazz",
			want: []string{"team/people/nazz.md"},
		},
		{
			name: "full team path passes through unchanged",
			slug: "team/decisions/q3.md",
			want: []string{"team/decisions/q3.md"},
		},
		{
			name: "full team path without .md gains the suffix",
			slug: "team/decisions/q3",
			want: []string{"team/decisions/q3.md"},
		},
		{
			name: "empty slug yields no candidates",
			slug: "   ",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := candidateRelPaths(tc.slug)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("candidateRelPaths(%q) = %v, want %v", tc.slug, got, tc.want)
			}
		})
	}
}

func TestRewriteWikilinks_BareSlug(t *testing.T) {
	const from = "team/people/nazz.md"
	const to = "team/companies/nazz.md"
	articles := map[string]string{
		"team/projects/x.md": "see [[nazz]] for context",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md")
	resolveTo := resolverFor(to, "team/projects/x.md")

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	// Destination companies/nazz: bare basename "nazz" still resolves (post-move
	// people/nazz is gone, companies/nazz wins) so the link STAYS bare. The text
	// is byte-identical, so it is NOT counted or reported as a changed article —
	// references_rewritten tracks links whose bytes actually changed, keeping it
	// consistent with rewritten_paths.
	if count != 0 {
		t.Fatalf("count = %d, want 0 (bare slug stays byte-identical)", count)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want empty (no byte change)", changed)
	}
}

func TestRewriteWikilinks_FullPath(t *testing.T) {
	const from = "team/people/nazz.md"
	const to = "team/people/najmuzzaman.md"
	articles := map[string]string{
		"team/projects/x.md": "ref [[team/people/nazz.md]] here",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md")
	resolveTo := resolverFor(to, "team/projects/x.md")

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	want := "ref [[team/people/najmuzzaman.md]] here"
	if got := changed["team/projects/x.md"]; got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}
}

func TestRewriteWikilinks_KindedSlugPreservesDisplay(t *testing.T) {
	const from = "team/people/nazz.md"
	const to = "team/companies/nazz.md"
	articles := map[string]string{
		"team/projects/x.md": "hi [[people/nazz|Display Text]] bye",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md")
	resolveTo := resolverFor(to, "team/projects/x.md")

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	want := "hi [[companies/nazz|Display Text]] bye"
	if got := changed["team/projects/x.md"]; got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}
}

// The headline correctness property: with two same-basename pages, a bare
// [[nazz]] resolves to people/nazz (people wins the candidate order). Moving
// people/nazz rewrites it; moving companies/nazz must NOT touch it.
func TestRewriteWikilinks_SameBasenameCollision(t *testing.T) {
	const people = "team/people/nazz.md"
	const companies = "team/companies/nazz.md"
	const refArticle = "team/projects/x.md"

	t.Run("moving the page the bare slug resolves to rewrites it", func(t *testing.T) {
		to := "team/people/renamed.md"
		articles := map[string]string{refArticle: "[[nazz]]"}
		// Pre-move: both nazz pages exist; people wins.
		resolveFrom := resolverFor(people, companies, refArticle)
		// Post-move: people/nazz gone, people/renamed + companies/nazz exist.
		resolveTo := resolverFor(to, companies, refArticle)

		changed, count := rewriteWikilinks(articles, people, to, resolveFrom, resolveTo)
		if count != 1 {
			t.Fatalf("count = %d, want 1", count)
		}
		// Bare "renamed" resolves to people/renamed (companies/nazz does not
		// shadow it) so it can stay bare.
		if got := changed[refArticle]; got != "[[renamed]]" {
			t.Fatalf("rewrite = %q, want [[renamed]]", got)
		}
	})

	t.Run("moving the shadowed page leaves the bare slug untouched", func(t *testing.T) {
		to := "team/companies/renamed.md"
		articles := map[string]string{refArticle: "[[nazz]]"}
		resolveFrom := resolverFor(people, companies, refArticle)
		resolveTo := resolverFor(people, to, refArticle)

		changed, count := rewriteWikilinks(articles, companies, to, resolveFrom, resolveTo)
		if count != 0 {
			t.Fatalf("count = %d, want 0 (the bare slug resolves to people/nazz, not the moved companies/nazz)", count)
		}
		if len(changed) != 0 {
			t.Fatalf("changed = %v, want empty", changed)
		}
	})
}

// When the bare destination basename WOULD be shadowed by another page, the
// rewrite falls back to the kinded slug so the link still points at `to`.
func TestRewriteWikilinks_BareFallsBackWhenDestinationShadowed(t *testing.T) {
	// people/foo moves to companies/nazz. After the move, bare "nazz" resolves
	// to people/nazz (still present, wins the order), NOT to companies/nazz.
	// So a [[foo]] reference that pointed at people/foo cannot stay bare as
	// "nazz" — it must become the kinded "companies/nazz".
	const from = "team/people/foo.md"
	const to = "team/companies/nazz.md"
	const shadow = "team/people/nazz.md"
	const refArticle = "team/projects/x.md"

	articles := map[string]string{refArticle: "[[foo]]"}
	resolveFrom := resolverFor(from, shadow, refArticle)
	resolveTo := resolverFor(to, shadow, refArticle)

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if got := changed[refArticle]; got != "[[companies/nazz]]" {
		t.Fatalf("rewrite = %q, want [[companies/nazz]] (bare nazz is shadowed)", got)
	}
}

func TestRewriteWikilinks_ReparentVsRename(t *testing.T) {
	const refArticle = "team/projects/x.md"

	t.Run("rename within the same directory", func(t *testing.T) {
		from := "team/people/nazz.md"
		to := "team/people/naz.md"
		articles := map[string]string{refArticle: "[[people/nazz]]"}
		resolveFrom := resolverFor(from, refArticle)
		resolveTo := resolverFor(to, refArticle)
		changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
		if count != 1 || changed[refArticle] != "[[people/naz]]" {
			t.Fatalf("rename rewrite = %q count=%d, want [[people/naz]] count=1", changed[refArticle], count)
		}
	})

	t.Run("reparent to a different directory", func(t *testing.T) {
		from := "team/people/nazz.md"
		to := "team/companies/nazz.md"
		articles := map[string]string{refArticle: "[[people/nazz]]"}
		resolveFrom := resolverFor(from, refArticle)
		resolveTo := resolverFor(to, refArticle)
		changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
		if count != 1 || changed[refArticle] != "[[companies/nazz]]" {
			t.Fatalf("reparent rewrite = %q count=%d, want [[companies/nazz]] count=1", changed[refArticle], count)
		}
	})
}

// A folder move cascades: links to every descendant page are rewritten.
func TestRewriteWikilinksMulti_FolderCascade(t *testing.T) {
	moves := []pageMove{
		{From: "team/people/a.md", To: "team/staff/a.md"},
		{From: "team/people/b.md", To: "team/staff/b.md"},
	}
	const refArticle = "team/projects/x.md"
	articles := map[string]string{
		refArticle: "[[people/a]] and [[people/b]] and [[other]]",
	}
	resolveFrom := resolverFor("team/people/a.md", "team/people/b.md", "team/other.md", refArticle)
	resolveTo := resolverFor("team/staff/a.md", "team/staff/b.md", "team/other.md", refArticle)

	changed, count := rewriteWikilinksMulti(articles, moves, resolveFrom, resolveTo)
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	want := "[[staff/a]] and [[staff/b]] and [[other]]"
	if got := changed[refArticle]; got != want {
		t.Fatalf("cascade rewrite = %q, want %q", got, want)
	}
}

// A link in the moved article to a sibling stays valid; a self-link is handled.
func TestRewriteWikilinksMulti_SelfAndSiblingLinks(t *testing.T) {
	// Move people/a.md -> staff/a.md. Its body links to itself and to a sibling
	// people/b.md that is NOT moving. After the move the snapshot is re-keyed to
	// the destination path (the production MovePage does this via
	// remapSnapshotKeys), so the rewrite input has the body under staff/a.md.
	moves := []pageMove{{From: "team/people/a.md", To: "team/staff/a.md"}}
	articles := map[string]string{
		// keyed at the post-move path, body still pre-move text
		"team/staff/a.md": "self [[people/a]] sibling [[people/b]]",
	}
	resolveFrom := resolverFor("team/people/a.md", "team/people/b.md")
	resolveTo := resolverFor("team/staff/a.md", "team/people/b.md")

	changed, count := rewriteWikilinksMulti(articles, moves, resolveFrom, resolveTo)
	if count != 1 {
		t.Fatalf("count = %d, want 1 (only the self-link is rewritten)", count)
	}
	want := "self [[staff/a]] sibling [[people/b]]"
	if got := changed["team/staff/a.md"]; got != want {
		t.Fatalf("self/sibling rewrite = %q, want %q", got, want)
	}
}

func TestRewriteWikilinks_BrokenLinkLeftUntouched(t *testing.T) {
	const from = "team/people/nazz.md"
	const to = "team/companies/nazz.md"
	articles := map[string]string{
		"team/projects/x.md": "dangling [[does-not-exist]] and [[nazz]]",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md")
	resolveTo := resolverFor(to, "team/projects/x.md")

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	// The dangling link never resolves (untouched); the [[nazz]] link resolves
	// to `from` but stays byte-identical (bare → bare), so no byte change is
	// reported. The dangling link proves a broken slug is left alone.
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want empty (dangling untouched, bare stays bare)", changed)
	}
}

func TestRewriteWikilinks_InvalidFormsIgnored(t *testing.T) {
	// Rename within people so the kinded slug visibly changes text, proving the
	// valid link IS rewritten while the invalid forms beside it are left alone.
	const from = "team/people/nazz.md"
	const to = "team/people/naz.md"
	articles := map[string]string{
		// extra-pipe (invalid), traversal (invalid), empty (invalid), then a
		// valid kinded slug that must be rewritten.
		"team/projects/x.md": "[[a|b|c]] [[../escape]] [[]] [[people/nazz]]",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md")
	resolveTo := resolverFor(to, "team/projects/x.md")

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	if count != 1 {
		t.Fatalf("count = %d, want 1 (only the valid kinded slug)", count)
	}
	want := "[[a|b|c]] [[../escape]] [[]] [[people/naz]]"
	if got := changed["team/projects/x.md"]; got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}
}

// The inner-class regex must accept a stray '[' inside the link, byte-identical
// to the web buildReplacements grammar (/\[\[([^\]\n]+)\]\]/g). On input
// "[[a[b]]" the web client parses inner "a[b"; the Go rewriter must too, so a
// slug containing '[' resolves and rewrites instead of being silently skipped.
func TestRewriteWikilinks_SlugContainingOpenBracket(t *testing.T) {
	// Sanity: the regex captures the same inner text the web grammar would.
	got := rewriteWikilinkInner.FindStringSubmatch("[[a[b]]")
	if len(got) != 2 || got[1] != "a[b" {
		t.Fatalf("regex inner = %#v, want capture group %q (web parity)", got, "a[b")
	}

	const from = "team/people/a[b.md"
	const to = "team/people/c.md"
	const refArticle = "team/projects/x.md"
	articles := map[string]string{
		refArticle: "see [[a[b]] for context",
	}
	// Pre-move: the '['-bearing page exists and the bare slug resolves to it.
	resolveFrom := resolverFor(from, refArticle)
	resolveTo := resolverFor(to, refArticle)

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	if count != 1 {
		t.Fatalf("count = %d, want 1 (slug with '[' must rewrite, matching web grammar)", count)
	}
	// Bare "c" resolves to people/c post-move (nothing shadows it) so it stays bare.
	if got := changed[refArticle]; got != "see [[c]] for context" {
		t.Fatalf("rewrite = %q, want %q", got, "see [[c]] for context")
	}
}

func TestRewriteWikilinks_NoChangeReturnsEmpty(t *testing.T) {
	const from = "team/people/nazz.md"
	const to = "team/companies/nazz.md"
	articles := map[string]string{
		"team/projects/x.md": "no links here at all",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md")
	resolveTo := resolverFor(to, "team/projects/x.md")

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	if count != 0 || len(changed) != 0 {
		t.Fatalf("changed=%v count=%d, want empty/0", changed, count)
	}
}

func TestRewriteWikilinks_MultipleLinksSameArticle(t *testing.T) {
	const from = "team/people/nazz.md"
	const to = "team/people/naz.md"
	articles := map[string]string{
		"team/projects/x.md": "[[people/nazz]] then [[people/nazz|Alias]] then [[team/people/nazz.md]]",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md")
	resolveTo := resolverFor(to, "team/projects/x.md")

	changed, count := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	want := "[[people/naz]] then [[people/naz|Alias]] then [[team/people/naz.md]]"
	if got := changed["team/projects/x.md"]; got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}
}

func TestSlugFormHelpers(t *testing.T) {
	if got := slugFromRelPath("team/people/nazz.md"); got != "people/nazz" {
		t.Fatalf("slugFromRelPath = %q, want people/nazz", got)
	}
	if got := bareSlugFromRelPath("team/companies/acme.md"); got != "acme" {
		t.Fatalf("bareSlugFromRelPath = %q, want acme", got)
	}
	if got := slugFromRelPath("not-a-team-path"); got != "" {
		t.Fatalf("slugFromRelPath(non-team) = %q, want empty", got)
	}
	if !isFullPathSlug("team/x/y.md") {
		t.Fatal("isFullPathSlug(team/x/y.md) = false, want true")
	}
	if isFullPathSlug("people/nazz") {
		t.Fatal("isFullPathSlug(people/nazz) = true, want false")
	}
}

// changedKeys is a small test helper to compare changed-set membership.
func changedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestRewriteWikilinks_OnlyResolvingArticlesChange(t *testing.T) {
	const from = "team/people/nazz.md"
	const to = "team/people/naz.md"
	articles := map[string]string{
		"team/projects/x.md": "[[nazz]]",
		"team/projects/y.md": "no link",
		"team/projects/z.md": "[[people/nazz]]",
	}
	resolveFrom := resolverFor(from, "team/projects/x.md", "team/projects/y.md", "team/projects/z.md")
	resolveTo := resolverFor(to, "team/projects/x.md", "team/projects/y.md", "team/projects/z.md")

	changed, _ := rewriteWikilinks(articles, from, to, resolveFrom, resolveTo)
	got := changedKeys(changed)
	want := []string{"team/projects/x.md", "team/projects/z.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed keys = %v, want %v", got, want)
	}
}
