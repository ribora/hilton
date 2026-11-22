package otel

import (
	"context"
	"errors"
	"fmt"

	otelapi "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/ribora/hilton"
)

type config struct {
	attributes   []attribute.KeyValue
	sampler      sdktrace.Sampler
	errorHandler func(error)
}

type ProviderOption func(*config) error

func WithInstanceID(id string) ProviderOption {
	return func(c *config) error {
		if id == "" {
			return nil
		}
		c.attributes = append(c.attributes, semconv.ServiceInstanceID(id))
		return nil
	}
}

// ParentBased: honor upstream trace decisions, ratio-sample only root spans.
func WithSamplingRatio(rate float64) ProviderOption {
	return func(c *config) error {
		if rate < 0 || rate > 1 {
			return fmt.Errorf("sampling ratio must be in [0, 1], got %v", rate)
		}
		c.sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(rate))
		return nil
	}
}

func WithResource(attrs ...hilton.Attribute) ProviderOption {
	return func(c *config) error {
		c.attributes = append(c.attributes, toOtelAttrs(attrs)...)
		return nil
	}
}

// A refused export reaches nothing else — not even Shutdown's error — so
// without a handler a service stops exporting silently. Process-global, so only
// the code owning start-up should set it.
func WithErrorHandler(handle func(error)) ProviderOption {
	return func(c *config) error {
		if handle == nil {
			return errors.New("hilton: nil error handler")
		}
		c.errorHandler = handle
		return nil
	}
}

func NewProvider(service, environment string, traceExp TraceExporter, metricExp MetricExporter, options ...ProviderOption) (hilton.Provider, error) {
	ctx := context.Background()

	cfg := &config{
		attributes: []attribute.KeyValue{
			semconv.ServiceName(service),
			semconv.DeploymentEnvironment(environment),
		},
	}
	for _, option := range options {
		if err := option(cfg); err != nil {
			return nil, err
		}
	}

	if cfg.errorHandler != nil {
		otelapi.SetErrorHandler(otelapi.ErrorHandlerFunc(cfg.errorHandler))
	}

	res, err := sdkresource.New(ctx, sdkresource.WithAttributes(cfg.attributes...))
	if err != nil {
		return nil, fmt.Errorf("hilton resource: %w", err)
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
	}
	if cfg.sampler != nil {
		tpOpts = append(tpOpts, sdktrace.WithSampler(cfg.sampler))
	}
	if traceExp != nil {
		if raw := traceExp.unwrap(); raw != nil {
			tpOpts = append(tpOpts, sdktrace.WithBatcher(raw))
		}
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)

	mpOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}
	if metricExp != nil {
		if raw := metricExp.unwrap(); raw != nil {
			mpOpts = append(mpOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(raw)))
		}
	}
	mp := sdkmetric.NewMeterProvider(mpOpts...)

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	return &provider{tp: tp, mp: mp, prop: prop}, nil
}

type provider struct {
	tp   *sdktrace.TracerProvider
	mp   *sdkmetric.MeterProvider
	prop propagation.TextMapPropagator
}

func (p *provider) Tracer(name string) hilton.Tracer {
	return &tracer{t: p.tp.Tracer(name)}
}

func (p *provider) Meter(name string) hilton.Meter {
	return &meter{m: p.mp.Meter(name)}
}

func (p *provider) Propagator() hilton.Propagator {
	return &propagator{p: p.prop}
}

func (p *provider) Shutdown(ctx context.Context) error {
	_ = p.mp.Shutdown(ctx)
	return p.tp.Shutdown(ctx)
}

func (p *provider) Close() error {
	_ = p.mp.Shutdown(context.Background())
	return p.tp.Shutdown(context.Background())
}
