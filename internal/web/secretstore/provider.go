package secretstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type purposeContextKey struct{}

var (
	ErrNotFound           = errors.New("secret is not configured")
	ErrUnavailable        = errors.New("secret provider is unavailable")
	ErrPermissionRequired = errors.New("secret provider requires permission")
	ErrReadOnly           = errors.New("secret provider is read-only")
	ErrUnknownProvider    = errors.New("unknown secret provider")
)

type Purpose string

const (
	PurposeConnectionValidation Purpose = "connection_validation"
	PurposeInspect              Purpose = "inspect"
	PurposeQuery                Purpose = "query"
	PurposeMaterialize          Purpose = "materialize"
	PurposeScheduleValidation   Purpose = "schedule_validation"
	PurposeScheduledRun         Purpose = "scheduled_run"
	PurposeNotebookQuery        Purpose = "notebook_query"
	PurposePythonInjection      Purpose = "python_injection"
	PurposeSecretAdministration Purpose = "secret_administration"
)

func (p Purpose) Valid() bool {
	switch p {
	case PurposeConnectionValidation,
		PurposeInspect,
		PurposeQuery,
		PurposeMaterialize,
		PurposeScheduleValidation,
		PurposeScheduledRun,
		PurposeNotebookQuery,
		PurposePythonInjection,
		PurposeSecretAdministration:
		return true
	default:
		return false
	}
}

func WithPurpose(ctx context.Context, purpose Purpose) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !purpose.Valid() {
		return ctx
	}
	return context.WithValue(ctx, purposeContextKey{}, purpose)
}

func PurposeFromContext(ctx context.Context, fallback Purpose) Purpose {
	if ctx != nil {
		if purpose, ok := ctx.Value(purposeContextKey{}).(Purpose); ok && purpose.Valid() {
			return purpose
		}
	}
	if fallback.Valid() {
		return fallback
	}
	return PurposeQuery
}

type ResolveRequest struct {
	ProjectID   string
	Environment string
	Reference   Ref
	Purpose     Purpose
	RunID       string
}

func (r ResolveRequest) validate() error {
	if err := r.Reference.Validate(); err != nil {
		return err
	}
	if !r.Purpose.Valid() {
		return fmt.Errorf("invalid secret resolution purpose %q", r.Purpose)
	}
	return nil
}

type PutRequest struct {
	ProjectID   string
	Environment string
	Reference   Ref
	Value       []byte
	Purpose     Purpose
}

type DeleteRequest struct {
	ProjectID   string
	Environment string
	Reference   Ref
	Purpose     Purpose
}

type StatusState string

const (
	StatusConfigured         StatusState = "configured"
	StatusMissing            StatusState = "missing"
	StatusUnavailable        StatusState = "unavailable"
	StatusPermissionRequired StatusState = "permission_required"
)

type Status struct {
	State     StatusState
	Provider  string
	Reference string
	Writable  bool
	Rotatable bool
	VersionID string
}

type Lease interface {
	Bytes() []byte
	VersionID() string
	ExpiresAt() time.Time
	Close(context.Context) error
}

type Provider interface {
	Name() string
	Stat(context.Context, ResolveRequest) (Status, error)
	Resolve(context.Context, ResolveRequest) (Lease, error)
	Put(context.Context, PutRequest) (Status, error)
	Delete(context.Context, DeleteRequest) error
}

type memoryLease struct {
	value     []byte
	versionID string
	expiresAt time.Time
	closed    bool
}

func newMemoryLease(value []byte) *memoryLease {
	return &memoryLease{value: append([]byte(nil), value...)}
}

func (l *memoryLease) Bytes() []byte {
	if l == nil || l.closed {
		return nil
	}
	return l.value
}

func (l *memoryLease) VersionID() string {
	if l == nil {
		return ""
	}
	return l.versionID
}

func (l *memoryLease) ExpiresAt() time.Time {
	if l == nil {
		return time.Time{}
	}
	return l.expiresAt
}

func (l *memoryLease) Close(context.Context) error {
	if l == nil || l.closed {
		return nil
	}
	for index := range l.value {
		l.value[index] = 0
	}
	l.value = nil
	l.closed = true
	return nil
}
