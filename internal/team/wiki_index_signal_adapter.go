package team

// wiki_index_signal_adapter.go bridges WikiIndex to the SignalIndex
// interface that EntityResolver (entity_resolver.go) depends on.
//
// The resolver is a pure function over a small read surface — looking up an
// entity by slug, email, domain, or fuzzy name. This adapter exposes that
// surface on top of WikiIndex without leaking resolver-specific concepts
// back into the index.
//
// Backend support:
//   - in-memory store: direct map lookups under RWMutex.
//   - SQLite (or any other FactStore that satisfies the interface): fall
//     through to FactStore.IterateEntities and filter in-process. Bounded
//     by the total entity count, which is small (~hundreds) for the
//     bench corpus. Slice 3 can add typed SELECTs if this becomes a hot path.

import (
	"context"
	"errors"
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
	if mem, ok := a.idx.store.(*inMemoryFactStore); ok {
		mem.mu.RLock()
		defer mem.mu.RUnlock()
		ent, found := mem.entities[slug]
		if !found {
			return resolverEntity{}, false, nil
		}
		return toResolverEntity(ent), true, nil
	}

	// Persistent backend: scan via the FactStore iterator. Bounded by
	// total entity count; acceptable at Slice 2 corpus sizes.
	var found IndexEntity
	var hit bool
	err := a.idx.store.IterateEntities(ctx, func(e IndexEntity) error {
		if e.Slug == slug {
			found = e
			hit = true
			return errStopIteration
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopIteration) {
		return resolverEntity{}, false, err
	}
	if !hit {
		return resolverEntity{}, false, nil
	}
	return toResolverEntity(found), true, nil
}

// EntityByEmail returns the entity whose signals.email matches (case-insensitive).
func (a *WikiIndexSignalAdapter) EntityByEmail(ctx context.Context, email string) (resolverEntity, bool, error) {
	if a.idx == nil {
		return resolverEntity{}, false, nil
	}
	target := strings.ToLower(strings.TrimSpace(email))
	if target == "" {
		return resolverEntity{}, false, nil
	}
	if mem, ok := a.idx.store.(*inMemoryFactStore); ok {
		mem.mu.RLock()
		defer mem.mu.RUnlock()
		for _, ent := range mem.entities {
			if strings.ToLower(strings.TrimSpace(ent.Signals.Email)) == target {
				return toResolverEntity(ent), true, nil
			}
		}
		return resolverEntity{}, false, nil
	}

	var found IndexEntity
	var hit bool
	err := a.idx.store.IterateEntities(ctx, func(e IndexEntity) error {
		if strings.ToLower(strings.TrimSpace(e.Signals.Email)) == target {
			found = e
			hit = true
			return errStopIteration
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopIteration) {
		return resolverEntity{}, false, err
	}
	if !hit {
		return resolverEntity{}, false, nil
	}
	return toResolverEntity(found), true, nil
}

// EntityByDomain returns every entity associated with the given domain.
func (a *WikiIndexSignalAdapter) EntityByDomain(ctx context.Context, domain string) ([]resolverEntity, error) {
	if a.idx == nil {
		return nil, nil
	}
	target := strings.ToLower(strings.TrimSpace(domain))
	if target == "" {
		return nil, nil
	}
	if mem, ok := a.idx.store.(*inMemoryFactStore); ok {
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

	var out []resolverEntity
	err := a.idx.store.IterateEntities(ctx, func(e IndexEntity) error {
		if strings.ToLower(strings.TrimSpace(e.Signals.Domain)) == target {
			out = append(out, toResolverEntity(e))
		}
		return nil
	})
	if err != nil {
		return nil, err
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
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil, nil
	}
	if mem, ok := a.idx.store.(*inMemoryFactStore); ok {
		mem.mu.RLock()
		defer mem.mu.RUnlock()
		var out []resolverEntity
		for _, ent := range mem.entities {
			if matchEntityName(ent, target) {
				out = append(out, toResolverEntity(ent))
			}
		}
		return out, nil
	}

	var out []resolverEntity
	err := a.idx.store.IterateEntities(ctx, func(e IndexEntity) error {
		if matchEntityName(e, target) {
			out = append(out, toResolverEntity(e))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// matchEntityName returns true iff target (already lowercased + trimmed) is
// a substring of e.Signals.PersonName or any alias.
func matchEntityName(e IndexEntity, target string) bool {
	n := strings.ToLower(strings.TrimSpace(e.Signals.PersonName))
	if n != "" && strings.Contains(n, target) {
		return true
	}
	for _, a := range e.Aliases {
		if strings.Contains(strings.ToLower(strings.TrimSpace(a)), target) {
			return true
		}
	}
	return false
}

func toResolverEntity(e IndexEntity) resolverEntity {
	return resolverEntity{
		Slug:  e.Slug,
		Kind:  EntityKind(e.Kind),
		Name:  e.Signals.PersonName,
		Email: strings.ToLower(strings.TrimSpace(e.Signals.Email)),
	}
}

// errStopIteration is returned from the FactStore.IterateEntities callback
// to short-circuit traversal after a match is found. Adapter callers treat
// it as a sentinel, not a real error.
//
// Callers MUST compare with errors.Is(err, errStopIteration) rather than
// err == errStopIteration, so that any future `fmt.Errorf("iterate: %w",
// errStopIteration)` wrapping continues to match and does not surface a
// spurious error to the caller. The typed stopIterationError defines an Is
// method so the wrapped comparison works regardless of wrapping depth.
var errStopIteration error = stopIterationError("stop")

type stopIterationError string

func (e stopIterationError) Error() string { return string(e) }

// Is reports whether target is the package-level stopIteration sentinel.
// This lets `errors.Is(wrapped, errStopIteration)` match even after callers
// wrap the sentinel via fmt.Errorf("%w", ...).
func (e stopIterationError) Is(target error) bool {
	_, ok := target.(stopIterationError)
	return ok
}
