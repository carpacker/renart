package secretstore

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	providerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	envNamePattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	localAliasPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// Ref is the parsed, provider-neutral identity of a secret. The value behind
// the reference is intentionally not represented here.
type Ref struct {
	Provider string
	Key      string
	Field    string
	Version  string
}

func ParseRef(value string) (Ref, error) {
	value = strings.TrimSpace(value)
	provider, key, found := strings.Cut(value, ":")
	if !found || !providerNamePattern.MatchString(provider) {
		return Ref{}, fmt.Errorf("secret reference must use provider:key syntax")
	}
	if key == "" {
		return Ref{}, fmt.Errorf("secret reference %q has an empty key", provider)
	}

	ref := Ref{Provider: provider}
	if base, field, hasField := strings.Cut(key, "#"); hasField {
		if base == "" || field == "" || strings.Contains(field, "#") {
			return Ref{}, fmt.Errorf("secret reference %q has an invalid field selector", value)
		}
		ref.Key = base
		ref.Field = field
	} else {
		ref.Key = key
	}

	if err := ref.Validate(); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

func (r Ref) Validate() error {
	if !providerNamePattern.MatchString(r.Provider) {
		return fmt.Errorf("invalid secret provider %q", r.Provider)
	}
	if strings.TrimSpace(r.Key) == "" || r.Key != strings.TrimSpace(r.Key) {
		return fmt.Errorf("secret reference key is required and cannot have surrounding whitespace")
	}
	if strings.ContainsAny(r.Key, "\r\n\x00") || strings.ContainsAny(r.Field, "\r\n\x00") {
		return fmt.Errorf("secret reference contains invalid control characters")
	}
	switch r.Provider {
	case "env":
		if r.Field != "" || r.Version != "" || !envNamePattern.MatchString(r.Key) {
			return fmt.Errorf("environment secret reference must be env:NAME")
		}
	case "local", localVaultProviderName:
		if r.Field != "" || r.Version != "" || !validLocalAlias(r.Key) {
			return fmt.Errorf("%s secret reference must use a portable alias", r.Provider)
		}
	}
	return nil
}

func validLocalAlias(alias string) bool {
	if len(alias) > 256 || !localAliasPattern.MatchString(alias) {
		return false
	}
	for _, segment := range strings.Split(alias, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (r Ref) String() string {
	value := r.Provider + ":" + r.Key
	if r.Field != "" {
		value += "#" + r.Field
	}
	return value
}

func (r Ref) MarshalText() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return []byte(r.String()), nil
}

func (r *Ref) UnmarshalText(value []byte) error {
	parsed, err := ParseRef(string(value))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func ValidSymbol(value string) bool {
	return envNamePattern.MatchString(value)
}
