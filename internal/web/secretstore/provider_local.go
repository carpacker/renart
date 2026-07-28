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

type credentialStoreProbeState uint8

const (
	credentialStoreProbeUnknown credentialStoreProbeState = iota
	credentialStoreProbeConfigured
	credentialStoreProbeMissing
	credentialStoreProbePermissionRequired
)

type credentialStoreProbe interface {
	Probe(context.Context, string, string) (credentialStoreProbeState, error)
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

func (p *LocalProvider) Stat(ctx context.Context, request ResolveRequest) (Status, error) {
	if err := validateLocalRequest(request); err != nil {
		return Status{}, err
	}
	if state, probed, err := p.probe(ctx, request); probed {
		return localStatus(request, state, err)
	}
	_, err := p.store.Get(localServiceName(request), request.Reference.Key)
	switch {
	case err == nil:
		return localStatus(request, credentialStoreProbeConfigured, nil)
	case errors.Is(err, keyring.ErrNotFound):
		return localStatus(request, credentialStoreProbeMissing, nil)
	default:
		return localStatus(request, credentialStoreProbeUnknown, localCredentialStoreUnavailable())
	}
}

func (p *LocalProvider) Resolve(ctx context.Context, request ResolveRequest) (Lease, error) {
	if err := validateLocalRequest(request); err != nil {
		return nil, err
	}
	if state, probed, err := p.probe(ctx, request); probed {
		if err != nil {
			return nil, err
		}
		if state == credentialStoreProbeMissing {
			return nil, fmt.Errorf("%w: local credential %s", ErrNotFound, request.Reference.Key)
		}
	}
	value, err := p.store.Get(localServiceName(request), request.Reference.Key)
	switch {
	case err == nil:
		return newMemoryLease([]byte(value)), nil
	case errors.Is(err, keyring.ErrNotFound):
		return nil, fmt.Errorf("%w: local credential %s", ErrNotFound, request.Reference.Key)
	default:
		return nil, localCredentialStoreUnavailable()
	}
}

func (p *LocalProvider) Put(ctx context.Context, request PutRequest) (Status, error) {
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
	if _, probed, err := p.probe(ctx, resolveRequest); probed && err != nil {
		return Status{}, err
	}
	if err := p.store.Set(localServiceName(resolveRequest), request.Reference.Key, string(request.Value)); err != nil {
		return Status{}, localCredentialStoreUnavailable()
	}
	return Status{
		State:     StatusConfigured,
		Provider:  p.Name(),
		Reference: request.Reference.String(),
		Writable:  true,
		Rotatable: true,
	}, nil
}

func (p *LocalProvider) Delete(ctx context.Context, request DeleteRequest) error {
	resolveRequest := ResolveRequest{
		ProjectID:   request.ProjectID,
		Environment: request.Environment,
		Reference:   request.Reference,
		Purpose:     request.Purpose,
	}
	if err := validateLocalRequest(resolveRequest); err != nil {
		return err
	}
	if state, probed, err := p.probe(ctx, resolveRequest); probed {
		if err != nil {
			return err
		}
		if state == credentialStoreProbeMissing {
			return nil
		}
	}
	err := p.store.Delete(localServiceName(resolveRequest), request.Reference.Key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return localCredentialStoreUnavailable()
	}
	return nil
}

func (p *LocalProvider) probe(
	ctx context.Context,
	request ResolveRequest,
) (credentialStoreProbeState, bool, error) {
	probe, ok := p.store.(credentialStoreProbe)
	if !ok {
		return credentialStoreProbeUnknown, false, nil
	}
	state, err := probe.Probe(ctx, localServiceName(request), request.Reference.Key)
	if err != nil {
		return credentialStoreProbeUnknown, true, localCredentialStoreUnavailable()
	}
	if state == credentialStoreProbePermissionRequired {
		return state, true, localCredentialStorePermissionRequired()
	}
	if state != credentialStoreProbeConfigured && state != credentialStoreProbeMissing {
		return credentialStoreProbeUnknown, true, localCredentialStoreUnavailable()
	}
	return state, true, nil
}

func localStatus(
	request ResolveRequest,
	state credentialStoreProbeState,
	err error,
) (Status, error) {
	status := Status{
		Provider:  request.Reference.Provider,
		Reference: request.Reference.String(),
	}
	switch state {
	case credentialStoreProbeConfigured:
		status.State = StatusConfigured
		status.Writable = true
		status.Rotatable = true
	case credentialStoreProbeMissing:
		status.State = StatusMissing
		status.Writable = true
		status.Rotatable = true
	case credentialStoreProbePermissionRequired:
		status.State = StatusPermissionRequired
	default:
		status.State = StatusUnavailable
	}
	return status, err
}

func localCredentialStorePermissionRequired() error {
	return fmt.Errorf(
		"%w: unlock the system credential store in this user session or use an environment reference",
		ErrPermissionRequired,
	)
}

func localCredentialStoreUnavailable() error {
	return fmt.Errorf(
		"%w: start a system credential service or use an environment reference on headless systems",
		ErrUnavailable,
	)
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
