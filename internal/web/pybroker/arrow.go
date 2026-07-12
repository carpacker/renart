package pybroker

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/bruin/pkg/query"
)

// arrowBatchRows is how many rows go into one Arrow record batch. The whole
// result set is already materialized in memory (bruin connections return
// [][]any), so batching only bounds the per-batch allocation on the way out.
const arrowBatchRows = 8192

// columnKind is the inferred logical type of a result column. Inference scans
// the actual Go values (bruin connections surface warehouse-specific driver
// types), assisted by the connection's declared column type where available.
type columnKind int

const (
	kindString columnKind = iota
	kindInt64
	kindFloat64
	kindBool
	kindTimestamp
	kindDate
	kindBinary
)

// writeQueryResultArrow streams a bruin QueryResult as an Arrow IPC stream.
func writeQueryResultArrow(w io.Writer, result *query.QueryResult) error {
	kinds := inferColumnKinds(result)

	fields := make([]arrow.Field, len(result.Columns))
	for i, name := range result.Columns {
		fields[i] = arrow.Field{Name: name, Type: arrowType(kinds[i]), Nullable: true}
	}
	schema := arrow.NewSchema(fields, nil)

	writer := ipc.NewWriter(w, ipc.WithSchema(schema))
	defer writer.Close()

	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()

	flush := func(rows int) error {
		if rows == 0 {
			return nil
		}
		record := builder.NewRecord()
		defer record.Release()
		return writer.Write(record)
	}

	pending := 0
	for _, row := range result.Rows {
		for col := range result.Columns {
			var value any
			if col < len(row) {
				value = row[col]
			}
			appendValue(builder.Field(col), kinds[col], value)
		}
		pending++
		if pending == arrowBatchRows {
			if err := flush(pending); err != nil {
				return err
			}
			pending = 0
		}
	}
	if err := flush(pending); err != nil {
		return err
	}
	// A zero-row result still needs one write so the reader sees the schema.
	if len(result.Rows) == 0 {
		if err := flush(0); err != nil {
			return err
		}
		record := builder.NewRecord()
		defer record.Release()
		return writer.Write(record)
	}
	return nil
}

func arrowType(kind columnKind) arrow.DataType {
	switch kind {
	case kindInt64:
		return arrow.PrimitiveTypes.Int64
	case kindFloat64:
		return arrow.PrimitiveTypes.Float64
	case kindBool:
		return arrow.FixedWidthTypes.Boolean
	case kindTimestamp:
		return arrow.FixedWidthTypes.Timestamp_us
	case kindDate:
		return arrow.FixedWidthTypes.Date32
	case kindBinary:
		return arrow.BinaryTypes.Binary
	default:
		return arrow.BinaryTypes.String
	}
}

// inferColumnKinds picks a logical type per column from the values, promoting
// on conflict (int+float → float, anything else mixed → string).
func inferColumnKinds(result *query.QueryResult) []columnKind {
	kinds := make([]columnKind, len(result.Columns))
	seen := make([]bool, len(result.Columns))

	for _, row := range result.Rows {
		for col := range result.Columns {
			if col >= len(row) || row[col] == nil {
				continue
			}
			kind, ok := valueKind(row[col])
			if !ok {
				kind = kindString
			}
			if !seen[col] {
				kinds[col] = kind
				seen[col] = true
				continue
			}
			kinds[col] = mergeKinds(kinds[col], kind)
		}
	}

	// A DATE column's Go value is a midnight time.Time, indistinguishable from
	// a timestamp; use the connection's declared column type to narrow it.
	for col := range kinds {
		if kinds[col] != kindTimestamp || col >= len(result.ColumnTypes) {
			continue
		}
		declared := strings.ToLower(result.ColumnTypes[col])
		if declared == "date" || strings.HasPrefix(declared, "date(") || declared == "date32" {
			kinds[col] = kindDate
		}
	}
	return kinds
}

func mergeKinds(a, b columnKind) columnKind {
	if a == b {
		return a
	}
	if (a == kindInt64 && b == kindFloat64) || (a == kindFloat64 && b == kindInt64) {
		return kindFloat64
	}
	return kindString
}

func valueKind(value any) (columnKind, bool) {
	switch v := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32:
		return kindInt64, true
	case float32, float64:
		return kindFloat64, true
	case bool:
		return kindBool, true
	case time.Time:
		return kindTimestamp, true
	case []byte:
		return kindBinary, true
	case string:
		return kindString, true
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return kindInt64, true
		}
		if _, err := v.Float64(); err == nil {
			return kindFloat64, true
		}
		return kindString, true
	default:
		return kindString, false
	}
}

func appendValue(builder array.Builder, kind columnKind, value any) {
	if value == nil {
		builder.AppendNull()
		return
	}
	switch kind {
	case kindInt64:
		if v, ok := toInt64(value); ok {
			builder.(*array.Int64Builder).Append(v)
			return
		}
	case kindFloat64:
		if v, ok := toFloat64(value); ok {
			builder.(*array.Float64Builder).Append(v)
			return
		}
	case kindBool:
		if v, ok := value.(bool); ok {
			builder.(*array.BooleanBuilder).Append(v)
			return
		}
	case kindTimestamp:
		if v, ok := value.(time.Time); ok {
			builder.(*array.TimestampBuilder).Append(arrow.Timestamp(v.UTC().UnixMicro()))
			return
		}
	case kindDate:
		if v, ok := value.(time.Time); ok {
			builder.(*array.Date32Builder).Append(arrow.Date32FromTime(v))
			return
		}
	case kindBinary:
		if v, ok := value.([]byte); ok {
			builder.(*array.BinaryBuilder).Append(v)
			return
		}
	case kindString:
		builder.(*array.StringBuilder).Append(stringifyValue(value))
		return
	}
	builder.AppendNull()
}

func toInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case json.Number:
		parsed, err := v.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func toFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		if i, ok := toInt64(value); ok {
			return float64(i), true
		}
		return 0, false
	}
}

// stringifyValue renders a value the string column can hold: strings pass
// through, structured values (maps, slices — duckdb structs/lists) become
// JSON, everything else goes through fmt.
func stringifyValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case map[string]any, []any:
		if encoded, err := json.Marshal(v); err == nil {
			return string(encoded)
		}
	}
	return fmt.Sprintf("%v", value)
}
