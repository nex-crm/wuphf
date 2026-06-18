package team

// wiki_categories_test.go — Phase 1 coverage for the article→category derived
// index: frontmatter parsing, store round-trips on BOTH backends, the §7.4
// rm-rf rebuild-equality contract with categories present, backend hash
// parity, and BuildArticle surfacing categories from frontmatter.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseCategoriesFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"none", "# Title\n\nNo frontmatter.\n", nil},
		{"empty list", "---\ncategories: []\n---\n# T\n", nil},
		{
			"inline bracket",
			"---\ncategories: [revenue-operations, ai-agents]\n---\n# T\n",
			[]string{"ai-agents", "revenue-operations"}, // sorted
		},
		{
			"block list",
			"---\ncategories:\n  - Revenue Operations\n  - AI Agents\n---\n# T\n",
			[]string{"ai-agents", "revenue-operations"},
		},
		{
			"bare scalar",
			"---\ncategories: Sales\n---\n# T\n",
			[]string{"sales"},
		},
		{
			"dedup + casing collapse",
			"---\ncategories: [Sales, sales, SALES]\n---\n# T\n",
			[]string{"sales"},
		},
		{
			"other frontmatter keys ignored",
			"---\nkind: company\ncategories: [b2b]\naliases:\n  - x\n---\n# T\n",
			[]string{"b2b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCategoriesFrontmatter(tc.body)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseCategoriesFrontmatter() = %v, want %v", got, tc.want)
			}
		})
	}
}

// storeFactory builds a fresh FactStore for the backend-parameterized tests.
type storeFactory struct {
	name string
	make func(t *testing.T) FactStore
}

func categoryStoreFactories() []storeFactory {
	return []storeFactory{
		{"inmemory", func(*testing.T) FactStore { return newInMemoryFactStore() }},
		{"sqlite", func(t *testing.T) FactStore {
			s, err := NewSQLiteFactStore(filepath.Join(t.TempDir(), "cat.sqlite"))
			if err != nil {
				t.Fatalf("NewSQLiteFactStore: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			return s
		}},
	}
}

func TestArticleCategories_StoreRoundtrip(t *testing.T) {
	ctx := context.Background()
	for _, sf := range categoryStoreFactories() {
		t.Run(sf.name, func(t *testing.T) {
			s := sf.make(t)

			// Two articles share "ai-agents"; one also has "revenue-operations".
			if err := s.UpsertArticleCategories(ctx, "team/companies/acme.md", []string{"ai-agents", "revenue-operations"}); err != nil {
				t.Fatalf("upsert acme: %v", err)
			}
			if err := s.UpsertArticleCategories(ctx, "team/concepts/mql.md", []string{"ai-agents"}); err != nil {
				t.Fatalf("upsert mql: %v", err)
			}

			assertSlice(t, "articles in ai-agents", listArticles(t, ctx, s, "ai-agents"),
				[]string{"team/companies/acme.md", "team/concepts/mql.md"})
			assertSlice(t, "articles in revenue-operations", listArticles(t, ctx, s, "revenue-operations"),
				[]string{"team/companies/acme.md"})
			assertSlice(t, "categories for acme", listCats(t, ctx, s, "team/companies/acme.md"),
				[]string{"ai-agents", "revenue-operations"})

			all, err := s.ListAllCategories(ctx)
			if err != nil {
				t.Fatalf("ListAllCategories: %v", err)
			}
			want := []CategoryCount{{Slug: "ai-agents", Count: 2}, {Slug: "revenue-operations", Count: 1}}
			if !reflect.DeepEqual(all, want) {
				t.Errorf("ListAllCategories = %v, want %v", all, want)
			}

			// Set-replace: re-upsert acme with a different set; old rows drop.
			if err := s.UpsertArticleCategories(ctx, "team/companies/acme.md", []string{"b2b"}); err != nil {
				t.Fatalf("re-upsert acme: %v", err)
			}
			assertSlice(t, "acme after replace", listCats(t, ctx, s, "team/companies/acme.md"), []string{"b2b"})
			assertSlice(t, "revenue-operations now empty", listArticles(t, ctx, s, "revenue-operations"), nil)

			// Clear: empty set removes all rows for the article.
			if err := s.UpsertArticleCategories(ctx, "team/companies/acme.md", nil); err != nil {
				t.Fatalf("clear acme: %v", err)
			}
			assertSlice(t, "acme cleared", listCats(t, ctx, s, "team/companies/acme.md"), nil)
			assertSlice(t, "b2b empty after clear", listArticles(t, ctx, s, "b2b"), nil)
		})
	}
}

// TestWikiIndex_CategoriesReconcileAndRebuild proves the reconcile hook
// populates article_categories from frontmatter, the §7.4 hash is stable across
// a full rm-rf rebuild WITH categories present, and a frontmatter mutation moves
// the hash (the category layer participates in drift detection).
func TestWikiIndex_CategoriesReconcileAndRebuild(t *testing.T) {
	ctx := context.Background()
	for _, sf := range categoryStoreFactories() {
		t.Run(sf.name, func(t *testing.T) {
			root := t.TempDir()
			writeBrief(t, root, "team/companies/acme.md",
				"---\nkind: company\ncategories: [revenue-operations, ai-agents]\n---\n# Acme\n")
			writeBrief(t, root, "team/concepts/mql.md",
				"---\ntype: concept\ncategories:\n  - AI Agents\n---\n# MQL\n")

			store1 := sf.make(t)
			idx1 := NewWikiIndex(root, WithFactStore(store1))
			if err := idx1.ReconcileFromMarkdown(ctx); err != nil {
				t.Fatalf("reconcile 1: %v", err)
			}

			// Memberships landed, keyed by wiki-root-relative path.
			assertSlice(t, "ai-agents members", listArticles(t, ctx, store1, "ai-agents"),
				[]string{"team/companies/acme.md", "team/concepts/mql.md"})
			assertSlice(t, "revenue-operations members", listArticles(t, ctx, store1, "revenue-operations"),
				[]string{"team/companies/acme.md"})

			h1, err := idx1.CanonicalHashAll(ctx)
			if err != nil {
				t.Fatalf("hash 1: %v", err)
			}

			// rm -rf index: rebuild from the same markdown into a fresh store.
			store2 := sf.make(t)
			idx2 := NewWikiIndex(root, WithFactStore(store2))
			if err := idx2.ReconcileFromMarkdown(ctx); err != nil {
				t.Fatalf("reconcile 2: %v", err)
			}
			h2, err := idx2.CanonicalHashAll(ctx)
			if err != nil {
				t.Fatalf("hash 2: %v", err)
			}
			if h1 != h2 {
				t.Errorf("CanonicalHashAll drift after rebuild with categories: %s -> %s", h1, h2)
			}

			// Mutate acme's categories on disk; the hash must move (category
			// layer is covered, not silently dropped).
			writeBrief(t, root, "team/companies/acme.md",
				"---\nkind: company\ncategories: [revenue-operations]\n---\n# Acme\n")
			store3 := sf.make(t)
			idx3 := NewWikiIndex(root, WithFactStore(store3))
			if err := idx3.ReconcileFromMarkdown(ctx); err != nil {
				t.Fatalf("reconcile 3: %v", err)
			}
			h3, err := idx3.CanonicalHashAll(ctx)
			if err != nil {
				t.Fatalf("hash 3: %v", err)
			}
			if h1 == h3 {
				t.Error("CanonicalHashAll unchanged after category mutation (layer not covered)")
			}
			// acme no longer carries ai-agents; mql still does.
			assertSlice(t, "ai-agents after mutation", listArticles(t, ctx, store3, "ai-agents"),
				[]string{"team/concepts/mql.md"})
		})
	}
}

// TestWikiIndex_CategoriesHashBackendParity locks the §7.4 promise that the
// canonical hash is backend-agnostic: the same markdown corpus yields the same
// CanonicalHashAll on the in-memory and SQLite stores once categories exist.
func TestWikiIndex_CategoriesHashBackendParity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBrief(t, root, "team/companies/acme.md",
		"---\ncanonical_slug: acme\nkind: company\ncategories: [revenue-operations, ai-agents]\n---\n# Acme\n")
	writeBrief(t, root, "team/concepts/mql.md",
		"---\ntype: concept\ncategories: [ai-agents]\n---\n# MQL\n")

	mem := newInMemoryFactStore()
	idxMem := NewWikiIndex(root, WithFactStore(mem))
	if err := idxMem.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile in-memory: %v", err)
	}
	hMem, err := idxMem.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("hash in-memory: %v", err)
	}

	sq, err := NewSQLiteFactStore(filepath.Join(t.TempDir(), "parity.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteFactStore: %v", err)
	}
	defer func() { _ = sq.Close() }()
	idxSq := NewWikiIndex(root, WithFactStore(sq))
	if err := idxSq.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile sqlite: %v", err)
	}
	hSq, err := idxSq.CanonicalHashAll(ctx)
	if err != nil {
		t.Fatalf("hash sqlite: %v", err)
	}

	if hMem != hSq {
		t.Errorf("CanonicalHashAll backend mismatch: in-memory %s != sqlite %s", hMem, hSq)
	}
}

func TestBuildArticle_Categories(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := t.TempDir()
	repo := NewRepoAt(root, filepath.Join(t.TempDir(), "bak"))
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	withCats := "---\ncategories: [Revenue Operations, ai-agents]\n---\n# Acme\n\nBody.\n"
	if _, _, err := repo.Commit(ctx, "archivist", "team/companies/acme.md", withCats, "create", "add acme"); err != nil {
		t.Fatalf("commit acme: %v", err)
	}
	noCats := "# Bare\n\nNo frontmatter here.\n"
	if _, _, err := repo.Commit(ctx, "archivist", "team/companies/bare.md", noCats, "create", "add bare"); err != nil {
		t.Fatalf("commit bare: %v", err)
	}

	meta, err := repo.BuildArticle(ctx, "team/companies/acme.md", "", nil)
	if err != nil {
		t.Fatalf("BuildArticle acme: %v", err)
	}
	if !reflect.DeepEqual(meta.Categories, []string{"ai-agents", "revenue-operations"}) {
		t.Errorf("acme Categories = %v, want [ai-agents revenue-operations]", meta.Categories)
	}

	bare, err := repo.BuildArticle(ctx, "team/companies/bare.md", "", nil)
	if err != nil {
		t.Fatalf("BuildArticle bare: %v", err)
	}
	// No categories declared → empty, non-nil slice (stable JSON shape).
	if bare.Categories == nil || len(bare.Categories) != 0 {
		t.Errorf("bare Categories = %v, want non-nil empty slice", bare.Categories)
	}
}

// --- helpers --------------------------------------------------------------

func writeBrief(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func listArticles(t *testing.T, ctx context.Context, s FactStore, cat string) []string {
	t.Helper()
	got, err := s.ListArticlesInCategory(ctx, cat)
	if err != nil {
		t.Fatalf("ListArticlesInCategory(%q): %v", cat, err)
	}
	return got
}

func listCats(t *testing.T, ctx context.Context, s FactStore, path string) []string {
	t.Helper()
	got, err := s.ListCategoriesForArticle(ctx, path)
	if err != nil {
		t.Fatalf("ListCategoriesForArticle(%q): %v", path, err)
	}
	return got
}

func assertSlice(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}
