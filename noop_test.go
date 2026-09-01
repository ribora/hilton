package hilton

import (
	"context"
	"errors"
	"testing"
)

// This is the default every consumer gets, so a nil slipping through is a panic
// in a service that merely left telemetry unconfigured.
func TestNoopProviderIsFullyUsable(t *testing.T) {
	ctx := context.Background()
	p := Noop()

	spanCtx, span := p.Tracer("test").Start(ctx, "op",
		WithSpanKind(SpanKindServer),
		WithAttributes(Attr("k", "v")),
	)
	if spanCtx != ctx {
		t.Error("noop tracer replaced the context")
	}
	if span.IsRecording() {
		t.Error("noop span reports itself as recording")
	}
	span.SetName("renamed")
	span.SetAttributes(Attr("n", 1))
	span.RecordError(errors.New("boom"))
	span.SetStatus(StatusError, "boom")
	span.End()

	m := p.Meter("test")
	m.Counter("requests", WithDescription("requests served"), WithUnit("1")).Add(ctx, 1, Attr("k", "v"))
	m.UpDownCounter("inflight").Add(ctx, 1)
	m.UpDownCounter("inflight").Add(ctx, -1)
	m.Histogram("latency", WithUnit("s"), WithSecondsBoundaries()).Record(ctx, 1.5, Attr("k", "v"))

	reg, err := m.ObservableGauge("pool.size", func(o Observer) {
		o.Observe(3, Attr("k", "v"))
	})
	if err != nil {
		t.Errorf("ObservableGauge: %v", err)
	}
	if reg == nil {
		t.Fatal("ObservableGauge returned a nil registration")
	}
	if err := reg.Unregister(); err != nil {
		t.Errorf("Unregister: %v", err)
	}

	if err := p.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// Whatever the callback reads may not even be constructed when telemetry is off.
func TestNoopObservableGaugeNeverObserves(t *testing.T) {
	var called bool
	reg, err := Noop().Meter("test").ObservableGauge("pool.size", func(Observer) {
		called = true
	})
	if err != nil {
		t.Fatalf("ObservableGauge: %v", err)
	}
	if err := reg.Unregister(); err != nil {
		t.Errorf("Unregister: %v", err)
	}
	if called {
		t.Error("noop provider invoked the observe callback")
	}
}

func TestNoopSpanContextIsInvalid(t *testing.T) {
	sc := noopSpan{}.SpanContext()
	if sc.IsValid() {
		t.Error("IsValid = true for the noop span context")
	}
	if sc.IsSampled() {
		t.Error("IsSampled = true for the noop span context")
	}
	if sc.TraceID() != [16]byte{} {
		t.Errorf("TraceID = %x, want all zero", sc.TraceID())
	}
	if sc.SpanID() != [8]byte{} {
		t.Errorf("SpanID = %x, want all zero", sc.SpanID())
	}
}

// Instrumentation injects unconditionally, so a noop must not leave half a
// trace context on the wire.
func TestNoopPropagator(t *testing.T) {
	ctx := context.Background()
	prop := Noop().Propagator()

	carrier := mapCarrier{}
	prop.Inject(ctx, carrier)
	if len(carrier) != 0 {
		t.Errorf("noop propagator wrote %v", carrier)
	}
	if got := prop.Extract(ctx, carrier); got != ctx {
		t.Error("noop propagator replaced the context")
	}
}
