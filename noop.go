package hilton

import "context"

func Noop() Provider { return noopProvider{} }

type noopProvider struct{}

func (noopProvider) Tracer(_ string) Tracer           { return noopTracer{} }
func (noopProvider) Meter(_ string) Meter             { return noopMeter{} }
func (noopProvider) Propagator() Propagator           { return noopPropagator{} }
func (noopProvider) Shutdown(_ context.Context) error { return nil }

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ string, _ ...SpanStartOption) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) End()                             {}
func (noopSpan) SetName(_ string)                 {}
func (noopSpan) SetAttributes(_ ...Attribute)     {}
func (noopSpan) RecordError(_ error)              {}
func (noopSpan) SetStatus(_ StatusCode, _ string) {}
func (noopSpan) SpanContext() SpanContext         { return noopSpanContext{} }
func (noopSpan) IsRecording() bool                { return false }

type noopSpanContext struct{}

func (noopSpanContext) TraceID() [16]byte { return [16]byte{} }
func (noopSpanContext) SpanID() [8]byte   { return [8]byte{} }
func (noopSpanContext) IsValid() bool     { return false }
func (noopSpanContext) IsSampled() bool   { return false }

type noopMeter struct{}

func (noopMeter) Counter(_ string, _ ...MeterOption) Counter     { return noopCounter{} }
func (noopMeter) Histogram(_ string, _ ...MeterOption) Histogram { return noopHistogram{} }

type noopCounter struct{}

func (noopCounter) Add(_ context.Context, _ int64, _ ...Attribute) {}

type noopHistogram struct{}

func (noopHistogram) Record(_ context.Context, _ float64, _ ...Attribute) {}

type noopPropagator struct{}

func (noopPropagator) Inject(_ context.Context, _ Carrier)                    {}
func (noopPropagator) Extract(ctx context.Context, _ Carrier) context.Context { return ctx }
