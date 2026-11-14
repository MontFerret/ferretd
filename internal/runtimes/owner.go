package runtimes

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MontFerret/api"
	wireclient "github.com/MontFerret/wire/client"

	"github.com/MontFerret/ferretd/internal/lifecycle"
)

type (
	// Owner owns one native root and every admitted Wire acquisition. Callers must
	// settle borrowed-runtime children before releasing bindings or closing Owner.
	Owner struct {
		catalog *Catalog
		native  api.Runtime
		connect func(context.Context, string, ...wireclient.Option) (api.Runtime, error)

		// mu orders admission, cancellation registration, publication, and release
		// settlement. No constructor, cancellation callback, or root close runs here.
		mu         sync.Mutex
		gate       lifecycle.Gate
		pending    map[*acquisition]struct{}
		bindings   map[*Binding]struct{}
		cleanupErr error
	}

	acquisition struct {
		cancel context.CancelFunc
	}
)

// NewOwner borrows the immutable catalog and takes ownership of native only on
// success. On failure the caller remains responsible for the native root.
func NewOwner(catalog *Catalog, native api.Runtime) (*Owner, error) {
	if catalog == nil {
		return nil, errors.New("runtime owner: nil catalog")
	}

	if native == nil {
		return nil, errors.New("runtime owner: nil native runtime")
	}

	return &Owner{
		catalog:  catalog,
		native:   native,
		connect:  wireclient.New,
		pending:  make(map[*acquisition]struct{}),
		bindings: make(map[*Binding]struct{}),
	}, nil
}

// Acquire resolves a name (or the catalog default) and supplies a borrowed
// runtime. Wire startup is bounded by the earlier caller/configured deadline;
// detached constructor rollback may outlast it. After publication only explicit
// release or owner shutdown controls the binding's lifetime.
func (o *Owner) Acquire(ctx context.Context, name string) (*Binding, error) {
	if ctx == nil {
		return nil, errNilContext
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	definition, err := o.catalog.Resolve(name)
	if err != nil {
		return nil, fmt.Errorf("acquire runtime: %w", err)
	}

	if definition.Provider == ProviderNative {
		o.mu.Lock()
		defer o.mu.Unlock()

		if err := o.publicationError(ctx); err != nil {
			return nil, fmt.Errorf("acquire runtime %q: %w", definition.Name, err)
		}

		return newBinding(o, definition, o.native), nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, definition.ConnectTimeout)
	defer cancel()

	creation := &acquisition{cancel: cancel}
	if err := o.admit(creation); err != nil {
		return nil, fmt.Errorf("acquire runtime %q: %w", definition.Name, err)
	}

	defer o.finishAcquisition(creation)

	var options []wireclient.Option

	if definition.Insecure {
		options = append(options, wireclient.WithInsecure())
	}

	remote, err := o.connect(connectCtx, definition.target, options...)
	if err != nil {
		if remote != nil {
			err = errors.Join(err, o.rollback(definition.Name, remote))
		} else if joined, ok := err.(interface{ Unwrap() []error }); ok && len(joined.Unwrap()) > 1 {
			// Wire joins construction and detached cleanup errors when it cannot
			// return a runtime. Release cannot be acknowledged in this case.
			err = &RollbackError{Cause: err}
		}

		return nil, fmt.Errorf("connect runtime %q: %w", definition.Name, errors.Join(ErrRuntimeUnavailable, err))
	}

	if remote == nil {
		return nil, fmt.Errorf("%w: connect runtime %q: constructor returned no runtime", ErrRuntimeUnavailable, definition.Name)
	}

	o.mu.Lock()

	err = o.publicationError(connectCtx)
	if err == nil {
		binding := newBinding(o, definition, remote)
		o.bindings[binding] = struct{}{}
		o.mu.Unlock()

		return binding, nil
	}

	o.mu.Unlock()

	return nil, fmt.Errorf("acquire runtime %q: %w", definition.Name, errors.Join(err, o.rollback(definition.Name, remote)))
}

func (o *Owner) admit(creation *acquisition) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.gate.BeginCreate() {
		return ErrClosed
	}

	o.pending[creation] = struct{}{}

	return nil
}

func (o *Owner) finishAcquisition(creation *acquisition) {
	o.mu.Lock()
	defer o.mu.Unlock()

	delete(o.pending, creation)
	o.gate.EndCreate()
}

// publicationError runs under mu so shutdown and publication have one ordering.
func (o *Owner) publicationError(ctx context.Context) error {
	var closedErr error

	if !o.gate.Accepting() {
		closedErr = ErrClosed
	}

	return errors.Join(closedErr, ctx.Err())
}

func (o *Owner) rollback(name string, runtime api.Runtime) error {
	err := runtime.Close()
	if err == nil {
		return nil
	}

	err = &RollbackError{Cause: fmt.Errorf("release unpublished runtime %q: %w", name, err)}
	o.mu.Lock()
	o.cleanupErr = errors.Join(o.cleanupErr, err)
	o.mu.Unlock()

	return err
}

func (o *Owner) finishRelease(binding *Binding, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Publish before removing the binding. Shutdown either waits for this exact
	// release or sees its retained error after it has completely settled.
	if err != nil {
		o.cleanupErr = errors.Join(o.cleanupErr, err)
	}

	binding.close.Finish(err)
	delete(o.bindings, binding)
}

// Close commits shutdown once. Cancellation stops only this caller's wait;
// admitted construction, rollback, and root cleanup continue to settlement.
func (o *Owner) Close(ctx context.Context) error {
	if ctx == nil {
		return errNilContext
	}

	o.mu.Lock()
	started := o.gate.BeginClose()
	var pending []*acquisition

	if started {
		pending = make([]*acquisition, 0, len(o.pending))
		for creation := range o.pending {
			pending = append(pending, creation)
		}
	}

	o.mu.Unlock()

	if started {
		go o.settleClose(pending)
	}

	return o.gate.WaitClose(ctx)
}

func (o *Owner) settleClose(pending []*acquisition) {
	for _, creation := range pending {
		creation.cancel()
	}

	o.gate.WaitForCreates()
	o.mu.Lock()
	bindings := make([]*Binding, 0, len(o.bindings))
	for binding := range o.bindings {
		bindings = append(bindings, binding)
	}

	o.mu.Unlock()

	for _, binding := range bindings {
		// Release records its error before dropping owner membership.
		_ = binding.Release()
	}

	err := o.native.Close()
	if err != nil {
		err = fmt.Errorf("close native runtime %q: %w", LocalName, err)
	}

	o.mu.Lock()
	result := errors.Join(o.cleanupErr, err)
	o.mu.Unlock()
	o.gate.FinishClose(result)
}
