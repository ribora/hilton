package otel

import (
	"context"

	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"gitlab.com/gorib/hilton"
)

type tracer struct {
	t oteltrace.Tracer
}

func (t *tracer) Start(ctx context.Context, name string, opts ...hilton.SpanStartOption) (context.Context, hilton.Span) {
	cfg := &hilton.SpanConfig{}
	for _, o := range opts {
		o(cfg)
	}

	otelOpts := []oteltrace.SpanStartOption{
		oteltrace.WithSpanKind(toOtelKind(cfg.Kind)),
		oteltrace.WithAttributes(toOtelAttrs(cfg.Attrs)...),
	}

	if rsc, ok := hilton.RemoteSpanContextFromContext(ctx); ok {
		otelSC := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
			TraceID:    rsc.TraceID(),
			SpanID:     rsc.SpanID(),
			TraceFlags: oteltrace.FlagsSampled,
			Remote:     true,
		})
		ctx = oteltrace.ContextWithRemoteSpanContext(ctx, otelSC)
	}

	ctx, otelSpan := t.t.Start(ctx, name, otelOpts...)
	s := &span{s: otelSpan}
	ctx = hilton.ContextWithSpan(ctx, s)
	return ctx, s
}

type span struct {
	s oteltrace.Span
}

func (s *span) End()                  { s.s.End() }
func (s *span) IsRecording() bool     { return s.s.IsRecording() }
func (s *span) RecordError(err error) { s.s.RecordError(err) }
func (s *span) SetName(name string)   { s.s.SetName(name) }

func (s *span) SetAttributes(attrs ...hilton.Attribute) {
	s.s.SetAttributes(toOtelAttrs(attrs)...)
}

func (s *span) SetStatus(code hilton.StatusCode, desc string) {
	s.s.SetStatus(toOtelStatus(code), desc)
}

func (s *span) SpanContext() hilton.SpanContext {
	return spanContext{sc: s.s.SpanContext()}
}

type spanContext struct {
	sc oteltrace.SpanContext
}

func (c spanContext) TraceID() [16]byte { return c.sc.TraceID() }
func (c spanContext) SpanID() [8]byte   { return c.sc.SpanID() }
func (c spanContext) IsValid() bool     { return c.sc.IsValid() }
func (c spanContext) IsSampled() bool   { return c.sc.IsSampled() }

func toOtelKind(k hilton.SpanKind) oteltrace.SpanKind {
	switch k {
	case hilton.SpanKindServer:
		return oteltrace.SpanKindServer
	case hilton.SpanKindClient:
		return oteltrace.SpanKindClient
	case hilton.SpanKindProducer:
		return oteltrace.SpanKindProducer
	case hilton.SpanKindConsumer:
		return oteltrace.SpanKindConsumer
	default:
		return oteltrace.SpanKindInternal
	}
}

func toOtelStatus(c hilton.StatusCode) otelcodes.Code {
	switch c {
	case hilton.StatusOK:
		return otelcodes.Ok
	case hilton.StatusError:
		return otelcodes.Error
	default:
		return otelcodes.Unset
	}
}
