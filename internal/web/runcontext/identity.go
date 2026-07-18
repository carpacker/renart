package runcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
)

const selectedConfigurationIdentityVersion = "renart-selected-configuration-v1"

// IdentityFidelity describes whether an identity covers every value that can
// affect the selected runtime configuration. RuntimeOnly is deliberately
// fail-closed: callers must not use its empty digest for plan confirmation or
// cache keys.
type IdentityFidelity string

const (
	IdentityFidelityExact       IdentityFidelity = "exact"
	IdentityFidelityRuntimeOnly IdentityFidelity = "runtime_only"
)

type Identity struct {
	Digest   string
	Fidelity IdentityFidelity
	Message  string
}

// SelectedConfigurationIdentity returns a canonical, secret-free identity for
// the selected Bruin environment controls and the named connections that the
// operation will actually use. Unrelated connections are intentionally out of
// scope. Credential values and credential-file paths are represented only by
// their presence, as declared by Bruin's sensitive and sensitive_file tags.
// This identity intentionally does not identify a physical target.
func SelectedConfigurationIdentity(environmentName string, environment *config.Environment, connectionNames []string) Identity {
	projection := selectedConfiguration{
		Environment: strings.TrimSpace(environmentName),
	}
	if environment != nil {
		projection.SchemaPrefix = environment.SchemaPrefix
		projection.RefreshRestricted = environment.Config != nil && environment.Config.RefreshRestricted
	}

	reflected := reflect.ValueOf(projection)
	if err := validateCanonicalType(reflected.Type(), "configuration", nil); err != nil {
		return runtimeOnlyIdentity(err.Error())
	}
	encoder := canonicalEncoder{}
	encoder.token("namespace", selectedConfigurationIdentityVersion)
	if err := encoder.value(reflected, "configuration", nil); err != nil {
		return runtimeOnlyIdentity(err.Error())
	}

	names := uniqueSortedStrings(connectionNames)
	encoder.token("connections", strconv.Itoa(len(names)))
	if len(names) > 0 && (environment == nil || environment.Connections == nil) {
		return runtimeOnlyIdentity("selected environment has no connection configuration")
	}
	for _, name := range names {
		connectionType := environment.Connections.ConnectionsSummaryList()[name]
		connection := environment.Connections.GetConnection(name)
		if connection == nil || connectionType == "" {
			return runtimeOnlyIdentity(fmt.Sprintf("connection %q is not present in the selected environment", name))
		}
		value := reflect.ValueOf(connection)
		if err := validateCanonicalType(value.Type(), "connection."+name, nil); err != nil {
			return runtimeOnlyIdentity(err.Error())
		}
		encoder.token("connection_name", name)
		encoder.token("connection_type", connectionType)
		if err := encoder.value(value, "connection."+name, nil); err != nil {
			return runtimeOnlyIdentity(err.Error())
		}
	}

	digest := sha256.Sum256(encoder.bytes)
	return Identity{Digest: hex.EncodeToString(digest[:]), Fidelity: IdentityFidelityExact}
}

type selectedConfiguration struct {
	Environment       string `mapstructure:"environment"`
	SchemaPrefix      string `mapstructure:"schema_prefix"`
	RefreshRestricted bool   `mapstructure:"full_refresh_restricted"`
}

// SecretFreeCanonicalIdentity is the generic identity primitive used by run
// contexts. It reflects ordinary data directly and never invokes JSON/YAML or
// text marshalers. Unsupported or ambiguous types return runtime_only instead
// of being skipped, so dependency upgrades cannot silently weaken an identity.
func SecretFreeCanonicalIdentity(namespace string, value any) Identity {
	if strings.TrimSpace(namespace) == "" {
		return runtimeOnlyIdentity("identity namespace is empty")
	}
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return runtimeOnlyIdentity("identity value has no concrete type")
	}
	if err := validateCanonicalType(reflected.Type(), "configuration", nil); err != nil {
		return runtimeOnlyIdentity(err.Error())
	}
	encoder := canonicalEncoder{}
	encoder.token("namespace", namespace)
	if err := encoder.value(reflected, "configuration", nil); err != nil {
		return runtimeOnlyIdentity(err.Error())
	}
	digest := sha256.Sum256(encoder.bytes)
	return Identity{
		Digest:   hex.EncodeToString(digest[:]),
		Fidelity: IdentityFidelityExact,
	}
}

func runtimeOnlyIdentity(message string) Identity {
	return Identity{Fidelity: IdentityFidelityRuntimeOnly, Message: message}
}

func validateCanonicalType(value reflect.Type, path string, stack map[reflect.Type]bool) error {
	if stack == nil {
		stack = make(map[reflect.Type]bool)
	}
	switch value.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return nil
	case reflect.Pointer, reflect.Array, reflect.Slice:
		if value.Kind() == reflect.Slice && value.Elem().Kind() == reflect.Uint8 {
			return fmt.Errorf("%s uses an opaque byte payload", path)
		}
		return validateCanonicalType(value.Elem(), path, stack)
	case reflect.Map:
		return fmt.Errorf("%s uses opaque map type %s", path, value)
	case reflect.Interface:
		return fmt.Errorf("%s uses opaque interface type %s", path, value)
	case reflect.Struct:
		if stack[value] {
			return fmt.Errorf("%s uses recursive type %s", path, value)
		}
		stack[value] = true
		defer delete(stack, value)

		fields, err := canonicalStructFields(value, path)
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			return fmt.Errorf("%s uses opaque struct type %s", path, value)
		}
		for _, field := range fields {
			if field.sensitive {
				continue
			}
			if err := validateCanonicalType(field.field.Type, path+"."+field.name, stack); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s uses unsupported type %s", path, value)
	}
}

type canonicalField struct {
	name      string
	index     []int
	field     reflect.StructField
	sensitive bool
}

func canonicalStructFields(t reflect.Type, path string) ([]canonicalField, error) {
	fields := make([]canonicalField, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		if _, declared := field.Tag.Lookup("mapstructure"); !declared {
			return nil, fmt.Errorf("%s.%s has no explicit public configuration key", path, field.Name)
		}
		name, squash, ignored := canonicalFieldName(field)
		if ignored {
			continue
		}
		if squash {
			nested := field.Type
			for nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if nested.Kind() != reflect.Struct {
				return nil, fmt.Errorf("%s.%s declares squash on non-struct type %s", path, field.Name, field.Type)
			}
			nestedFields, err := canonicalStructFields(nested, path+"."+field.Name)
			if err != nil {
				return nil, err
			}
			for _, nestedField := range nestedFields {
				nestedField.index = append([]int{i}, nestedField.index...)
				fields = append(fields, nestedField)
			}
			continue
		}
		fields = append(fields, canonicalField{
			name:      name,
			index:     []int{i},
			field:     field,
			sensitive: field.Tag.Get("sensitive") == "true" || field.Tag.Get("sensitive_file") == "true",
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
	for i := 1; i < len(fields); i++ {
		if fields[i-1].name == fields[i].name {
			return nil, fmt.Errorf("%s has duplicate canonical field %q", path, fields[i].name)
		}
	}
	return fields, nil
}

func canonicalFieldName(field reflect.StructField) (name string, squash, ignored bool) {
	tag := field.Tag.Get("mapstructure")
	parts := strings.Split(tag, ",")
	if len(parts) > 0 && parts[0] == "-" {
		return "", false, true
	}
	for _, option := range parts[1:] {
		if option == "squash" {
			squash = true
		}
	}
	if squash {
		return "", true, false
	}
	if len(parts) > 0 && parts[0] != "" {
		return parts[0], false, false
	}
	return field.Name, false, false
}

func unsafeStringField(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "value" || normalized == "dsn" || normalized == "url" || normalized == "uri" || normalized == "endpoint" ||
		normalized == "options" || normalized == "params" || normalized == "parameters" || normalized == "properties" {
		return true
	}
	for _, fragment := range []string{
		"_url", "_uri", "dsn_", "_dsn", "connection_string", "connect_string",
		"endpoint_", "_endpoint", "proxy", "raw", "blob", "payload", "content",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

type canonicalEncoder struct {
	bytes []byte
}

func (e *canonicalEncoder) token(kind, value string) {
	e.bytes = strconv.AppendInt(e.bytes, int64(len(kind)), 10)
	e.bytes = append(e.bytes, ':')
	e.bytes = append(e.bytes, kind...)
	e.bytes = strconv.AppendInt(e.bytes, int64(len(value)), 10)
	e.bytes = append(e.bytes, ':')
	e.bytes = append(e.bytes, value...)
}

func (e *canonicalEncoder) value(value reflect.Value, path string, field *canonicalField) error {
	if field != nil && field.sensitive {
		e.token("sensitive", strconv.FormatBool(!value.IsZero()))
		return nil
	}
	if field != nil && unsafeStringField(field.name) {
		if raw, isString := reflectedString(value); isString && raw != "" {
			return fmt.Errorf("%s may contain inline credential material", path)
		}
	}
	if field != nil && dereferencedType(field.field.Type).Kind() == reflect.Slice && dereferencedType(field.field.Type).Elem().Kind() == reflect.Uint8 {
		return fmt.Errorf("%s uses an opaque byte payload", path)
	}

	switch value.Kind() {
	case reflect.Bool:
		e.token("bool", strconv.FormatBool(value.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.token("int", strconv.FormatInt(value.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		e.token("uint", strconv.FormatUint(value.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		e.token("float", strconv.FormatFloat(value.Float(), 'g', -1, value.Type().Bits()))
	case reflect.String:
		e.token("string", value.String())
	case reflect.Pointer:
		if value.IsNil() {
			e.token("pointer", "nil")
			return nil
		}
		e.token("pointer", "value")
		return e.value(value.Elem(), path, nil)
	case reflect.Slice:
		if value.IsNil() {
			e.token("slice", "nil")
			return nil
		}
		e.token("slice", strconv.Itoa(value.Len()))
		for i := range value.Len() {
			if err := e.value(value.Index(i), fmt.Sprintf("%s[%d]", path, i), nil); err != nil {
				return err
			}
		}
	case reflect.Array:
		e.token("array", strconv.Itoa(value.Len()))
		for i := range value.Len() {
			if err := e.value(value.Index(i), fmt.Sprintf("%s[%d]", path, i), nil); err != nil {
				return err
			}
		}
	case reflect.Map:
		return fmt.Errorf("%s uses opaque map type %s", path, value.Type())
	case reflect.Struct:
		fields, err := canonicalStructFields(value.Type(), path)
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			return fmt.Errorf("%s uses opaque struct type %s", path, value.Type())
		}
		e.token("struct", value.Type().PkgPath()+"."+value.Type().Name())
		for i := range fields {
			current := fields[i]
			fieldValue, ok := fieldByIndex(value, current.index)
			if !ok {
				e.token("field", current.name)
				e.token("pointer", "nil")
				continue
			}
			e.token("field", current.name)
			if err := e.value(fieldValue, path+"."+current.name, &current); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s uses unsupported type %s", path, value.Type())
	}
	return nil
}

func reflectedString(value reflect.Value) (string, bool) {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", true
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.String {
		return "", false
	}
	return value.String(), true
}

func dereferencedType(value reflect.Type) reflect.Type {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value
}

func fieldByIndex(value reflect.Value, index []int) (reflect.Value, bool) {
	current := value
	for _, position := range index {
		for current.Kind() == reflect.Pointer {
			if current.IsNil() {
				return reflect.Value{}, false
			}
			current = current.Elem()
		}
		current = current.Field(position)
	}
	return current, true
}
