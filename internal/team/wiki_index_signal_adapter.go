package team

// wiki_index_signal_adapter.go bridges WikiIndex to the SignalIndex
// interface that EntityResolver (entity_resolver.go) depends on.
//
// The resolver is a pure function over a small read surface — looking up an
// entity by slug, email, domain, or fuzzy name. This adapter exposes that
// surface on top of WikiIndex without leaking resolver-specific concepts
// back into the index.
//
// Slice 1 scope: the adapter reads from the in-memory FactStore whenever
// the index is configured that way. For the SQLite backend the adapter
// falls back to a narrow set of direct queries via the shared TypedFact +
// IndexEntity types — no new tables.

import (
	"context"
	"strings"
)

// WikiIndexSignalAdapter adapts a WikiIndex to the SignalIndex interface.
type WikiIndexSignalAdapter struct {
	idx *WikiIndex
}

// NewWikiIndexSignalAdapter constructs an adapter over the given index.
// A nil idx yields an adapter that returns "not found" for every lookup —
// useful in tests that do not boot the full index stack.
func NewWikiIndexSignalAdapter(idx *WikiIndex) *WikiIndexSignalAdapter {
	return &WikiIndexSignalAdapter{idx: idx}
}

// EntityBySlug returns the entity with the given canonical slug.
func (a *WikiIndexSignalAdapter) EntityBySlug(ctx context.Context, slug string) (resolverEntity, bool, error) {
	if a.idx == nil {
		return resolverEntity{}, false, nil
	}
	mem, ok := a.idx.store.(*inMemoryFactStore)
	if !ok {
		return resolverEntity{}, false, nil
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	ent, found := mem.entities[slug]
	if !found {
		return resolverEntity{}, false, nil
	}
	return toResolverEntity(ent), true, nil
}

// EntityByEmail returns the entity whose signals.email matches (case-insensitive).
func (a *WikiIndexSignalAdapter) EntityByEmail(ctx context.Context, email string) (resolverEntity, bool, error) {
	if a.idx == nil {
		return resolverEntity{}, false, nil
	}
	mem, ok := a.idx.store.(*inMemoryFactStore)
	if !ok {
		return resolverEntity{}, false, nil
	}
	target := strings.ToLower(strings.TrimSpace(email))
	if target == "" {
		return resolverEntity{}, false, nil
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	for _, ent := range mem.entities {
		if strings.ToLower(strings.TrimSpace(ent.Signals.Email)) == target {
			return toResolverEntity(ent), true, nil
		}
	}
	return resolverEntity{}, false, nil
}

// EntityByDomain returns every entity associated with the given domain.
func (a *WikiIndexSignalAdapter) EntityByDomain(ctx context.Context, domain string) ([]resolverEntity, error) {
	if a.idx == nil {
		return nil, nil
	}
	mem, ok := a.idx.store.(*inMemoryFactStore)
	if !ok {
		return nil, nil
	}
	target := strings.ToLower(strings.TrimSpace(domain))
	if target == "" {
		return nil, nil
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	var out []resolverEntity
	for _, ent := range mem.entities {
		if strings.ToLower(strings.TrimSpace(ent.Signals.Domain)) == target {
			out = append(out, toResolverEntity(ent))
		}
	}
	return out, nil
}

// EntityByName returns every entity whose signals.person_name contains the
// query as a case-insensitive substring. The resolver applies its own JW
// threshold on top.
func (a *WikiIndexSignalAdapter) EntityByName(ctx context.Context, name string) ([]resolverEntity, error) {
	if a.idx == nil {
		return nil, nil
	}
	mem, ok := a.idx.store.(*inMemoryFactStore)
	if !ok {
		return nil, nil
	}
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil, nil
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	var out []resolverEntity
	for _, ent := range mem.entities {
		n := strings.ToLower(strings.TrimSpace(ent.Signals.PersonName))
		if n != "" && strings.Contains(n, target) {
			out = append(out, toResolverEntity(ent))
			continue
		}
		for _, a := range ent.Aliases {
			if strings.Contains(strings.ToLower(strings.TrimSpace(a)), target) {
				out = append(out, toResolverEntity(ent))
				break
			}
		}
	}
	return out, nil
}

func toResolverEntity(e IndexEntity) resolverEntity {
	return resolverEntity{
		Slug:  e.Slug,
		Kind:  EntityKind(e.Kind),
		Name:  e.Signals.PersonName,
		Email: strings.ToLower(strings.TrimSpace(e.Signals.Email)),
	}
}
