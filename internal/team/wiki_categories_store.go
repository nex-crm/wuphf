package team

// wiki_categories_store.go — the in-memory FactStore implementation of the
// category derived indexes (article→category memberships + category→parent
// tree edges) plus the WikiIndex passthroughs that expose them. Split out of
// wiki_index.go to keep that file within the file-size budget; the SQLite
// implementation of the same methods lives in wiki_index_sqlite.go.

import (
	"context"
	"sort"
)

// --- WikiIndex passthroughs --------------------------------------------------

// ListAllCategories returns every category slug with its article count, sorted
// by slug. Passthrough to the FactStore; backs GET /wiki/categories.
func (w *WikiIndex) ListAllCategories(ctx context.Context) ([]CategoryCount, error) {
	return w.store.ListAllCategories(ctx)
}

// ListArticlesInCategory returns the wiki-root-relative paths of every article
// filed under a category slug, sorted. Passthrough to the FactStore; backs
// GET /wiki/categories/{slug}.
func (w *WikiIndex) ListArticlesInCategory(ctx context.Context, category string) ([]string, error) {
	return w.store.ListArticlesInCategory(ctx, category)
}

// ListAllCategoryParents returns every category→parent edge (the subcategory
// tree). Passthrough to the FactStore; backs GET /wiki/categories tree.
func (w *WikiIndex) ListAllCategoryParents(ctx context.Context) ([]CategoryParent, error) {
	return w.store.ListAllCategoryParents(ctx)
}

// ListCategoryParents returns a single category's parents. Passthrough.
func (w *WikiIndex) ListCategoryParents(ctx context.Context, category string) ([]string, error) {
	return w.store.ListCategoryParents(ctx, category)
}

// --- inMemoryFactStore: article_categories + category_parents ---------------

func (s *inMemoryFactStore) UpsertArticleCategories(_ context.Context, articlePath string, categories []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(categories) == 0 {
		delete(s.articleCats, articlePath)
		return nil
	}
	set := make(map[string]bool, len(categories))
	for _, c := range categories {
		if c != "" {
			set[c] = true
		}
	}
	if len(set) == 0 {
		delete(s.articleCats, articlePath)
		return nil
	}
	s.articleCats[articlePath] = set
	return nil
}

func (s *inMemoryFactStore) ListArticlesInCategory(_ context.Context, category string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for path, set := range s.articleCats {
		if set[category] {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *inMemoryFactStore) ListCategoriesForArticle(_ context.Context, articlePath string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.articleCats[articlePath]
	if len(set) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

func (s *inMemoryFactStore) ListAllCategories(_ context.Context) ([]CategoryCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := map[string]int{}
	for _, set := range s.articleCats {
		for c := range set {
			counts[c]++
		}
	}
	out := make([]CategoryCount, 0, len(counts))
	for slug, n := range counts {
		out = append(out, CategoryCount{Slug: slug, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (s *inMemoryFactStore) UpsertCategoryParents(_ context.Context, category string, parents []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := make(map[string]bool, len(parents))
	for _, p := range parents {
		if p != "" {
			set[p] = true
		}
	}
	if len(set) == 0 {
		delete(s.categoryParents, category)
		return nil
	}
	s.categoryParents[category] = set
	return nil
}

func (s *inMemoryFactStore) ListCategoryParents(_ context.Context, category string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.categoryParents[category]
	if len(set) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (s *inMemoryFactStore) ListAllCategoryParents(_ context.Context) ([]CategoryParent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []CategoryParent
	for cat, set := range s.categoryParents {
		for p := range set {
			out = append(out, CategoryParent{Category: cat, Parent: p})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Parent < out[j].Parent
	})
	return out, nil
}
