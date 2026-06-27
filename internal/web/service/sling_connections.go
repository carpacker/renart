package service

import "strings"

// Sling connection categories. A Sling asset moves data from one of these to
// another, so the asset editor restricts its source/target connection pickers to
// connections whose type maps to one of these categories.
const (
	SlingCategoryDatabase = "database"
	SlingCategoryStorage  = "storage"
	SlingCategoryFile     = "file"
)

// slingDatabaseConnectionTypes are bruin connection types that Sling can read
// from / write to as a database.
var slingDatabaseConnectionTypes = map[string]struct{}{
	"athena":                {},
	"clickhouse":            {},
	"couchbase":             {},
	"databricks":            {},
	"db2":                   {},
	"duckdb":                {},
	"dynamodb":              {},
	"elasticsearch":         {},
	"fabric":                {},
	"google_cloud_platform": {}, // BigQuery
	"hana":                  {},
	"mariadb":               {},
	"mongo":                 {},
	"mongo_atlas":           {},
	"motherduck":            {},
	"mssql":                 {},
	"mysql":                 {},
	"oracle":                {},
	"postgres":              {},
	"redshift":              {},
	"snowflake":             {},
	"spanner":               {},
	"sqlite":                {},
	"synapse":               {},
	"trino":                 {},
	"vertica":               {},
}

// slingStorageConnectionTypes are object-storage backends Sling reads/writes via
// file formats (CSV/Parquet/JSON).
var slingStorageConnectionTypes = map[string]struct{}{
	"s3":  {},
	"gcs": {},
}

// slingFileConnectionTypes are file transports (a remote/local filesystem rather
// than an object store).
var slingFileConnectionTypes = map[string]struct{}{
	"sftp": {},
}

// slingConnectionCategory classifies a bruin connection type into a Sling
// category, or returns "" when the connection is not a Sling-movable data store
// (e.g. an API source such as stripe or notion).
func slingConnectionCategory(connectionType string) string {
	normalized := strings.ToLower(strings.TrimSpace(connectionType))
	if _, ok := slingDatabaseConnectionTypes[normalized]; ok {
		return SlingCategoryDatabase
	}
	if _, ok := slingStorageConnectionTypes[normalized]; ok {
		return SlingCategoryStorage
	}
	if _, ok := slingFileConnectionTypes[normalized]; ok {
		return SlingCategoryFile
	}
	return ""
}
