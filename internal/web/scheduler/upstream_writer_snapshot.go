package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const upstreamWriterSnapshotVersionV1 = 1

var (
	ErrInvalidUpstreamWriterSnapshot  = errors.New("invalid upstream writer snapshot")
	ErrUpstreamWriterSnapshotConflict = errors.New("upstream writer snapshot is already persisted with different evidence")
)

type upstreamWriterSnapshotEnvelopeV1 struct {
	Version int                                      `json:"version"`
	Writers map[string]upstreamWriterSnapshotEntryV1 `json:"writers"`
}

type upstreamWriterSnapshotEntryV1 struct {
	AssetID           string `json:"asset_id"`
	TargetIdentity    string `json:"target_identity"`
	Fingerprint       string `json:"fingerprint"`
	VarsHash          string `json:"vars_hash"`
	TargetGeneration  int64  `json:"target_generation"`
	CompletionID      string `json:"completion_id"`
	CompletionOrdinal int64  `json:"completion_ordinal"`
	MaterializedAt    string `json:"materialized_at"`
}

func marshalUpstreamWriterSnapshot(
	writers map[string]UpstreamWriterSnapshot,
	present bool,
) (string, error) {
	if !present {
		if len(writers) != 0 {
			return "", fmt.Errorf("%w: writers require an explicit presence marker", ErrInvalidUpstreamWriterSnapshot)
		}
		return "", nil
	}
	wireWriters := make(map[string]upstreamWriterSnapshotEntryV1, len(writers))
	for assetID, writer := range writers {
		if err := validateUpstreamWriterSnapshotEntry(assetID, writer); err != nil {
			return "", err
		}
		wireWriters[assetID] = upstreamWriterSnapshotEntryV1{
			AssetID:           writer.AssetID,
			TargetIdentity:    writer.TargetIdentity,
			Fingerprint:       writer.Fingerprint,
			VarsHash:          writer.VarsHash,
			TargetGeneration:  writer.TargetGeneration,
			CompletionID:      writer.CompletionID,
			CompletionOrdinal: writer.CompletionOrdinal,
			MaterializedAt:    writer.MaterializedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	body, err := json.Marshal(upstreamWriterSnapshotEnvelopeV1{
		Version: upstreamWriterSnapshotVersionV1,
		Writers: wireWriters,
	})
	if err != nil {
		return "", fmt.Errorf("%w: encode: %v", ErrInvalidUpstreamWriterSnapshot, err)
	}
	return string(body), nil
}

func unmarshalUpstreamWriterSnapshot(body string) (map[string]UpstreamWriterSnapshot, bool, error) {
	if body == "" {
		return nil, false, nil
	}
	if strings.TrimSpace(body) != body {
		return nil, false, fmt.Errorf("%w: body is not canonical", ErrInvalidUpstreamWriterSnapshot)
	}
	var envelope upstreamWriterSnapshotEnvelopeV1
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, false, fmt.Errorf("%w: decode: %v", ErrInvalidUpstreamWriterSnapshot, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, false, fmt.Errorf("%w: trailing content: %v", ErrInvalidUpstreamWriterSnapshot, err)
	}
	if envelope.Version != upstreamWriterSnapshotVersionV1 {
		return nil, false, fmt.Errorf(
			"%w: unsupported version %d",
			ErrInvalidUpstreamWriterSnapshot,
			envelope.Version,
		)
	}
	if envelope.Writers == nil {
		return nil, false, fmt.Errorf("%w: writers must be an object", ErrInvalidUpstreamWriterSnapshot)
	}
	writers := make(map[string]UpstreamWriterSnapshot, len(envelope.Writers))
	for assetID, entry := range envelope.Writers {
		materializedAt, err := time.Parse(time.RFC3339Nano, entry.MaterializedAt)
		if err != nil || materializedAt.IsZero() || entry.MaterializedAt != materializedAt.UTC().Format(time.RFC3339Nano) {
			return nil, false, fmt.Errorf(
				"%w: writer %q has a non-canonical materialized_at",
				ErrInvalidUpstreamWriterSnapshot,
				assetID,
			)
		}
		writer := UpstreamWriterSnapshot{
			AssetID:           entry.AssetID,
			TargetIdentity:    entry.TargetIdentity,
			Fingerprint:       entry.Fingerprint,
			VarsHash:          entry.VarsHash,
			TargetGeneration:  entry.TargetGeneration,
			CompletionID:      entry.CompletionID,
			CompletionOrdinal: entry.CompletionOrdinal,
			MaterializedAt:    materializedAt,
		}
		if err := validateUpstreamWriterSnapshotEntry(assetID, writer); err != nil {
			return nil, false, err
		}
		writers[assetID] = writer
	}
	canonical, err := marshalUpstreamWriterSnapshot(writers, true)
	if err != nil {
		return nil, false, err
	}
	if canonical != body {
		return nil, false, fmt.Errorf("%w: body is not canonical", ErrInvalidUpstreamWriterSnapshot)
	}
	return writers, true, nil
}

func validateUpstreamWriterSnapshotEntry(assetID string, writer UpstreamWriterSnapshot) error {
	if strings.TrimSpace(assetID) == "" || strings.TrimSpace(assetID) != assetID {
		return fmt.Errorf("%w: writer key %q is not a canonical asset id", ErrInvalidUpstreamWriterSnapshot, assetID)
	}
	if writer.AssetID != assetID {
		return fmt.Errorf(
			"%w: writer %q carries asset_id %q",
			ErrInvalidUpstreamWriterSnapshot,
			assetID,
			writer.AssetID,
		)
	}
	for field, value := range map[string]string{
		"target_identity": writer.TargetIdentity,
		"fingerprint":     writer.Fingerprint,
		"vars_hash":       writer.VarsHash,
		"completion_id":   writer.CompletionID,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf(
				"%w: writer %q requires canonical %s",
				ErrInvalidUpstreamWriterSnapshot,
				assetID,
				field,
			)
		}
	}
	if writer.TargetGeneration <= 0 {
		return fmt.Errorf("%w: writer %q requires a positive target_generation", ErrInvalidUpstreamWriterSnapshot, assetID)
	}
	if writer.CompletionOrdinal < 0 {
		return fmt.Errorf("%w: writer %q has a negative completion_ordinal", ErrInvalidUpstreamWriterSnapshot, assetID)
	}
	if writer.MaterializedAt.IsZero() {
		return fmt.Errorf("%w: writer %q requires materialized_at", ErrInvalidUpstreamWriterSnapshot, assetID)
	}
	return nil
}
