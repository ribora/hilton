package hilton

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

func NewInsecureCredentials() credentials {
	return credentials{Insecure: true}
}

func NewCredentials(headers map[string]string) credentials {
	return credentials{Headers: headers}
}

type credentials struct {
	Headers  map[string]string
	Insecure bool
}

func NewGrpcTraceExporter(endpoint string, creds credentials) (TraceExporter, error) {
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
	return &grpcTraceExporter{exp: exp}, nil
}

func NewHttpTraceExporter(endpoint string, creds credentials) (TraceExporter, error) {
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
	return &grpcTraceExporter{exp: exp}, nil
}

type grpcTraceExporter struct {
	exp sdktrace.SpanExporter
}

func (e *grpcTraceExporter) unwrap() sdktrace.SpanExporter { return e.exp }

func NewGrpcMetricExporter(endpoint string, creds credentials) (MetricExporter, error) {
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
	return &grpcMetricExporter{exp: exp}, nil
}

func NewHttpMetricExporter(endpoint string, creds credentials) (MetricExporter, error) {
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
	return &grpcMetricExporter{exp: exp}, nil
}

type grpcMetricExporter struct {
	exp sdkmetric.Exporter
}

func (e *grpcMetricExporter) unwrap() sdkmetric.Exporter { return e.exp }
