package hilton

import (
	"fmt"
	"slices"
	"testing"
)

type mapCarrier map[string]string

func (c mapCarrier) Get(key string) string { return c[key] }
func (c mapCarrier) Set(key, value string) { c[key] = value }

func (c mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// An identity to hand the context helpers; behaviour comes from noopSpan.
type fakeSpan struct{ noopSpan }

// Must append, not replace: a layer adding its own attrs would otherwise drop
// the caller's.
func TestSpanStartOptionsAccumulate(t *testing.T) {
	cfg := &SpanConfig{}
	for _, o := range []SpanStartOption{
		WithSpanKind(SpanKindConsumer),
		WithAttributes(Attr("a", 1)),
		WithAttributes(Attr("b", "two"), Attr("c", true)),
		WithLinks(Link{SpanContext: noopSpanContext{}}),
		WithLinks(Link{SpanContext: noopSpanContext{}}),
	} {
		o(cfg)
	}

	if cfg.Kind != SpanKindConsumer {
		t.Errorf("Kind = %v, want %v", cfg.Kind, SpanKindConsumer)
	}
	if len(cfg.Links) != 2 {
		t.Errorf("got %d links, want 2 — WithLinks must append", len(cfg.Links))
	}
	want := []Attribute{{"a", 1}, {"b", "two"}, {"c", true}}
	if len(cfg.Attrs) != len(want) {
		t.Fatalf("got %d attrs, want %d", len(cfg.Attrs), len(want))
	}
	for i := range want {
		if cfg.Attrs[i] != want[i] {
			t.Errorf("Attrs[%d] = %v, want %v", i, cfg.Attrs[i], want[i])
		}
	}
}

func TestMeterOptions(t *testing.T) {
	cfg := MeterConfig{}
	WithDescription("requests served")(&cfg)
	WithUnit("1")(&cfg)
	WithBoundaries(0.1, 0.5, 1)(&cfg)

	if cfg.Description != "requests served" {
		t.Errorf("Description = %q, want %q", cfg.Description, "requests served")
	}
	if cfg.Unit != "1" {
		t.Errorf("Unit = %q, want %q", cfg.Unit, "1")
	}
	if !slices.Equal(cfg.Boundaries, []float64{0.1, 0.5, 1}) {
		t.Errorf("Boundaries = %v, want [0.1 0.5 1]", cfg.Boundaries)
	}
}

// The resolution has to sit under a second, which is where the requests are.
func TestWithSecondsBoundaries(t *testing.T) {
	cfg := MeterConfig{}
	WithSecondsBoundaries()(&cfg)

	if len(cfg.Boundaries) == 0 {
		t.Fatal("WithSecondsBoundaries set no boundaries")
	}
	if !slices.IsSorted(cfg.Boundaries) {
		t.Errorf("boundaries not ascending: %v", cfg.Boundaries)
	}
	if first := cfg.Boundaries[0]; first > 0.01 {
		t.Errorf("first boundary = %v, want single-digit milliseconds", first)
	}
	if last := cfg.Boundaries[len(cfg.Boundaries)-1]; last < 10 {
		t.Errorf("last boundary = %v, want at least 10s of headroom", last)
	}

	var sub int
	for _, b := range cfg.Boundaries {
		if b < 1 {
			sub++
		}
	}
	if sub < 5 {
		t.Errorf("only %d boundaries below 1s, want the resolution to live there", sub)
	}
}

// Replaces rather than appends, so a layer can override the default set.
func TestWithBoundariesReplaces(t *testing.T) {
	cfg := MeterConfig{}
	WithSecondsBoundaries()(&cfg)
	WithBoundaries(1, 2)(&cfg)

	if !slices.Equal(cfg.Boundaries, []float64{1, 2}) {
		t.Errorf("Boundaries = %v, want [1 2]", cfg.Boundaries)
	}
}

// Width must survive: an int64 that collapses to int maps to a different otel
// attribute type.
func TestAttrKeepsValueType(t *testing.T) {
	for _, tc := range []struct {
		attr Attribute
		want string
	}{
		{Attr("k", "s"), "string"},
		{Attr("k", 1), "int"},
		{Attr("k", int64(1)), "int64"},
		{Attr("k", true), "bool"},
		{Attr("k", 1.5), "float64"},
		{Attr("k", []string{"s"}), "[]string"},
		{Attr("k", []int{1}), "[]int"},
		{Attr("k", []int64{1}), "[]int64"},
		{Attr("k", []bool{true}), "[]bool"},
		{Attr("k", []float64{1.5}), "[]float64"},
	} {
		if tc.attr.Key() != "k" {
			t.Errorf("Key = %q, want k", tc.attr.Key())
		}
		if got := fmt.Sprintf("%T", tc.attr.Value()); got != tc.want {
			t.Errorf("Attr stored %s, want %s", got, tc.want)
		}
	}
}
