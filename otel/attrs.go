package otel

import (
	"go.opentelemetry.io/otel/attribute"

	"gitlab.com/gorib/hilton"
)

func toOtelAttrs(attrs []hilton.Attribute) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		switch v := a.Value.(type) {
		case string:
			out = append(out, attribute.String(a.Key, v))
		case int:
			out = append(out, attribute.Int(a.Key, v))
		case int64:
			out = append(out, attribute.Int64(a.Key, v))
		case bool:
			out = append(out, attribute.Bool(a.Key, v))
		case float64:
			out = append(out, attribute.Float64(a.Key, v))
		case []string:
			out = append(out, attribute.StringSlice(a.Key, v))
		case []int:
			out = append(out, attribute.IntSlice(a.Key, v))
		case []int64:
			out = append(out, attribute.Int64Slice(a.Key, v))
		case []bool:
			out = append(out, attribute.BoolSlice(a.Key, v))
		case []float64:
			out = append(out, attribute.Float64Slice(a.Key, v))
		}
	}
	return out
}
