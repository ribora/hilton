package otel

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"gitlab.com/gorib/hilton"
)

// A type switch, so a missing case costs an attribute at run time rather than a
// compile error.
func TestToOtelAttrsCoversEveryType(t *testing.T) {
	in := []hilton.Attribute{
		hilton.Attr("string", "s"),
		hilton.Attr("int", 1),
		hilton.Attr("int64", int64(2)),
		hilton.Attr("bool", true),
		hilton.Attr("float64", 3.5),
		hilton.Attr("[]string", []string{"a", "b"}),
		hilton.Attr("[]int", []int{1, 2}),
		hilton.Attr("[]int64", []int64{3, 4}),
		hilton.Attr("[]bool", []bool{true, false}),
		hilton.Attr("[]float64", []float64{5.5, 6.5}),
	}
	want := []attribute.KeyValue{
		attribute.String("string", "s"),
		attribute.Int("int", 1),
		attribute.Int64("int64", 2),
		attribute.Bool("bool", true),
		attribute.Float64("float64", 3.5),
		attribute.StringSlice("[]string", []string{"a", "b"}),
		attribute.IntSlice("[]int", []int{1, 2}),
		attribute.Int64Slice("[]int64", []int64{3, 4}),
		attribute.BoolSlice("[]bool", []bool{true, false}),
		attribute.Float64Slice("[]float64", []float64{5.5, 6.5}),
	}

	got := toOtelAttrs(in)
	if len(got) != len(in) {
		t.Fatalf("mapped %d of %d attributes", len(got), len(in))
	}
	for i := range want {
		if got[i].Key != want[i].Key {
			t.Errorf("attr %d key = %q, want %q", i, got[i].Key, want[i].Key)
			continue
		}
		if got[i].Value.Type() != want[i].Value.Type() {
			t.Errorf("attr %q type = %v, want %v", got[i].Key, got[i].Value.Type(), want[i].Value.Type())
		}
		if got[i].Value.String() != want[i].Value.String() {
			t.Errorf("attr %q = %s, want %s", got[i].Key, got[i].Value.String(), want[i].Value.String())
		}
	}
}

// The zero value is all that can still reach the switch's default now that Attr
// is the only constructor — it must be dropped, not mapped to an empty string.
func TestToOtelAttrsDropsZeroValue(t *testing.T) {
	got := toOtelAttrs([]hilton.Attribute{
		hilton.Attr("known", "s"),
		{},
	})

	if len(got) != 1 {
		t.Fatalf("mapped %d attributes, want 1", len(got))
	}
	if got[0].Key != "known" {
		t.Errorf("kept %q, want known", got[0].Key)
	}
}

func TestToOtelAttrsEmpty(t *testing.T) {
	if got := toOtelAttrs(nil); len(got) != 0 {
		t.Errorf("toOtelAttrs(nil) = %v, want empty", got)
	}
}
