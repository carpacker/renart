package secretstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

type credentialStore interface {
	Set(service, user, password string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type osCredentialStore struct{}

func (osCredentialStore) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (osCredentialStore) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (osCredentialStore) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

type LocalProvider struct {
	store credentialStore
}

func NewLocalProvider() *LocalProvider {
	return &LocalProvider{store: osCredentialStore{}}
}

func newLocalProviderWithStore(store credentialStore) *LocalProvider {
	return &LocalProvider{store: store}
}

func (p *LocalProvider) Name() string {
	return "local"
}

func (p *LocalProvider) Stat(_ context.Context, request ResolveRequest) (Status, error) {
	if err := validateLocalRequest(request); err != nil {
		return Status{}, err
	}
	_, err := p.store.Get(localServiceName(request), request.Reference.Key)
	status := Status{
		Provider:  p.Name(),
		Reference: request.Reference.String(),
		Writable:  true,
		Rotatable: true,
	}
	switch {
	case err == nil:
		status.State = StatusConfigured
		return status, nil
	case errors.Is(err, keyring.ErrNotFound):
		status.State = StatusMissing
		return status, nil
	default:
		status.State = StatusUnavailable
		return status, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
}

func (p *LocalProvider) Resolve(_ context.Context, request ResolveRequest) (Lease, error) {
	if err := validateLocalRequest(request); err != nil {
		return nil, err
	}
	value, err := p.store.Get(localServiceName(request), request.Reference.Key)
	switch {
	case err == nil:
		return newMemoryLease([]byte(value)), nil
	case errors.Is(err, keyring.ErrNotFound):
		return nil, fmt.Errorf("%w: local credential %s", ErrNotFound, request.Reference.Key)
	default:
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
}

func (p *LocalProvider) Put(_ context.Context, request PutRequest) (Status, error) {
	resolveRequest := ResolveRequest{
		ProjectID:   request.ProjectID,
		Environment: request.Environment,
		Reference:   request.Reference,
		Purpose:     request.Purpose,
	}
	if err := validateLocalRequest(resolveRequest); err != nil {
		return Status{}, err
	}
	if len(request.Value) == 0 {
		return Status{}, fmt.Errorf("secret value is required")
	}
	if err := p.store.Set(localServiceName(resolveRequest), request.Reference.Key, string(request.Value)); err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return Status{
		State:     StatusConfigured,
		Provider:  p.Name(),
		Reference: request.Reference.String(),
		Writable:  true,
		Rotatable: true,
	}, nil
}

func (p *LocalProvider) Delete(_ context.Context, request DeleteRequest) error {
	resolveRequest := ResolveRequest{
		ProjectID:   request.ProjectID,
		Environment: request.Environment,
		Reference:   request.Reference,
		Purpose:     request.Purpose,
	}
	if err := validateLocalRequest(resolveRequest); err != nil {
		return err
	}
	err := p.store.Delete(localServiceName(resolveRequest), request.Reference.Key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

func validateLocalRequest(request ResolveRequest) error {
	if err := validateProviderRequest(request, "local"); err != nil {
		return err
	}
	if strings.TrimSpace(request.ProjectID) == "" {
		return fmt.Errorf("project ID is required for local secrets")
	}
	if strings.TrimSpace(request.Environment) == "" {
		return fmt.Errorf("environment is required for local secrets")
	}
	return nil
}

func localServiceName(request ResolveRequest) string {
	return "renart/" + strings.TrimSpace(request.ProjectID) + "/" + strings.TrimSpace(request.Environment)
}
