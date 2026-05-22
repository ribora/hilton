package hilton

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type otelProvider struct {
	tp   *sdktrace.TracerProvider
	mp   *sdkmetric.MeterProvider
	prop propagation.TextMapPropagator
}

func (p *otelProvider) Tracer(name string) Tracer {
	return &otelTracer{t: p.tp.Tracer(name)}
}

func (p *otelProvider) Meter(name string) Meter {
	return &otelMeter{m: p.mp.Meter(name)}
}

func (p *otelProvider) Propagator() Propagator {
	return &otelPropagator{p: p.prop}
}

func (p *otelProvider) Shutdown(ctx context.Context) error {
	_ = p.mp.Shutdown(ctx)
	return p.tp.Shutdown(ctx)
}

type otelTracer struct {
	t oteltrace.Tracer
}

func (t *otelTracer) Start(ctx context.Context, name string, opts ...SpanStartOption) (context.Context, Span) {
	cfg := &SpanConfig{}
	for _, o := range opts {
		o(cfg)
	}

	otelOpts := []oteltrace.SpanStartOption{
		oteltrace.WithSpanKind(toOtelKind(cfg.Kind)),
		oteltrace.WithAttributes(toOtelAttrs(cfg.Attrs)...),
	}

	// Translate hilton's remote parent (set via ContextWithRemoteSpanContext)
	// back into the form OTel expects, so the SDK picks it up.
	if rsc, ok := remoteSpanContextFromContext(ctx); ok {
		otelSC := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
			TraceID:    rsc.TraceID(),
			SpanID:     rsc.SpanID(),
			TraceFlags: oteltrace.FlagsSampled,
			Remote:     true,
		})
		ctx = oteltrace.ContextWithRemoteSpanContext(ctx, otelSC)
	}

	ctx, otelSpan := t.t.Start(ctx, name, otelOpts...)
	s := &otelSpan_{s: otelSpan}
	ctx = ContextWithSpan(ctx, s)
	return ctx, s
}

type otelSpan_ struct {
	s oteltrace.Span
}

func (s *otelSpan_) End()                  { s.s.End() }
func (s *otelSpan_) IsRecording() bool     { return s.s.IsRecording() }
func (s *otelSpan_) RecordError(err error) { s.s.RecordError(err) }

func (s *otelSpan_) SetAttributes(attrs ...Attribute) {
	s.s.SetAttributes(toOtelAttrs(attrs)...)
}

func (s *otelSpan_) SetStatus(code StatusCode, desc string) {
	s.s.SetStatus(toOtelStatus(code), desc)
}

func (s *otelSpan_) SpanContext() SpanContext {
	return otelSpanContext{sc: s.s.SpanContext()}
}

type otelSpanContext struct {
	sc oteltrace.SpanContext
}

func (c otelSpanContext) TraceID() [16]byte { return c.sc.TraceID() }
func (c otelSpanContext) SpanID() [8]byte   { return c.sc.SpanID() }
func (c otelSpanContext) IsValid() bool     { return c.sc.IsValid() }
func (c otelSpanContext) IsSampled() bool   { return c.sc.IsSampled() }

type otelMeter struct {
	m metric.Meter
}

func (m *otelMeter) Counter(name string, opts ...MeterOption) Counter {
	cfg := resolveMeterConfig(opts)
	c, _ := m.m.Int64Counter(name,
		metric.WithDescription(cfg.Description),
		metric.WithUnit(cfg.Unit),
	)
	return &otelCounter{c: c}
}

func (m *otelMeter) Histogram(name string, opts ...MeterOption) Histogram {
	cfg := resolveMeterConfig(opts)
	h, _ := m.m.Float64Histogram(name,
		metric.WithDescription(cfg.Description),
		metric.WithUnit(cfg.Unit),
	)
	return &otelHistogram{h: h}
}

func resolveMeterConfig(opts []MeterOption) MeterConfig {
	cfg := MeterConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

type otelCounter struct {
	c metric.Int64Counter
}

func (c *otelCounter) Add(ctx context.Context, value int64, attrs ...Attribute) {
	safeCtx := context.Background()
	if b := baggage.FromContext(ctx); b.Len() > 0 {
		safeCtx = baggage.ContextWithBaggage(safeCtx, b)
	}
	c.c.Add(safeCtx, value, metric.WithAttributes(toOtelAttrs(attrs)...))
}

type otelHistogram struct {
	h metric.Float64Histogram
}

func (h *otelHistogram) Record(ctx context.Context, value float64, attrs ...Attribute) {
	safeCtx := context.Background()
	if b := baggage.FromContext(ctx); b.Len() > 0 {
		safeCtx = baggage.ContextWithBaggage(safeCtx, b)
	}
	h.h.Record(safeCtx, value, metric.WithAttributes(toOtelAttrs(attrs)...))
}

type otelPropagator struct {
	p propagation.TextMapPropagator
}

func (p *otelPropagator) Inject(ctx context.Context, carrier Carrier) {
	p.p.Inject(ctx, carrierBridge{carrier})
}

func (p *otelPropagator) Extract(ctx context.Context, carrier Carrier) context.Context {
	return p.p.Extract(ctx, carrierBridge{carrier})
}

type carrierBridge struct{ c Carrier }

func (b carrierBridge) Get(key string) string { return b.c.Get(key) }
func (b carrierBridge) Set(key, value string) { b.c.Set(key, value) }
func (b carrierBridge) Keys() []string        { return b.c.Keys() }

var _ propagation.TextMapCarrier = carrierBridge{}

func toOtelKind(k SpanKind) oteltrace.SpanKind {
	switch k {
	case SpanKindServer:
		return oteltrace.SpanKindServer
	case SpanKindClient:
		return oteltrace.SpanKindClient
	case SpanKindProducer:
		return oteltrace.SpanKindProducer
	case SpanKindConsumer:
		return oteltrace.SpanKindConsumer
	default:
		return oteltrace.SpanKindInternal
	}
}

func toOtelStatus(c StatusCode) otelcodes.Code {
	switch c {
	case StatusOK:
		return otelcodes.Ok
	case StatusError:
		return otelcodes.Error
	default:
		return otelcodes.Unset
	}
}

func toOtelAttrs(attrs []Attribute) []attribute.KeyValue {
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
		}
	}
	return out
}
