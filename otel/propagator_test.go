package otel

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	oteltrace "go.opentelemetry.io/otel/trace"

	"gitlab.com/gorib/hilton"
)

// The two halves meet through a Carrier the two services share no code for, so
// a round trip is the only check that the bridge drops nothing.
func TestPropagatorRoundTrip(t *testing.T) {
	p, _ := newTraceProvider(t)
	prop := p.Propagator()

	ctx, span := p.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	carrier := mapCarrier{}
	prop.Inject(ctx, carrier)
	if carrier.Get("traceparent") == "" {
		t.Fatalf("nothing injected: %v", carrier)
	}

	got := oteltrace.SpanContextFromContext(prop.Extract(context.Background(), carrier))
	want := span.SpanContext()
	if got.TraceID() != oteltrace.TraceID(want.TraceID()) {
		t.Errorf("TraceID = %s, want %x", got.TraceID(), want.TraceID())
	}
	if got.SpanID() != oteltrace.SpanID(want.SpanID()) {
		t.Errorf("SpanID = %s, want %x", got.SpanID(), want.SpanID())
	}
	if !got.IsRemote() {
		t.Error("IsRemote = false for an extracted span context")
	}
	if got.IsSampled() != want.IsSampled() {
		t.Errorf("IsSampled = %v, want %v", got.IsSampled(), want.IsSampled())
	}
}

// The propagator is composite, so baggage has to ride along with trace context.
func TestPropagatorCarriesBaggage(t *testing.T) {
	p, _ := newTraceProvider(t)
	prop := p.Propagator()

	member, err := baggage.NewMember("tenant", "acme")
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	b, err := baggage.New(member)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := baggage.ContextWithBaggage(context.Background(), b)

	carrier := mapCarrier{}
	prop.Inject(ctx, carrier)
	if carrier.Get("baggage") == "" {
		t.Fatalf("no baggage injected: %v", carrier)
	}

	out := prop.Extract(context.Background(), carrier)
	if got := baggage.FromContext(out).Member("tenant").Value(); got != "acme" {
		t.Errorf("baggage tenant = %q, want acme", got)
	}
}

// An empty carrier must not yield an invalid parent.
func TestPropagatorExtractEmptyCarrier(t *testing.T) {
	p, _ := newTraceProvider(t)

	out := p.Propagator().Extract(context.Background(), mapCarrier{})
	if sc := oteltrace.SpanContextFromContext(out); sc.IsValid() {
		t.Errorf("extracted a valid span context from an empty carrier: %v", sc)
	}
}

// Keys is what the composite propagator needs and hilton.Carrier is its only
// source.
func TestCarrierBridge(t *testing.T) {
	var c hilton.Carrier = mapCarrier{"traceparent": "tp"}
	b := carrierBridge{c}

	b.Set("baggage", "tenant=acme")
	if got := b.Get("baggage"); got != "tenant=acme" {
		t.Errorf("Get(baggage) = %q, want tenant=acme", got)
	}
	if got := b.Get("absent"); got != "" {
		t.Errorf("Get(absent) = %q, want empty", got)
	}

	keys := b.Keys()
	slices.Sort(keys)
	if want := []string{"baggage", "traceparent"}; !slices.Equal(keys, want) {
		t.Errorf("Keys = %v, want %v", keys, want)
	}
}
