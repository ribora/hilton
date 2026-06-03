package otel

import (
	"context"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/metric"

	"gitlab.com/gorib/hilton"
)

type meter struct {
	m metric.Meter
}

func (m *meter) Counter(name string, opts ...hilton.MeterOption) hilton.Counter {
	cfg := resolveMeterConfig(opts)
	c, _ := m.m.Int64Counter(name,
		metric.WithDescription(cfg.Description),
		metric.WithUnit(cfg.Unit),
	)
	return &counter{c: c}
}

func (m *meter) Histogram(name string, opts ...hilton.MeterOption) hilton.Histogram {
	cfg := resolveMeterConfig(opts)
	h, _ := m.m.Float64Histogram(name,
		metric.WithDescription(cfg.Description),
		metric.WithUnit(cfg.Unit),
	)
	return &histogram{h: h}
}

func resolveMeterConfig(opts []hilton.MeterOption) hilton.MeterConfig {
	cfg := hilton.MeterConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

type counter struct {
	c metric.Int64Counter
}

func (c *counter) Add(ctx context.Context, value int64, attrs ...hilton.Attribute) {
	c.c.Add(safeBaggageCtx(ctx), value, metric.WithAttributes(toOtelAttrs(attrs)...))
}

type histogram struct {
	h metric.Float64Histogram
}

func (h *histogram) Record(ctx context.Context, value float64, attrs ...hilton.Attribute) {
	h.h.Record(safeBaggageCtx(ctx), value, metric.WithAttributes(toOtelAttrs(attrs)...))
}

// Strip ctx to baggage only — instruments live past request lifetime.
func safeBaggageCtx(ctx context.Context) context.Context {
	safe := context.Background()
	if b := baggage.FromContext(ctx); b.Len() > 0 {
		safe = baggage.ContextWithBaggage(safe, b)
	}
	return safe
}
