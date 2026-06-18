package team

// wiki_category_tree_test.go — Phase 3b coverage for the category_parents
// derived index (the subcategory tree): frontmatter parsing, path detection,
// store round-trips on both backends, the rm-rf rebuild + backend-parity
// contract, and the guarantee that a category page is NOT indexed as an entity
// or article.

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseParentCategoriesFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"none", "# Sales\n\nA category.\n", nil},
		{"inline", "---\nparent_categories: [revenue, gtm]\n---\n# T\n", []string{"gtm", "revenue"}},
		{"block + casing", "---\nparent_categories:\n  - Revenue\n  - GTM\n---\n# T\n", []string{"gtm", "revenue"}},
		{"dedup", "---\nparent_categories: [revenue, Revenue]\n---\n# T\n", []string{"revenue"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseParentCategoriesFrontmatter(tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseParentCategoriesFrontmatter() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsCategoryPagePath(t *testing.T) {
	cases := []struct {
		path string
		ok   bool
		slug string
	}{
		{"team/.categories/sales.md", true, "sales"},
		{"team/.categories/revenue-operations.md", true, "revenue-operations"},
		{"team/companies/acme.md", false, ""},
		{"team/.categories/sub/nested.md", false, ""}, // only direct children
		{"team/people/bob.md", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := isCategoryPagePath(tc.path); got != tc.ok {
				t.Errorf("isCategoryPagePath(%q) = %v, want %v", tc.path, got, tc.ok)
			}
			if tc.ok {
				if got := categoryPageSlug(tc.path); got != tc.slug {
					t.Errorf("categoryPageSlug(%q) = %q, want %q", tc.path, got, tc.slug)
				}
			}
		})
	}
}

func TestCategoryParents_StoreRoundtrip(t *testing.T) {
	ctx := context.Background()
	for _, sf := range categoryStoreFactories() {
		t.Run(sf.name, func(t *testing.T) {
			s := sf.make(t)
			if err := s.UpsertCategoryParents(ctx, "sales", []string{"revenue", "gtm"}); err != nil {
				t.Fatalf("upsert sales: %v", err)
			}
			if err := s.UpsertCategoryParents(ctx, "marketing", []string{"gtm"}); err != nil {
				t.Fatalf("upsert marketing: %v", err)
			}

			parents, err := s.ListCategoryParents(ctx, "sales")
			if err != nil {
				t.Fatalf("list parents: %v", err)
			}
			if !reflect.DeepEqual(parents, []string{"gtm", "revenue"}) {
				t.Errorf("sales parents = %v, want [gtm revenue]", parents)
			}

			all, err := s.ListAllCategoryParents(ctx)
			if err != nil {
				t.Fatalf("list all: %v", err)
			}
			want := []CategoryParent{
				{Category: "marketing", Parent: "gtm"},
				{Category: "sales", Parent: "gtm"},
				{Category: "sales", Parent: "revenue"},
			}
			if !reflect.DeepEqual(all, want) {
				t.Errorf("all parents = %v, want %v", all, want)
			}

			// Set-replace, then clear.
			if err := s.UpsertCategoryParents(ctx, "sales", []string{"revenue"}); err != nil {
				t.Fatalf("replace: %v", err)
			}
			if p, _ := s.ListCategoryParents(ctx, "sales"); !reflect.DeepEqual(p, []string{"revenue"}) {
				t.Errorf("after replace = %v, want [revenue]", p)
			}
			if err := s.UpsertCategoryParents(ctx, "sales", nil); err != nil {
				t.Fatalf("clear: %v", err)
			}
			if p, _ := s.ListCategoryParents(ctx, "sales"); len(p) != 0 {
				t.Errorf("after clear = %v, want empty", p)
			}
		})
	}
}

// TestWikiIndex_CategoryTreeReconcileAndRebuild proves a category page's
// parent_categories reconcile into category_parents, the page is NOT indexed as
// an entity or article, the §7.4 hash is stable across rebuild + backend, and a
// frontmatter edit moves the hash.
func TestWikiIndex_CategoryTreeReconcileAndRebuild(t *testing.T) {
	ctx := context.Background()
	build := func(t *testing.T, store FactStore, root string) (*WikiIndex, string) {
		idx := NewWikiIndex(root, WithFactStore(store))
		if err := idx.ReconcileFromMarkdown(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		h, err := idx.CanonicalHashAll(ctx)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		return idx, h
	}

	for _, sf := range categoryStoreFactories() {
		t.Run(sf.name, func(t *testing.T) {
			root := t.TempDir()
			// A category page with parents + an ordinary article filed into it.
			writeBrief(t, root, "team/.categories/sales.md",
				"---\nparent_categories: [revenue, gtm]\n---\n# Sales\n\nDeals.\n")
			writeBrief(t, root, "team/companies/acme.md",
				"---\nkind: company\ncategories: [sales]\n---\n# Acme\n")

			store1 := sf.make(t)
			idx1, h1 := build(t, store1, root)

			edges, err := idx1.ListAllCategoryParents(ctx)
			if err != nil {
				t.Fatalf("edges: %v", err)
			}
			want := []CategoryParent{{Category: "sales", Parent: "gtm"}, {Category: "sales", Parent: "revenue"}}
			if !reflect.DeepEqual(edges, want) {
				t.Errorf("edges = %v, want %v", edges, want)
			}

			// The category page is NOT an article (no article_categories rows)…
			if cats, _ := store1.ListCategoriesForArticle(ctx, "team/.categories/sales.md"); len(cats) != 0 {
				t.Errorf("category page got article_categories rows: %v", cats)
			}
			// …and NOT an entity.
			entityCount := 0
			_ = store1.IterateEntities(ctx, func(e IndexEntity) error {
				if e.Slug == "sales" {
					t.Errorf("category page was indexed as entity %+v", e)
				}
				entityCount++
				return nil
			})
			// acme is the only entity; the category page must not have added one.
			if entityCount != 1 {
				t.Errorf("entity count = %d, want 1 (acme only)", entityCount)
			}

			// rm -rf index: rebuild from the same markdown → identical hash.
			store2 := sf.make(t)
			_, h2 := build(t, store2, root)
			if h1 != h2 {
				t.Errorf("CanonicalHashAll drift after rebuild with category tree: %s -> %s", h1, h2)
			}

			// Mutate the parents → hash moves.
			writeBrief(t, root, "team/.categories/sales.md",
				"---\nparent_categories: [revenue]\n---\n# Sales\n")
			store3 := sf.make(t)
			_, h3 := build(t, store3, root)
			if h1 == h3 {
				t.Error("CanonicalHashAll unchanged after parent mutation (category_parents not covered)")
			}
		})
	}
}

func TestWikiIndex_CategoryTreeHashBackendParity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBrief(t, root, "team/.categories/sales.md",
		"---\nparent_categories: [revenue, gtm]\n---\n# Sales\n")
	writeBrief(t, root, "team/companies/acme.md",
		"---\nkind: company\ncategories: [sales]\n---\n# Acme\n")

	mem := newInMemoryFactStore()
	idxMem := NewWikiIndex(root, WithFactStore(mem))
	if err := idxMem.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile mem: %v", err)
	}
	hMem, _ := idxMem.CanonicalHashAll(ctx)

	sq, err := NewSQLiteFactStore(filepath.Join(t.TempDir(), "tree.sqlite"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	defer func() { _ = sq.Close() }()
	idxSq := NewWikiIndex(root, WithFactStore(sq))
	if err := idxSq.ReconcileFromMarkdown(ctx); err != nil {
		t.Fatalf("reconcile sqlite: %v", err)
	}
	hSq, _ := idxSq.CanonicalHashAll(ctx)

	if hMem != hSq {
		t.Errorf("CanonicalHashAll backend mismatch: mem %s != sqlite %s", hMem, hSq)
	}
}

func TestCatalogExcludesCategoryPages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := t.TempDir()
	repo := NewRepoAt(root, filepath.Join(t.TempDir(), "bak"))
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/.categories/sales.md",
		"---\nparent_categories: [revenue]\n---\n# Sales\n", "create", "add cat"); err != nil {
		t.Fatalf("commit cat page: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/companies/acme.md", "# Acme\n", "create", "add acme"); err != nil {
		t.Fatalf("commit acme: %v", err)
	}

	entries, err := repo.BuildCatalog(ctx, "", nil, false)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	for _, e := range entries {
		if e.Path == "team/.categories/sales.md" {
			t.Fatalf("category page leaked into catalog: %+v", entries)
		}
	}
}
