package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeCreateAssetRequestReadsBinarySeedUpload(t *testing.T) {
	t.Parallel()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	encoded, err := json.Marshal(CreateAssetRequest{
		Name:       "analytics.customers",
		Type:       "duckdb.seed",
		Connection: "duckdb-default",
		Parameters: map[string]string{"enforce_schema": "false"},
	})
	require.NoError(t, err)
	requestField, err := writer.CreateFormField("request")
	require.NoError(t, err)
	_, err = requestField.Write(encoded)
	require.NoError(t, err)
	fileField, err := writer.CreateFormFile("file", "customers.parquet")
	require.NoError(t, err)
	upload := []byte{0x50, 0x41, 0x52, 0x31, 0x00, 0xff}
	_, err = fileField.Write(upload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/api/pipelines/x/assets", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	decoded, err := decodeCreateAssetRequest(response, request)
	require.NoError(t, err)
	assert.Equal(t, "analytics.customers", decoded.Name)
	assert.Equal(t, "duckdb.seed", decoded.Type)
	assert.Equal(t, "customers.parquet", decoded.SeedFileName)
	assert.Equal(t, upload, decoded.SeedFileBytes)
	assert.Equal(t, "false", decoded.Parameters["enforce_schema"])
}

func TestDecodeCreateAssetRequestKeepsJSONContract(t *testing.T) {
	t.Parallel()
	body := bytes.NewBufferString(`{"name":"analytics.orders","type":"duckdb.sql"}`)
	request := httptest.NewRequest("POST", "/api/pipelines/x/assets", body)
	request.Header.Set("Content-Type", "application/json")

	decoded, err := decodeCreateAssetRequest(httptest.NewRecorder(), request)
	require.NoError(t, err)
	assert.Equal(t, "analytics.orders", decoded.Name)
	assert.Equal(t, "duckdb.sql", decoded.Type)
}
