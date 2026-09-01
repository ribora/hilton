package otel

import (
	"context"
	"fmt"

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

func (m *meter) UpDownCounter(name string, opts ...hilton.MeterOption) hilton.UpDownCounter {
	cfg := resolveMeterConfig(opts)
	c, _ := m.m.Int64UpDownCounter(name,
		metric.WithDescription(cfg.Description),
		metric.WithUnit(cfg.Unit),
	)
	return &upDownCounter{c: c}
}

func (m *meter) Histogram(name string, opts ...hilton.MeterOption) hilton.Histogram {
	cfg := resolveMeterConfig(opts)
	histOpts := []metric.Float64HistogramOption{
		metric.WithDescription(cfg.Description),
		metric.WithUnit(cfg.Unit),
	}
	if len(cfg.Boundaries) > 0 {
		histOpts = append(histOpts, metric.WithExplicitBucketBoundaries(cfg.Boundaries...))
	}
	h, _ := m.m.Float64Histogram(name, histOpts...)
	return &histogram{h: h}
}

func (m *meter) ObservableGauge(name string, observe func(hilton.Observer), opts ...hilton.MeterOption) (hilton.Registration, error) {
	cfg := resolveMeterConfig(opts)
	g, err := m.m.Int64ObservableGauge(name,
		metric.WithDescription(cfg.Description),
		metric.WithUnit(cfg.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("hilton gauge %s: %w", name, err)
	}

	reg, err := m.m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		observe(observer{gauge: g, out: o})
		return nil
	}, g)
	if err != nil {
		return nil, fmt.Errorf("hilton gauge %s callback: %w", name, err)
	}
	return registration{reg: reg}, nil
}

type observer struct {
	gauge metric.Int64ObservableGauge
	out   metric.Observer
}

func (o observer) Observe(value int64, attrs ...hilton.Attribute) {
	o.out.ObserveInt64(o.gauge, value, metric.WithAttributes(toOtelAttrs(attrs)...))
}

type registration struct {
	reg metric.Registration
}

func (r registration) Unregister() error { return r.reg.Unregister() }

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

type upDownCounter struct {
	c metric.Int64UpDownCounter
}

func (c *upDownCounter) Add(ctx context.Context, value int64, attrs ...hilton.Attribute) {
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
