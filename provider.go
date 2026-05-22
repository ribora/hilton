package hilton

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type TraceExporter interface {
	unwrap() sdktrace.SpanExporter
}

type MetricExporter interface {
	unwrap() sdkmetric.Exporter
}

func New(service, environment string, traceExp TraceExporter, metricExp MetricExporter) (Provider, error) {
	ctx := context.Background()

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(service),
			attribute.String("deployment.environment", environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("hilton resource: %w", err)
	}

	// Trace Provider
	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
	}
	if traceExp != nil {
		if raw := traceExp.unwrap(); raw != nil {
			tpOpts = append(tpOpts, sdktrace.WithBatcher(raw))
		}
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)

	// Meter Provider
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

	return &otelProvider{tp: tp, mp: mp, prop: prop}, nil
}
