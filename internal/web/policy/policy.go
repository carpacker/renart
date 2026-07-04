// Package policy enforces per-environment execution rules at the single
// run-dispatch chokepoint every execution path goes through (UI build, CLI,
// scheduler). UI-side disabling mirrors these rules but is not the
// enforcement.
//
// Locally these are guardrails — the user owns the credentials. The
// enforced version is the cloud permission model, where protected
// environments' credentials only decrypt for the scheduler identity. Flag
// names and semantics are kept identical so the cloud later enforces the
// same configuration harder rather than introducing a second vocabulary.
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// EnvironmentPolicy is the per-environment rule set.
type EnvironmentPolicy struct {
	// Protected forbids interactive build-mode execution (working tree
	// runs from the UI or CLI). Scheduled snapshot runs pass.
	Protected bool `yaml:"protected" json:"protected"`
	// DeployedOnly forbids any execution that is not a deployed snapshot,
	// including scheduled runs falling back to the working tree.
	DeployedOnly bool `yaml:"deployed_only" json:"deployed_only"`
	// ConfirmDestructive requires a typed environment-name confirmation for
	// destructive operations (full refresh, backfill, drop).
	ConfirmDestructive bool `yaml:"confirm_destructive" json:"confirm_destructive"`
}

// Zero reports whether the policy has no flags set.
func (p EnvironmentPolicy) Zero() bool {
	return !p.Protected && !p.DeployedOnly && !p.ConfirmDestructive
}

// Config is the on-disk policy file (.renart/environments.yml).
type Config struct {
	Environments map[string]EnvironmentPolicy `yaml:"environments" json:"environments"`
}

// For returns the policy for an environment; absent environments have the
// zero (unrestricted) policy.
func (c Config) For(environment string) EnvironmentPolicy {
	return c.Environments[environment]
}

// Load reads the policy file; a missing file is an empty config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("policy: failed to parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the environment policy file, creating its parent directory when
// needed. Zero policies are omitted so clearing every flag removes the
// environment from the policy file without affecting Bruin config.
func Save(path string, cfg Config) error {
	cleaned := Config{Environments: map[string]EnvironmentPolicy{}}
	for name, envPolicy := range cfg.Environments {
		if name == "" || envPolicy.Zero() {
			continue
		}
		cleaned.Environments[name] = envPolicy
	}

	data, err := yaml.Marshal(cleaned)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Loader caches the policy file, revalidated by stat so edits apply without
// a restart.
type Loader struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	loaded  bool
	cfg     Config
}

func NewLoader(path string) *Loader {
	return &Loader{path: path}
}

func (l *Loader) Path() string {
	return l.path
}

func (l *Loader) Config() Config {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, err := os.Stat(l.path)
	if err != nil {
		l.cfg = Config{}
		l.loaded = true
		l.modTime = time.Time{}
		l.size = 0
		return l.cfg
	}
	if l.loaded && info.ModTime().Equal(l.modTime) && info.Size() == l.size {
		return l.cfg
	}
	cfg, err := Load(l.path)
	if err != nil {
		// Unparseable policy fails closed only for new reads; keep the last
		// good config rather than silently dropping protection.
		if l.loaded {
			return l.cfg
		}
		return Config{}
	}
	l.cfg = cfg
	l.loaded = true
	l.modTime = info.ModTime()
	l.size = info.Size()
	return cfg
}

func (l *Loader) For(environment string) EnvironmentPolicy {
	return l.Config().For(environment)
}

func (l *Loader) Set(environment string, envPolicy EnvironmentPolicy) (Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cfg, err := Load(l.path)
	if err != nil {
		return Config{}, err
	}
	if cfg.Environments == nil {
		cfg.Environments = map[string]EnvironmentPolicy{}
	}
	if envPolicy.Zero() {
		delete(cfg.Environments, environment)
	} else {
		cfg.Environments[environment] = envPolicy
	}
	if err := Save(l.path, cfg); err != nil {
		return Config{}, err
	}

	info, statErr := os.Stat(l.path)
	if statErr == nil {
		l.modTime = info.ModTime()
		l.size = info.Size()
	} else {
		l.modTime = time.Time{}
		l.size = 0
	}
	l.cfg = cfg
	l.loaded = true
	return cfg, nil
}

// RunRequest describes one execution attempt for policy evaluation.
type RunRequest struct {
	Environment string
	// Interactive marks build-mode execution (UI or CLI working-tree runs);
	// scheduler-dispatched runs are not interactive.
	Interactive bool
	// SnapshotBased marks execution of a deployed snapshot.
	SnapshotBased bool
	// Destructive marks full refresh / backfill / drop operations.
	Destructive bool
	// ConfirmedEnvironment carries the typed confirmation for destructive
	// operations.
	ConfirmedEnvironment string
}

// Check is the single enforcement point. Every execution path must pass
// through it; scattered UI-side checks are hints, not enforcement.
func Check(p EnvironmentPolicy, req RunRequest) error {
	if p.Protected && req.Interactive {
		return fmt.Errorf("environment %q is protected: interactive execution is disabled; deploy and schedule instead", req.Environment)
	}
	if p.DeployedOnly && !req.SnapshotBased {
		return fmt.Errorf("environment %q only executes deployed snapshots: deploy the pipeline first", req.Environment)
	}
	if p.ConfirmDestructive && req.Destructive && req.ConfirmedEnvironment != req.Environment {
		return fmt.Errorf("environment %q requires typing the environment name to confirm destructive operations", req.Environment)
	}
	return nil
}
