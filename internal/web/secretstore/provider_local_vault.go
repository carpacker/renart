package secretstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"filippo.io/age"
	"github.com/gofrs/flock"
)

const (
	localVaultProviderName = "local-vault"
	localVaultVersion      = 1
	localVaultWorkFactor   = 18
	maxLocalVaultBytes     = 16 << 20
	maxLocalVaultEntries   = 4096
	maxLocalVaultValue     = 1 << 20
	maxVaultPassphrase     = 1024
	minVaultPassphrase     = 12
)

var (
	ErrVaultNotInitialized     = errors.New("local secret vault is not initialized")
	ErrVaultAlreadyInitialized = errors.New("local secret vault is already initialized")
	ErrVaultInvalidPassphrase  = errors.New("local secret vault passphrase is incorrect")
)

type LocalVaultState string

const (
	LocalVaultUninitialized LocalVaultState = "uninitialized"
	LocalVaultLocked        LocalVaultState = "locked"
	LocalVaultUnlocked      LocalVaultState = "unlocked"
	LocalVaultUnavailable   LocalVaultState = "unavailable"
)

type LocalVaultStatus struct {
	State       LocalVaultState
	SecretCount int
	Message     string
}

type localVaultDocument struct {
	Version      int                          `json:"version"`
	ProjectID    string                       `json:"project_id"`
	Environments map[string]map[string][]byte `json:"environments,omitempty"`
}

type localVaultSession struct {
	passphrase  []byte
	document    localVaultDocument
	fingerprint [sha256.Size]byte
}

type LocalVaultProvider struct {
	directory    string
	directoryErr error
	workFactor   int

	mu       sync.Mutex
	sessions map[string]*localVaultSession
}

func NewLocalVaultProvider(directory string) *LocalVaultProvider {
	directory = strings.TrimSpace(directory)
	var directoryErr error
	if directory == "" {
		directory, directoryErr = DefaultLocalVaultDirectory()
	}
	return &LocalVaultProvider{
		directory:    directory,
		directoryErr: directoryErr,
		workFactor:   localVaultWorkFactor,
		sessions:     make(map[string]*localVaultSession),
	}
}

func newLocalVaultProviderForTests(directory string, workFactor int) *LocalVaultProvider {
	provider := NewLocalVaultProvider(directory)
	provider.workFactor = workFactor
	return provider
}

func DefaultLocalVaultDirectory() (string, error) {
	if override := strings.TrimSpace(os.Getenv("RENART_VAULT_DIR")); override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("resolve RENART_VAULT_DIR: %w", err)
		}
		return absolute, nil
	}
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(configDirectory, "renart", "vaults"), nil
}

func (p *LocalVaultProvider) Name() string {
	return localVaultProviderName
}

func (p *LocalVaultProvider) Status(projectID string) LocalVaultStatus {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return LocalVaultStatus{
			State:   LocalVaultUnavailable,
			Message: "The project has no stable identity, so its encrypted vault cannot be located.",
		}
	}
	if p == nil || p.directoryErr != nil || strings.TrimSpace(p.directory) == "" {
		return LocalVaultStatus{
			State:   LocalVaultUnavailable,
			Message: "Renart could not locate the user configuration directory for the encrypted vault.",
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if session := p.sessions[projectID]; session != nil {
		return LocalVaultStatus{
			State:       LocalVaultUnlocked,
			SecretCount: countLocalVaultSecrets(session.document),
			Message:     "Unlocked for this Renart process. The vault locks when Renart stops.",
		}
	}
	if _, err := os.Stat(p.vaultPath(projectID)); err == nil {
		return LocalVaultStatus{
			State:   LocalVaultLocked,
			Message: "Unlock the encrypted vault to use or update its connection credentials.",
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return LocalVaultStatus{
			State:   LocalVaultUnavailable,
			Message: "The encrypted vault cannot be inspected in the user configuration directory.",
		}
	}
	return LocalVaultStatus{
		State:   LocalVaultUninitialized,
		Message: "Set up a passphrase-protected vault to store credentials without a system credential service.",
	}
}

func (p *LocalVaultProvider) Initialize(
	ctx context.Context,
	projectID string,
	passphrase []byte,
) error {
	if err := validateVaultProjectID(projectID); err != nil {
		return err
	}
	if err := validateVaultPassphrase(passphrase); err != nil {
		return err
	}
	if err := p.available(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	unlockFile, err := p.lockProject(ctx, projectID)
	if err != nil {
		return err
	}
	defer unlockFile()

	path := p.vaultPath(projectID)
	if _, err := os.Stat(path); err == nil {
		return ErrVaultAlreadyInitialized
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect encrypted vault: %v", ErrUnavailable, err)
	}

	document := newLocalVaultDocument(projectID)
	fingerprint, err := p.writeDocument(path, document, passphrase)
	if err != nil {
		clearLocalVaultDocument(&document)
		return err
	}
	p.replaceSession(projectID, &localVaultSession{
		passphrase:  append([]byte(nil), passphrase...),
		document:    document,
		fingerprint: fingerprint,
	})
	return nil
}

func (p *LocalVaultProvider) Unlock(
	ctx context.Context,
	projectID string,
	passphrase []byte,
) error {
	if err := validateVaultProjectID(projectID); err != nil {
		return err
	}
	if len(passphrase) == 0 || len(passphrase) > maxVaultPassphrase {
		return ErrVaultInvalidPassphrase
	}
	if err := p.available(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	unlockFile, err := p.lockProject(ctx, projectID)
	if err != nil {
		return err
	}
	defer unlockFile()

	ciphertext, err := readLocalVaultFile(p.vaultPath(projectID))
	if errors.Is(err, os.ErrNotExist) {
		return ErrVaultNotInitialized
	}
	if err != nil {
		return fmt.Errorf("%w: read encrypted vault: %v", ErrUnavailable, err)
	}
	document, err := decryptLocalVault(ciphertext, passphrase)
	if errors.Is(err, age.ErrIncorrectIdentity) {
		return ErrVaultInvalidPassphrase
	}
	if err != nil {
		return fmt.Errorf("%w: encrypted vault could not be opened", ErrUnavailable)
	}
	if err := validateLocalVaultDocument(document, projectID); err != nil {
		clearLocalVaultDocument(&document)
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	p.replaceSession(projectID, &localVaultSession{
		passphrase:  append([]byte(nil), passphrase...),
		document:    document,
		fingerprint: sha256.Sum256(ciphertext),
	})
	return nil
}

func (p *LocalVaultProvider) Lock(projectID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replaceSession(strings.TrimSpace(projectID), nil)
}

func (p *LocalVaultProvider) LockAll() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for projectID := range p.sessions {
		p.replaceSession(projectID, nil)
	}
}

func (p *LocalVaultProvider) ChangePassphrase(
	ctx context.Context,
	projectID string,
	passphrase []byte,
) error {
	if err := validateVaultProjectID(projectID); err != nil {
		return err
	}
	if err := validateVaultPassphrase(passphrase); err != nil {
		return err
	}
	if err := p.available(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	unlockFile, err := p.lockProject(ctx, projectID)
	if err != nil {
		return err
	}
	defer unlockFile()

	session, err := p.unlockedSession(projectID)
	if err != nil {
		return err
	}
	if err := p.refreshSession(projectID, session); err != nil {
		return err
	}
	fingerprint, err := p.writeDocument(p.vaultPath(projectID), session.document, passphrase)
	if err != nil {
		return err
	}
	clearBytes(session.passphrase)
	session.passphrase = append([]byte(nil), passphrase...)
	session.fingerprint = fingerprint
	return nil
}

func (p *LocalVaultProvider) Stat(
	ctx context.Context,
	request ResolveRequest,
) (Status, error) {
	if err := validateLocalVaultRequest(request); err != nil {
		return Status{}, err
	}
	status := Status{
		Provider:  p.Name(),
		Reference: request.Reference.String(),
	}
	if err := p.available(); err != nil {
		status.State = StatusUnavailable
		return status, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session := p.sessions[request.ProjectID]
	if session == nil {
		if _, err := os.Stat(p.vaultPath(request.ProjectID)); errors.Is(err, os.ErrNotExist) {
			status.State = StatusUnavailable
			return status, fmt.Errorf("%w: %w", ErrUnavailable, ErrVaultNotInitialized)
		}
		status.State = StatusPermissionRequired
		return status, localVaultLockedError()
	}
	unlockFile, err := p.lockProject(ctx, request.ProjectID)
	if err != nil {
		status.State = StatusUnavailable
		return status, err
	}
	defer unlockFile()
	if err := p.refreshSession(request.ProjectID, session); err != nil {
		status.State = vaultStatusStateForError(err)
		return status, err
	}
	status.Writable = true
	status.Rotatable = true
	if _, found := localVaultValue(session.document, request.Environment, request.Reference.Key); found {
		status.State = StatusConfigured
	} else {
		status.State = StatusMissing
	}
	return status, nil
}

func (p *LocalVaultProvider) Resolve(
	ctx context.Context,
	request ResolveRequest,
) (Lease, error) {
	if err := validateLocalVaultRequest(request); err != nil {
		return nil, err
	}
	if err := p.available(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.unlockedSession(request.ProjectID)
	if err != nil {
		return nil, err
	}
	unlockFile, err := p.lockProject(ctx, request.ProjectID)
	if err != nil {
		return nil, err
	}
	defer unlockFile()
	if err := p.refreshSession(request.ProjectID, session); err != nil {
		return nil, err
	}
	value, found := localVaultValue(session.document, request.Environment, request.Reference.Key)
	if !found {
		return nil, fmt.Errorf("%w: encrypted vault credential %s", ErrNotFound, request.Reference.Key)
	}
	return newMemoryLease(value), nil
}

func (p *LocalVaultProvider) Put(
	ctx context.Context,
	request PutRequest,
) (Status, error) {
	resolveRequest := ResolveRequest{
		ProjectID:   request.ProjectID,
		Environment: request.Environment,
		Reference:   request.Reference,
		Purpose:     request.Purpose,
	}
	if err := validateLocalVaultRequest(resolveRequest); err != nil {
		return Status{}, err
	}
	if len(request.Value) == 0 {
		return Status{}, errors.New("secret value is required")
	}
	if len(request.Value) > maxLocalVaultValue {
		return Status{}, fmt.Errorf("secret value exceeds the %d byte limit", maxLocalVaultValue)
	}
	if err := p.available(); err != nil {
		return Status{}, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.unlockedSession(request.ProjectID)
	if err != nil {
		return Status{}, err
	}
	unlockFile, err := p.lockProject(ctx, request.ProjectID)
	if err != nil {
		return Status{}, err
	}
	defer unlockFile()
	if err := p.refreshSession(request.ProjectID, session); err != nil {
		return Status{}, err
	}

	next := cloneLocalVaultDocument(session.document)
	environment := next.Environments[request.Environment]
	if environment == nil {
		environment = make(map[string][]byte)
		next.Environments[request.Environment] = environment
	}
	if _, exists := environment[request.Reference.Key]; !exists &&
		countLocalVaultSecrets(next) >= maxLocalVaultEntries {
		clearLocalVaultDocument(&next)
		return Status{}, fmt.Errorf("encrypted vault exceeds the %d entry limit", maxLocalVaultEntries)
	}
	environment[request.Reference.Key] = append([]byte(nil), request.Value...)
	fingerprint, err := p.writeDocument(p.vaultPath(request.ProjectID), next, session.passphrase)
	if err != nil {
		clearLocalVaultDocument(&next)
		return Status{}, err
	}
	clearLocalVaultDocument(&session.document)
	session.document = next
	session.fingerprint = fingerprint
	return Status{
		State:     StatusConfigured,
		Provider:  p.Name(),
		Reference: request.Reference.String(),
		Writable:  true,
		Rotatable: true,
	}, nil
}

func (p *LocalVaultProvider) Delete(
	ctx context.Context,
	request DeleteRequest,
) error {
	resolveRequest := ResolveRequest{
		ProjectID:   request.ProjectID,
		Environment: request.Environment,
		Reference:   request.Reference,
		Purpose:     request.Purpose,
	}
	if err := validateLocalVaultRequest(resolveRequest); err != nil {
		return err
	}
	if err := p.available(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.unlockedSession(request.ProjectID)
	if err != nil {
		return err
	}
	unlockFile, err := p.lockProject(ctx, request.ProjectID)
	if err != nil {
		return err
	}
	defer unlockFile()
	if err := p.refreshSession(request.ProjectID, session); err != nil {
		return err
	}
	if _, found := localVaultValue(session.document, request.Environment, request.Reference.Key); !found {
		return nil
	}

	next := cloneLocalVaultDocument(session.document)
	clearBytes(next.Environments[request.Environment][request.Reference.Key])
	delete(next.Environments[request.Environment], request.Reference.Key)
	if len(next.Environments[request.Environment]) == 0 {
		delete(next.Environments, request.Environment)
	}
	fingerprint, err := p.writeDocument(p.vaultPath(request.ProjectID), next, session.passphrase)
	if err != nil {
		clearLocalVaultDocument(&next)
		return err
	}
	clearLocalVaultDocument(&session.document)
	session.document = next
	session.fingerprint = fingerprint
	return nil
}

func (p *LocalVaultProvider) available() error {
	if p == nil || p.directoryErr != nil || strings.TrimSpace(p.directory) == "" {
		return fmt.Errorf("%w: encrypted vault directory is unavailable", ErrUnavailable)
	}
	return nil
}

func (p *LocalVaultProvider) unlockedSession(projectID string) (*localVaultSession, error) {
	if session := p.sessions[projectID]; session != nil {
		return session, nil
	}
	if _, err := os.Stat(p.vaultPath(projectID)); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, ErrVaultNotInitialized)
	} else if err != nil {
		return nil, fmt.Errorf("%w: inspect encrypted vault: %v", ErrUnavailable, err)
	}
	return nil, localVaultLockedError()
}

func (p *LocalVaultProvider) refreshSession(
	projectID string,
	session *localVaultSession,
) error {
	ciphertext, err := readLocalVaultFile(p.vaultPath(projectID))
	if errors.Is(err, os.ErrNotExist) {
		p.replaceSession(projectID, nil)
		return fmt.Errorf("%w: %w", ErrUnavailable, ErrVaultNotInitialized)
	}
	if err != nil {
		return fmt.Errorf("%w: read encrypted vault: %v", ErrUnavailable, err)
	}
	fingerprint := sha256.Sum256(ciphertext)
	if fingerprint == session.fingerprint {
		return nil
	}
	document, err := decryptLocalVault(ciphertext, session.passphrase)
	if err != nil {
		p.replaceSession(projectID, nil)
		return fmt.Errorf(
			"%w: the encrypted vault changed and must be unlocked again",
			ErrPermissionRequired,
		)
	}
	if err := validateLocalVaultDocument(document, projectID); err != nil {
		clearLocalVaultDocument(&document)
		p.replaceSession(projectID, nil)
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	clearLocalVaultDocument(&session.document)
	session.document = document
	session.fingerprint = fingerprint
	return nil
}

func (p *LocalVaultProvider) replaceSession(projectID string, next *localVaultSession) {
	if previous := p.sessions[projectID]; previous != nil {
		clearBytes(previous.passphrase)
		clearLocalVaultDocument(&previous.document)
		delete(p.sessions, projectID)
	}
	if next != nil {
		p.sessions[projectID] = next
	}
}

func (p *LocalVaultProvider) lockProject(
	ctx context.Context,
	projectID string,
) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.directory, 0o700); err != nil {
		return nil, fmt.Errorf("%w: create encrypted vault directory: %v", ErrUnavailable, err)
	}
	if err := os.Chmod(p.directory, 0o700); err != nil && runtime.GOOS != "windows" {
		return nil, fmt.Errorf("%w: secure encrypted vault directory: %v", ErrUnavailable, err)
	}
	lock := flock.New(p.lockPath(projectID))
	locked, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("%w: lock encrypted vault: %v", ErrUnavailable, err)
	}
	if !locked {
		return nil, fmt.Errorf("%w: lock encrypted vault: context cancelled", ErrUnavailable)
	}
	_ = os.Chmod(p.lockPath(projectID), 0o600)
	return func() { _ = lock.Unlock() }, nil
}

func (p *LocalVaultProvider) writeDocument(
	path string,
	document localVaultDocument,
	passphrase []byte,
) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	if err := validateLocalVaultDocument(document, document.ProjectID); err != nil {
		return zero, err
	}
	plaintext, err := json.Marshal(document)
	if err != nil {
		return zero, fmt.Errorf("encode encrypted vault: %w", err)
	}
	defer clearBytes(plaintext)

	recipient, err := age.NewScryptRecipient(string(passphrase))
	if err != nil {
		return zero, fmt.Errorf("prepare encrypted vault: %w", err)
	}
	recipient.SetWorkFactor(p.workFactor)
	var ciphertext bytes.Buffer
	writer, err := age.Encrypt(&ciphertext, recipient)
	if err != nil {
		return zero, fmt.Errorf("prepare encrypted vault: %w", err)
	}
	if _, err := writer.Write(plaintext); err != nil {
		_ = writer.Close()
		return zero, fmt.Errorf("encrypt local vault: %w", err)
	}
	if err := writer.Close(); err != nil {
		return zero, fmt.Errorf("finish encrypted vault: %w", err)
	}
	if ciphertext.Len() > maxLocalVaultBytes {
		return zero, fmt.Errorf("encrypted vault exceeds the %d byte limit", maxLocalVaultBytes)
	}
	data := ciphertext.Bytes()
	if err := writeLocalVaultFile(path, data); err != nil {
		return zero, err
	}
	return sha256.Sum256(data), nil
}

func decryptLocalVault(ciphertext, passphrase []byte) (localVaultDocument, error) {
	identity, err := age.NewScryptIdentity(string(passphrase))
	if err != nil {
		return localVaultDocument{}, err
	}
	identity.SetMaxWorkFactor(localVaultWorkFactor)
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return localVaultDocument{}, err
	}
	plaintext, err := io.ReadAll(io.LimitReader(reader, maxLocalVaultBytes+1))
	if err != nil {
		return localVaultDocument{}, err
	}
	defer clearBytes(plaintext)
	if len(plaintext) > maxLocalVaultBytes {
		return localVaultDocument{}, fmt.Errorf("decrypted vault exceeds the %d byte limit", maxLocalVaultBytes)
	}
	var document localVaultDocument
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return localVaultDocument{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON documents")
		}
		clearLocalVaultDocument(&document)
		return localVaultDocument{}, err
	}
	return document, nil
}

func writeLocalVaultFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".vault-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create temporary encrypted vault: %v", ErrUnavailable, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: secure temporary encrypted vault: %v", ErrUnavailable, err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: write temporary encrypted vault: %v", ErrUnavailable, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: sync temporary encrypted vault: %v", ErrUnavailable, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%w: close temporary encrypted vault: %v", ErrUnavailable, err)
	}
	if err := replaceLocalVaultFile(temporaryPath, path); err != nil {
		return fmt.Errorf("%w: replace encrypted vault: %v", ErrUnavailable, err)
	}
	if runtime.GOOS != "windows" {
		handle, err := os.Open(directory)
		if err != nil {
			return fmt.Errorf("%w: open encrypted vault directory: %v", ErrUnavailable, err)
		}
		defer handle.Close()
		if err := handle.Sync(); err != nil {
			return fmt.Errorf("%w: sync encrypted vault directory: %v", ErrUnavailable, err)
		}
	}
	return nil
}

func readLocalVaultFile(path string) ([]byte, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, maxLocalVaultBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLocalVaultBytes {
		return nil, fmt.Errorf("encrypted vault exceeds the %d byte limit", maxLocalVaultBytes)
	}
	return data, nil
}

func newLocalVaultDocument(projectID string) localVaultDocument {
	return localVaultDocument{
		Version:      localVaultVersion,
		ProjectID:    strings.TrimSpace(projectID),
		Environments: make(map[string]map[string][]byte),
	}
}

func validateLocalVaultDocument(document localVaultDocument, projectID string) error {
	if document.Version != localVaultVersion {
		return fmt.Errorf("unsupported encrypted vault version %d", document.Version)
	}
	if document.ProjectID != strings.TrimSpace(projectID) {
		return errors.New("encrypted vault belongs to a different project")
	}
	entries := 0
	for environment, values := range document.Environments {
		if strings.TrimSpace(environment) == "" ||
			environment != strings.TrimSpace(environment) ||
			len(environment) > 256 ||
			strings.ContainsAny(environment, "\r\n\x00") {
			return errors.New("encrypted vault contains an invalid environment")
		}
		for alias, value := range values {
			if err := (Ref{Provider: localVaultProviderName, Key: alias}).Validate(); err != nil {
				return err
			}
			if len(value) == 0 || len(value) > maxLocalVaultValue {
				return fmt.Errorf("encrypted vault value %q has an invalid size", alias)
			}
			entries++
			if entries > maxLocalVaultEntries {
				return fmt.Errorf("encrypted vault exceeds the %d entry limit", maxLocalVaultEntries)
			}
		}
	}
	return nil
}

func validateVaultProjectID(projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || len(projectID) > 256 || strings.ContainsAny(projectID, "\r\n\x00") {
		return errors.New("project ID is required for the encrypted vault")
	}
	return nil
}

func validateVaultPassphrase(passphrase []byte) error {
	if !utf8.Valid(passphrase) {
		return errors.New("vault passphrase must be valid UTF-8")
	}
	if utf8.RuneCount(passphrase) < minVaultPassphrase {
		return fmt.Errorf("vault passphrase must contain at least %d characters", minVaultPassphrase)
	}
	if len(passphrase) > maxVaultPassphrase {
		return fmt.Errorf("vault passphrase exceeds the %d byte limit", maxVaultPassphrase)
	}
	return nil
}

func validateLocalVaultRequest(request ResolveRequest) error {
	if err := validateProviderRequest(request, localVaultProviderName); err != nil {
		return err
	}
	if err := validateVaultProjectID(request.ProjectID); err != nil {
		return err
	}
	if strings.TrimSpace(request.Environment) == "" {
		return errors.New("environment is required for encrypted vault secrets")
	}
	return nil
}

func localVaultLockedError() error {
	return fmt.Errorf(
		"%w: unlock the encrypted local vault for this Renart session",
		ErrPermissionRequired,
	)
}

func vaultStatusStateForError(err error) StatusState {
	if errors.Is(err, ErrPermissionRequired) {
		return StatusPermissionRequired
	}
	return StatusUnavailable
}

func (p *LocalVaultProvider) vaultPath(projectID string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(projectID)))
	return filepath.Join(p.directory, fmt.Sprintf("v1-%x.age", hash[:]))
}

func (p *LocalVaultProvider) lockPath(projectID string) string {
	return p.vaultPath(projectID) + ".lock"
}

func localVaultValue(
	document localVaultDocument,
	environment string,
	alias string,
) ([]byte, bool) {
	values := document.Environments[environment]
	value, found := values[alias]
	return value, found
}

func countLocalVaultSecrets(document localVaultDocument) int {
	count := 0
	for _, values := range document.Environments {
		count += len(values)
	}
	return count
}

func cloneLocalVaultDocument(document localVaultDocument) localVaultDocument {
	result := newLocalVaultDocument(document.ProjectID)
	result.Version = document.Version
	for environment, values := range document.Environments {
		cloned := make(map[string][]byte, len(values))
		for alias, value := range values {
			cloned[alias] = append([]byte(nil), value...)
		}
		result.Environments[environment] = cloned
	}
	return result
}

func clearLocalVaultDocument(document *localVaultDocument) {
	if document == nil {
		return
	}
	for environment, values := range document.Environments {
		for alias, value := range values {
			clearBytes(value)
			delete(values, alias)
		}
		delete(document.Environments, environment)
	}
	document.Environments = nil
	document.ProjectID = ""
	document.Version = 0
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
