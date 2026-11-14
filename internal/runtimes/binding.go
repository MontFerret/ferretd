package runtimes

import (
	"context"
	"fmt"

	"github.com/MontFerret/api"
	wireclient "github.com/MontFerret/wire/client"

	"github.com/MontFerret/ferretd/internal/lifecycle"
)

// Binding supplies a borrowed canonical runtime and owns its release obligation.
// A Binding must not be copied. Outputs, sessions/debuggers, and plans must settle
// before Release; closing a Wire root does not close caller-owned descendants.
type Binding struct {
	owner    *Owner
	name     string
	provider Provider
	runtime  api.Runtime
	owned    bool
	close    lifecycle.CloseOperation
}

func newBinding(owner *Owner, definition Definition, runtime api.Runtime) *Binding {
	return &Binding{owner: owner, name: definition.Name, provider: definition.Provider, runtime: runtime, owned: definition.Provider == ProviderWire}
}

// Name returns the exact stable name resolved by the catalog.
func (b *Binding) Name() string {
	return b.name
}

// Provider identifies the implementation selected by this binding's definition.
func (b *Binding) Provider() Provider {
	return b.provider
}

// Runtime returns the borrowed runtime. The caller must not close it directly.
func (b *Binding) Runtime() api.Runtime {
	return b.runtime
}

// ConnectionLifetime returns this acquisition's optional Wire connection observer.
func (b *Binding) ConnectionLifetime() wireclient.ConnectionLifetime {
	if b.provider != ProviderWire {
		return nil
	}

	lifetime, _ := b.runtime.(wireclient.ConnectionLifetime)

	return lifetime
}

// Release settles this binding exactly once and retains its result. Native
// release leaves the shared root open; Wire release closes only this acquisition.
func (b *Binding) Release() error {
	if b.close.Begin() {
		var err error

		if b.owned {
			err = b.runtime.Close()
			if err != nil {
				err = fmt.Errorf("release runtime %q: %w", b.name, err)
			}
		}

		b.owner.finishRelease(b, err)
	}

	return b.close.Wait(context.Background())
}
