package imagegen

import (
	"context"
	"fmt"
	"sync"
)

// Registry holds the set of registered providers. There's one process-wide
// instance; providers self-register via init().
var (
	registryMu sync.RWMutex
	registry   = map[Kind]Provider{}
)

// Register installs a provider. Called from each backend's init() function.
// Panics on duplicate so the wiring bug surfaces at start, not at runtime.
func Register(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[p.Kind()]; ok {
		panic(fmt.Sprintf("imagegen: kind %q already registered", p.Kind()))
	}
	registry[p.Kind()] = p
}

// Get returns the provider for a kind, or false if not registered.
func Get(k Kind) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[k]
	return p, ok
}

// AllStatuses returns Status() for every registered provider, in stable order.
func AllStatuses(ctx context.Context) []Status {
	out := make([]Status, 0, len(AllKinds()))
	for _, k := range AllKinds() {
		if p, ok := Get(k); ok {
			out = append(out, p.Status(ctx))
		}
	}
	return out
}

// Generate dispatches to the named provider. Returns a clear error when the
// provider is unknown OR when its implementation is a stub.
func Generate(ctx context.Context, kind Kind, req Request) (Result, error) {
	p, ok := Get(kind)
	if !ok {
		return Result{}, fmt.Errorf("imagegen: provider %q is not registered", kind)
	}
	return p.Generate(ctx, req)
}
