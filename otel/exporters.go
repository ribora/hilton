package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type TraceExporter interface {
	unwrap() sdktrace.SpanExporter
}

type MetricExporter interface {
	unwrap() sdkmetric.Exporter
}

func NewInsecureCredentials() Credentials {
	return Credentials{Insecure: true}
}

func NewCredentials(headers map[string]string) Credentials {
	return Credentials{Headers: headers}
}

type Credentials struct {
	Headers  map[string]string
	Insecure bool
}

func NewGrpcTraceExporter(endpoint string, creds Credentials) (TraceExporter, error) {
	if endpoint == "" {
		return nil, nil
	}
	ctx := context.Background()
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if creds.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	if len(creds.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(creds.Headers))
	}

	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("trace exporter: %w", err)
	}
	return &traceExporter{exp: exp}, nil
}

func NewHttpTraceExporter(endpoint string, creds Credentials) (TraceExporter, error) {
	if endpoint == "" {
		return nil, nil
	}
	ctx := context.Background()
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
	}
	if creds.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if len(creds.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(creds.Headers))
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("trace exporter: %w", err)
	}
	return &traceExporter{exp: exp}, nil
}

type traceExporter struct {
	exp sdktrace.SpanExporter
}

func (e *traceExporter) unwrap() sdktrace.SpanExporter { return e.exp }

func NewGrpcMetricExporter(endpoint string, creds Credentials) (MetricExporter, error) {
	if endpoint == "" {
		return nil, nil
	}
	ctx := context.Background()
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(endpoint),
	}
	if creds.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	if len(creds.Headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(creds.Headers))
	}

	exp, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("metric exporter: %w", err)
	}
	return &metricExporter{exp: exp}, nil
}

func NewHttpMetricExporter(endpoint string, creds Credentials) (MetricExporter, error) {
	if endpoint == "" {
		return nil, nil
	}
	ctx := context.Background()
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
	}
	if creds.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	if len(creds.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(creds.Headers))
	}

	exp, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("metric exporter: %w", err)
	}
	return &metricExporter{exp: exp}, nil
}

type metricExporter struct {
	exp sdkmetric.Exporter
}

func (e *metricExporter) unwrap() sdkmetric.Exporter { return e.exp }
