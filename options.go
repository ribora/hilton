package hilton

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type config struct {
	service     string
	environment string
	endpoint    string // OTLP gRPC; empty = noop exporters
}

type Option func(*config)

func WithService(name string) Option {
	return func(c *config) { c.service = name }
}

func WithEnvironment(env string) Option {
	return func(c *config) { c.environment = env }
}

// WithOtlpGrpc sets the OTLP gRPC endpoint (e.g. "localhost:4317"). Empty
// means traces/metrics are created and propagated but not exported.
func WithOtlpGrpc(endpoint string) Option {
	return func(c *config) { c.endpoint = endpoint }
}

func New(opts ...Option) (Provider, error) {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}

	ctx := context.Background()

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(cfg.service),
			attribute.String("deployment.environment", cfg.environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("hilton resource: %w", err)
	}

	tp, err := buildTracerProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("hilton tracer: %w", err)
	}

	mp, err := buildMeterProvider(ctx, cfg, res)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("hilton meter: %w", err)
	}

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	return &otelProvider{tp: tp, mp: mp, prop: prop}, nil
}

func buildTracerProvider(ctx context.Context, cfg *config, res *sdkresource.Resource) (*sdktrace.TracerProvider, error) {
	if cfg.endpoint == "" {
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res)), nil
	}
	exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(cfg.endpoint), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	), nil
}

func buildMeterProvider(ctx context.Context, cfg *config, res *sdkresource.Resource) (*sdkmetric.MeterProvider, error) {
	if cfg.endpoint == "" {
		return sdkmetric.NewMeterProvider(sdkmetric.WithResource(res)), nil
	}
	exp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(cfg.endpoint), otlpmetricgrpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
		sdkmetric.WithResource(res),
	), nil
}
