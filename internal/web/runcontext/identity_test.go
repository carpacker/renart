package runcontext

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectedConfigurationProjectionCoversBruinEnvironmentSchema(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"Connections", "SchemaPrefix", "Config"}, exportedFieldNames(reflect.TypeOf(config.Environment{})))
	assert.Equal(t, []string{"RefreshRestricted"}, exportedFieldNames(reflect.TypeOf(config.EnvironmentConfig{})))
}

func TestSelectedConfigurationIdentitySupportsCommonConnectionSchemas(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		connections *config.Connections
	}{
		{
			name: "duckdb",
			connections: &config.Connections{DuckDB: []config.DuckDBConnection{{
				ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
				Path:               "analytics.duckdb",
			}}},
		},
		{
			name: "postgres",
			connections: &config.Connections{Postgres: []config.PostgresConnection{{
				ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
				Host:               "db.internal",
				Database:           "analytics",
			}}},
		},
		{
			name: "snowflake",
			connections: &config.Connections{Snowflake: []config.SnowflakeConnection{{
				ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
				Account:            "account",
				Database:           "analytics",
				PrivateKeyPath:     "/missing/private-key.pem",
			}}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			identity := SelectedConfigurationIdentity("default", &config.Environment{
				Connections: tt.connections,
				Config:      &config.EnvironmentConfig{},
			}, []string{"warehouse"})
			require.Equal(t, IdentityFidelityExact, identity.Fidelity, identity.Message)
			assert.Len(t, identity.Digest, 64)
			assert.Empty(t, identity.Message)
		})
	}
}

func TestSelectedConfigurationIdentityIsCanonicalAndTracksBehavior(t *testing.T) {
	t.Parallel()

	first := SelectedConfigurationIdentity("development", postgresIdentityEnvironment("first-secret", "db.internal", "dev_", true), []string{"warehouse"})
	require.Equal(t, IdentityFidelityExact, first.Fidelity, first.Message)

	assert.Equal(t, first.Digest, SelectedConfigurationIdentity("development", postgresIdentityEnvironment("rotated-secret", "db.internal", "dev_", true), []string{"warehouse"}).Digest,
		"credential rotation must not invalidate a secret-free configuration identity")

	assert.NotEqual(t, first.Digest, SelectedConfigurationIdentity("development", postgresIdentityEnvironment("first-secret", "new-db.internal", "dev_", true), []string{"warehouse"}).Digest)
	assert.NotEqual(t, first.Digest, SelectedConfigurationIdentity("development", postgresIdentityEnvironment("first-secret", "db.internal", "preview_", true), []string{"warehouse"}).Digest)
	assert.NotEqual(t, first.Digest, SelectedConfigurationIdentity("development", postgresIdentityEnvironment("first-secret", "db.internal", "dev_", false), []string{"warehouse"}).Digest)
	assert.NotEqual(t, first.Digest, SelectedConfigurationIdentity("production", postgresIdentityEnvironment("first-secret", "db.internal", "dev_", true), []string{"warehouse"}).Digest)
	assert.NotContains(t, first.Message, "first-secret")
	assert.NotContains(t, first.Message, "rotated-secret")
}

func TestSelectedConfigurationIdentityNormalizesMissingEnvironmentConfigToFalse(t *testing.T) {
	t.Parallel()

	withoutConfig := &config.Environment{
		Connections: &config.Connections{Postgres: []config.PostgresConnection{{
			ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
			Host:               "db.internal",
		}}},
	}
	withFalse := &config.Environment{
		Config: &config.EnvironmentConfig{RefreshRestricted: false},
		Connections: &config.Connections{Postgres: []config.PostgresConnection{{
			ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
			Host:               "db.internal",
		}}},
	}

	assert.Equal(t,
		SelectedConfigurationIdentity("default", withoutConfig, []string{"warehouse"}).Digest,
		SelectedConfigurationIdentity("default", withFalse, []string{"warehouse"}).Digest,
	)
}

func TestSelectedConfigurationIdentityUsesSensitiveFileTagWithoutReadingOrLeakingPath(t *testing.T) {
	t.Parallel()

	first := &config.Environment{Connections: &config.Connections{Snowflake: []config.SnowflakeConnection{
		{
			ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
			Account:            "account",
			Username:           "renart",
			Database:           "analytics",
			PrivateKeyPath:     "/definitely/missing/first-private-key.pem",
		},
	}}}
	second := &config.Environment{Connections: &config.Connections{Snowflake: []config.SnowflakeConnection{
		{
			ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
			Account:            "account",
			Username:           "renart",
			Database:           "analytics",
			PrivateKeyPath:     "/definitely/missing/rotated-private-key.pem",
		},
	}}}

	firstIdentity := SelectedConfigurationIdentity("default", first, []string{"warehouse"})
	secondIdentity := SelectedConfigurationIdentity("default", second, []string{"warehouse"})
	require.Equal(t, IdentityFidelityExact, firstIdentity.Fidelity, firstIdentity.Message)
	require.Equal(t, IdentityFidelityExact, secondIdentity.Fidelity, secondIdentity.Message)
	assert.Equal(t, firstIdentity.Digest, secondIdentity.Digest)
	assert.NotContains(t, firstIdentity.Message, "private-key")
}

func TestSelectedConfigurationIdentitySortsRelevantConnectionNamesAndIgnoresUnrelatedConnections(t *testing.T) {
	t.Parallel()

	left := &config.Environment{Connections: &config.Connections{Postgres: []config.PostgresConnection{
		{ConnectionMetadata: config.ConnectionMetadata{Name: "secondary"}, Host: "two.internal"},
		{ConnectionMetadata: config.ConnectionMetadata{Name: "primary"}, Host: "one.internal"},
		{ConnectionMetadata: config.ConnectionMetadata{Name: "unrelated"}, Host: "ignored.internal"},
	}}}
	right := &config.Environment{Connections: &config.Connections{Postgres: []config.PostgresConnection{
		{ConnectionMetadata: config.ConnectionMetadata{Name: "unrelated"}, Host: "changed.internal"},
		{ConnectionMetadata: config.ConnectionMetadata{Name: "primary"}, Host: "one.internal"},
		{ConnectionMetadata: config.ConnectionMetadata{Name: "secondary"}, Host: "two.internal"},
	}}}

	leftIdentity := SelectedConfigurationIdentity("default", left, []string{"secondary", "primary", "primary"})
	rightIdentity := SelectedConfigurationIdentity("default", right, []string{"primary", "secondary"})
	require.Equal(t, IdentityFidelityExact, leftIdentity.Fidelity, leftIdentity.Message)
	require.Equal(t, IdentityFidelityExact, rightIdentity.Fidelity, rightIdentity.Message)
	assert.Equal(t, leftIdentity.Digest, rightIdentity.Digest)
}

func TestSecretFreeCanonicalIdentityRejectsOpaqueMaps(t *testing.T) {
	t.Parallel()

	type opaqueMap struct {
		Options map[string]string `mapstructure:"options"`
	}

	identity := SecretFreeCanonicalIdentity("opaque-map-v1", opaqueMap{Options: map[string]string{"password": "secret"}})
	assert.Equal(t, IdentityFidelityRuntimeOnly, identity.Fidelity)
	assert.Empty(t, identity.Digest)
	assert.Contains(t, identity.Message, "opaque map")
	assert.NotContains(t, identity.Message, "secret")
}

func TestSelectedConfigurationIdentityFailsClosedForCredentialBearingURLFields(t *testing.T) {
	t.Parallel()

	identity := SelectedConfigurationIdentity("default", &config.Environment{
		Connections: &config.Connections{S3: []config.S3Connection{{
			ConnectionMetadata: config.ConnectionMetadata{Name: "source"},
			BucketName:         "events",
			EndpointURL:        "https://user:top-secret@example.test?token=hidden",
		}}},
	}, []string{"source"})

	assert.Equal(t, IdentityFidelityRuntimeOnly, identity.Fidelity)
	assert.Empty(t, identity.Digest)
	assert.Contains(t, identity.Message, "endpoint_url")
	assert.NotContains(t, identity.Message, "top-secret")
	assert.NotContains(t, identity.Message, "hidden")
}

func TestSelectedConfigurationIdentityFailsClosedForRawConnectionOptions(t *testing.T) {
	t.Parallel()

	identity := SelectedConfigurationIdentity("default", &config.Environment{
		Connections: &config.Connections{MsSQL: []config.MsSQLConnection{{
			ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
			Host:               "sql.internal",
			Database:           "analytics",
			Options:            "encrypt=true&password=top-secret",
		}}},
	}, []string{"warehouse"})

	assert.Equal(t, IdentityFidelityRuntimeOnly, identity.Fidelity)
	assert.Empty(t, identity.Digest)
	assert.Contains(t, identity.Message, "options")
	assert.NotContains(t, identity.Message, "top-secret")
}

func TestSecretFreeCanonicalIdentityFailsClosedForPointerToRawString(t *testing.T) {
	t.Parallel()

	secretOptions := "password=pointer-secret"
	identity := SecretFreeCanonicalIdentity("pointer-options-v1", struct {
		Options *string `mapstructure:"options"`
	}{Options: &secretOptions})

	assert.Equal(t, IdentityFidelityRuntimeOnly, identity.Fidelity)
	assert.Empty(t, identity.Digest)
	assert.Contains(t, identity.Message, "options")
	assert.NotContains(t, identity.Message, "pointer-secret")
}

func TestSecretFreeCanonicalIdentityFailsClosedForNilPointerToOpaqueSchema(t *testing.T) {
	t.Parallel()

	identity := SecretFreeCanonicalIdentity("nil-opaque-v1", struct {
		Opaque *map[string]string `mapstructure:"opaque"`
	}{})

	assert.Equal(t, IdentityFidelityRuntimeOnly, identity.Fidelity)
	assert.Empty(t, identity.Digest)
	assert.Contains(t, identity.Message, "opaque map")
}

func TestSecretFreeCanonicalIdentityFailsClosedForOpaqueFields(t *testing.T) {
	t.Parallel()

	identity := SecretFreeCanonicalIdentity("opaque-v1", struct {
		Name   string   `mapstructure:"name"`
		Opaque chan int `mapstructure:"opaque"`
	}{Name: "safe"})

	assert.Equal(t, IdentityFidelityRuntimeOnly, identity.Fidelity)
	assert.Empty(t, identity.Digest)
	assert.Contains(t, identity.Message, "opaque")
	assert.NotContains(t, identity.Message, "safe")
}

var customMarshalInvocations int

type customJSONMarshaler struct {
	Label string `mapstructure:"label"`
}

func (value customJSONMarshaler) MarshalJSON() ([]byte, error) {
	customMarshalInvocations++
	return json.Marshal(map[string]string{"not_the_reflected_value": value.Label})
}

func TestSecretFreeCanonicalIdentityDoesNotInvokeCustomMarshalers(t *testing.T) {
	customMarshalInvocations = 0

	identity := SecretFreeCanonicalIdentity("no-marshaler-v1", customJSONMarshaler{Label: "plain"})

	require.Equal(t, IdentityFidelityExact, identity.Fidelity, identity.Message)
	assert.Zero(t, customMarshalInvocations)
	assert.False(t, strings.Contains(identity.Message, "plain"))
}

func postgresIdentityEnvironment(password, host, schemaPrefix string, refreshRestricted bool) *config.Environment {
	maxConcurrent := 2
	return &config.Environment{
		SchemaPrefix: schemaPrefix,
		Config:       &config.EnvironmentConfig{RefreshRestricted: refreshRestricted},
		Connections: &config.Connections{Postgres: []config.PostgresConnection{{
			ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse", MaxConcurrentAssets: &maxConcurrent},
			Username:           "renart",
			Password:           password,
			Host:               host,
			Port:               5432,
			Database:           "analytics",
			Schema:             "public",
			PoolMaxConns:       10,
			SslMode:            "require",
		}}},
	}
}

func exportedFieldNames(value reflect.Type) []string {
	result := make([]string, 0, value.NumField())
	for index := range value.NumField() {
		field := value.Field(index)
		if field.IsExported() {
			result = append(result, field.Name)
		}
	}
	return result
}
