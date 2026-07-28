package secretstore

import (
	"context"
	"fmt"
	"os"
)

type EnvironmentProvider struct {
	lookup func(string) (string, bool)
}

func NewEnvironmentProvider() *EnvironmentProvider {
	return &EnvironmentProvider{lookup: os.LookupEnv}
}

func (p *EnvironmentProvider) Name() string {
	return "env"
}

func (p *EnvironmentProvider) Stat(_ context.Context, request ResolveRequest) (Status, error) {
	if err := validateProviderRequest(request, p.Name()); err != nil {
		return Status{}, err
	}
	_, found := p.lookup(request.Reference.Key)
	state := StatusMissing
	if found {
		state = StatusConfigured
	}
	return Status{
		State:     state,
		Provider:  p.Name(),
		Reference: request.Reference.String(),
		Writable:  false,
		Rotatable: false,
	}, nil
}

func (p *EnvironmentProvider) Resolve(_ context.Context, request ResolveRequest) (Lease, error) {
	if err := validateProviderRequest(request, p.Name()); err != nil {
		return nil, err
	}
	value, found := p.lookup(request.Reference.Key)
	if !found {
		return nil, fmt.Errorf("%w: environment variable %s", ErrNotFound, request.Reference.Key)
	}
	return newMemoryLease([]byte(value)), nil
}

func (p *EnvironmentProvider) Put(context.Context, PutRequest) (Status, error) {
	return Status{}, ErrReadOnly
}

func (p *EnvironmentProvider) Delete(context.Context, DeleteRequest) error {
	return ErrReadOnly
}

func validateProviderRequest(request ResolveRequest, provider string) error {
	if err := request.validate(); err != nil {
		return err
	}
	if request.Reference.Provider != provider {
		return fmt.Errorf("provider %q cannot resolve %s references", provider, request.Reference.Provider)
	}
	return nil
}
