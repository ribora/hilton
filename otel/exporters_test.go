package otel

import (
	"context"
	"testing"
)

// Nil and no error, so wiring code needs no branch for "telemetry off".
func TestExportersEmptyEndpoint(t *testing.T) {
	creds := NewInsecureCredentials()

	if exp, err := NewGrpcTraceExporter("", creds); exp != nil || err != nil {
		t.Errorf("NewGrpcTraceExporter(\"\") = %v, %v, want nil, nil", exp, err)
	}
	if exp, err := NewHttpTraceExporter("", creds); exp != nil || err != nil {
		t.Errorf("NewHttpTraceExporter(\"\") = %v, %v, want nil, nil", exp, err)
	}
	if exp, err := NewGrpcMetricExporter("", creds); exp != nil || err != nil {
		t.Errorf("NewGrpcMetricExporter(\"\") = %v, %v, want nil, nil", exp, err)
	}
	if exp, err := NewHttpMetricExporter("", creds); exp != nil || err != nil {
		t.Errorf("NewHttpMetricExporter(\"\") = %v, %v, want nil, nil", exp, err)
	}
}

// The other end of that: a nil exporter must survive all the way to a working
// provider.
func TestProviderAcceptsNilExporters(t *testing.T) {
	traceExp, err := NewHttpTraceExporter("", NewInsecureCredentials())
	if err != nil {
		t.Fatalf("NewHttpTraceExporter: %v", err)
	}
	metricExp, err := NewHttpMetricExporter("", NewInsecureCredentials())
	if err != nil {
		t.Fatalf("NewHttpMetricExporter: %v", err)
	}

	p, err := NewProvider("svc", "test", traceExp, metricExp)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	_, span := p.Tracer("test").Start(context.Background(), "op")
	span.End()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// No I/O: both transports connect lazily. Headers take a branch of their own
// from the insecure flag, hence both credential shapes.
func TestExportersBuild(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		endpoint string
		creds    Credentials
		trace    func(string, Credentials) (TraceExporter, error)
		metric   func(string, Credentials) (MetricExporter, error)
	}{
		{"grpc insecure", "localhost:4317", NewInsecureCredentials(), NewGrpcTraceExporter, NewGrpcMetricExporter},
		{"grpc headers", "localhost:4317", NewCredentials(map[string]string{"x-tenant": "acme"}), NewGrpcTraceExporter, NewGrpcMetricExporter},
		{"http insecure", "localhost:4318", NewInsecureCredentials(), NewHttpTraceExporter, NewHttpMetricExporter},
		{"http headers", "localhost:4318", NewCredentials(map[string]string{"x-tenant": "acme"}), NewHttpTraceExporter, NewHttpMetricExporter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			traceExp, err := tc.trace(tc.endpoint, tc.creds)
			if err != nil {
				t.Fatalf("trace exporter: %v", err)
			}
			if traceExp == nil || traceExp.unwrap() == nil {
				t.Fatal("nil trace exporter for a non-empty endpoint")
			}
			t.Cleanup(func() { _ = traceExp.unwrap().Shutdown(ctx) })

			metricExp, err := tc.metric(tc.endpoint, tc.creds)
			if err != nil {
				t.Fatalf("metric exporter: %v", err)
			}
			if metricExp == nil || metricExp.unwrap() == nil {
				t.Fatal("nil metric exporter for a non-empty endpoint")
			}
			t.Cleanup(func() { _ = metricExp.unwrap().Shutdown(ctx) })
		})
	}
}

func TestCredentials(t *testing.T) {
	insecure := NewInsecureCredentials()
	if !insecure.Insecure {
		t.Error("NewInsecureCredentials: Insecure = false")
	}
	if insecure.Headers != nil {
		t.Errorf("NewInsecureCredentials: Headers = %v, want nil", insecure.Headers)
	}

	withHeaders := NewCredentials(map[string]string{"x-tenant": "acme"})
	if withHeaders.Insecure {
		t.Error("NewCredentials: Insecure = true, want false")
	}
	if got := withHeaders.Headers["x-tenant"]; got != "acme" {
		t.Errorf("NewCredentials: Headers[x-tenant] = %q, want acme", got)
	}
}
