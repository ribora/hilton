package otel

import (
	"go.opentelemetry.io/otel/attribute"

	"github.com/ribora/hilton"
)

func toOtelAttrs(attrs []hilton.Attribute) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		key := a.Key()
		switch v := a.Value().(type) {
		case string:
			out = append(out, attribute.String(key, v))
		case int:
			out = append(out, attribute.Int(key, v))
		case int64:
			out = append(out, attribute.Int64(key, v))
		case bool:
			out = append(out, attribute.Bool(key, v))
		case float64:
			out = append(out, attribute.Float64(key, v))
		case []string:
			out = append(out, attribute.StringSlice(key, v))
		case []int:
			out = append(out, attribute.IntSlice(key, v))
		case []int64:
			out = append(out, attribute.Int64Slice(key, v))
		case []bool:
			out = append(out, attribute.BoolSlice(key, v))
		case []float64:
			out = append(out, attribute.Float64Slice(key, v))
		}
	}
	return out
}
