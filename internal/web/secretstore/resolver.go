package secretstore

import (
	"context"
	"fmt"
	"sort"
)

type Resolver struct {
	providers map[string]Provider
}

func NewResolver(providers ...Provider) (*Resolver, error) {
	result := &Resolver{providers: make(map[string]Provider, len(providers))}
	for _, provider := range providers {
		if provider == nil || provider.Name() == "" {
			return nil, fmt.Errorf("secret provider name is required")
		}
		if _, exists := result.providers[provider.Name()]; exists {
			return nil, fmt.Errorf("secret provider %q is registered twice", provider.Name())
		}
		result.providers[provider.Name()] = provider
	}
	return result, nil
}

func NewDefaultResolver() *Resolver {
	return NewDefaultResolverWithLocalVault(nil)
}

func NewDefaultResolverWithLocalVault(vault *LocalVaultProvider) *Resolver {
	if vault == nil {
		vault = NewLocalVaultProvider("")
	}
	resolver, err := NewResolver(NewEnvironmentProvider(), NewLocalProvider(), vault)
	if err != nil {
		panic(err)
	}
	return resolver
}

func (r *Resolver) provider(ref Ref) (Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, ref.Provider)
	}
	provider := r.providers[ref.Provider]
	if provider == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, ref.Provider)
	}
	return provider, nil
}

func (r *Resolver) Stat(ctx context.Context, request ResolveRequest) (Status, error) {
	provider, err := r.provider(request.Reference)
	if err != nil {
		return Status{}, err
	}
	return provider.Stat(ctx, request)
}

func (r *Resolver) Resolve(ctx context.Context, request ResolveRequest) (Lease, error) {
	provider, err := r.provider(request.Reference)
	if err != nil {
		return nil, err
	}
	return provider.Resolve(ctx, request)
}

func (r *Resolver) Put(ctx context.Context, request PutRequest) (Status, error) {
	provider, err := r.provider(request.Reference)
	if err != nil {
		return Status{}, err
	}
	return provider.Put(ctx, request)
}

func (r *Resolver) Delete(ctx context.Context, request DeleteRequest) error {
	provider, err := r.provider(request.Reference)
	if err != nil {
		return err
	}
	return provider.Delete(ctx, request)
}

type NamedRequest struct {
	Name    string
	Request ResolveRequest
}

type Bundle struct {
	leases map[string]Lease
	order  []string
}

func (r *Resolver) ResolveAll(ctx context.Context, requests []NamedRequest) (*Bundle, error) {
	bundle := &Bundle{leases: make(map[string]Lease, len(requests))}
	requests = append([]NamedRequest(nil), requests...)
	sort.SliceStable(requests, func(i, j int) bool {
		if requests[i].Request.Reference.Provider != requests[j].Request.Reference.Provider {
			return requests[i].Request.Reference.Provider < requests[j].Request.Reference.Provider
		}
		return requests[i].Name < requests[j].Name
	})
	for _, named := range requests {
		if named.Name == "" {
			_ = bundle.Close(ctx)
			return nil, fmt.Errorf("resolved secret name is required")
		}
		if _, exists := bundle.leases[named.Name]; exists {
			_ = bundle.Close(ctx)
			return nil, fmt.Errorf("resolved secret name %q is duplicated", named.Name)
		}
		lease, err := r.Resolve(ctx, named.Request)
		if err != nil {
			_ = bundle.Close(ctx)
			return nil, fmt.Errorf("resolve secret %q: %w", named.Name, err)
		}
		bundle.leases[named.Name] = lease
		bundle.order = append(bundle.order, named.Name)
	}
	return bundle, nil
}

func (b *Bundle) Value(name string) []byte {
	if b == nil || b.leases[name] == nil {
		return nil
	}
	return b.leases[name].Bytes()
}

func (b *Bundle) RedactionValues() []string {
	if b == nil {
		return nil
	}
	values := make([]string, 0, len(b.leases))
	for _, name := range b.order {
		if value := b.Value(name); len(value) > 0 {
			values = append(values, string(value))
		}
	}
	return values
}

func (b *Bundle) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	var firstErr error
	for index := len(b.order) - 1; index >= 0; index-- {
		name := b.order[index]
		if lease := b.leases[name]; lease != nil {
			if err := lease.Close(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		delete(b.leases, name)
	}
	b.order = nil
	return firstErr
}
