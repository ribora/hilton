package otel

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	otelapi "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ribora/hilton"
)

type mapCarrier map[string]string

func (c mapCarrier) Get(key string) string { return c[key] }
func (c mapCarrier) Set(key, value string) { c[key] = value }

func (c mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// Not tracetest.InMemoryExporter: its Shutdown clears what it collected, and
// Shutdown is exactly what flushes the batcher.
type spanRecorder struct {
	mu sync.Mutex
	ss tracetest.SpanStubs
}

func (r *spanRecorder) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ss = append(r.ss, tracetest.SpanStubsFromReadOnlySpans(spans)...)
	return nil
}

func (r *spanRecorder) Shutdown(context.Context) error { return nil }

func (r *spanRecorder) spans() tracetest.SpanStubs {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ss
}

func newTraceProvider(t *testing.T, opts ...ProviderOption) (hilton.Provider, *spanRecorder) {
	t.Helper()
	rec := &spanRecorder{}
	p, err := NewProvider("svc", "test", &traceExporter{exp: rec}, nil, opts...)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	// Shutdown is idempotent, so tests that flush themselves are unaffected.
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return p, rec
}

// Batched: nothing reaches the recorder until the provider is shut down.
func flush(t *testing.T, p hilton.Provider, rec *spanRecorder) tracetest.SpanStubs {
	t.Helper()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	return rec.spans()
}

func attrMap(t *testing.T, stub tracetest.SpanStub) map[string]string {
	t.Helper()
	out := make(map[string]string, len(stub.Attributes))
	for _, kv := range stub.Attributes {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

func resourceMap(t *testing.T, stub tracetest.SpanStub) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, kv := range stub.Resource.Attributes() {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

func onlySpan(t *testing.T, stubs tracetest.SpanStubs) tracetest.SpanStub {
	t.Helper()
	if len(stubs) != 1 {
		t.Fatalf("exported %d spans, want 1", len(stubs))
	}
	return stubs[0]
}

// Has to fail at wiring time: a service that silently samples nothing looks
// healthy.
func TestWithSamplingRatioRejectsOutOfRange(t *testing.T) {
	for _, rate := range []float64{-1, -0.01, 1.01, 2} {
		if _, err := NewProvider("svc", "test", nil, nil, WithSamplingRatio(rate)); err == nil {
			t.Errorf("ratio %v accepted", rate)
		}
	}
	for _, rate := range []float64{0, 0.5, 1} {
		if _, err := NewProvider("svc", "test", nil, nil, WithSamplingRatio(rate)); err != nil {
			t.Errorf("ratio %v rejected: %v", rate, err)
		}
	}
}

// Without a handler the SDK reports a refused export to a global nobody
// configured, and the service looks healthy while exporting nothing.
func TestWithErrorHandler(t *testing.T) {
	// The handler is process-global, so put the previous one back.
	previous := otelapi.GetErrorHandler()
	t.Cleanup(func() { otelapi.SetErrorHandler(previous) })

	var mu sync.Mutex
	var got []error
	p, err := NewProvider("svc", "test", &traceExporter{exp: failingExporter{}}, nil,
		WithErrorHandler(func(err error) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, err)
		}),
	)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, span := p.Tracer("test").Start(context.Background(), "op")
	span.End()
	// Shutdown returns nil here: the batcher reports the refusal to the handler
	// and nowhere else, which is exactly why the option is the only way to see
	// it.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Logf("Shutdown also returned the failure: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("the export failure never reached the handler")
	}
	if !errors.Is(got[0], errExportRefused) {
		t.Errorf("handler got %v, want %v", got[0], errExportRefused)
	}
}

// Fails at start-up rather than installing something that panics on the first
// export error.
func TestWithErrorHandlerRejectsNil(t *testing.T) {
	if _, err := NewProvider("svc", "test", nil, nil, WithErrorHandler(nil)); err == nil {
		t.Error("NewProvider accepted a nil error handler")
	}
}

var errExportRefused = errors.New("export refused")

type failingExporter struct{}

func (failingExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errExportRefused
}

func (failingExporter) Shutdown(context.Context) error { return nil }

// These separate two deployments in the backend, so they must reach every span
// rather than stay on the provider.
func TestProviderResourceAttributes(t *testing.T) {
	rec := &spanRecorder{}
	p, err := NewProvider("checkout", "staging", &traceExporter{exp: rec}, nil,
		WithInstanceID("pod-7"),
		WithResource(hilton.Attr("team", "payments")),
	)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, span := p.Tracer("test").Start(context.Background(), "op")
	span.End()

	got := resourceMap(t, onlySpan(t, flush(t, p, rec)))
	for key, want := range map[string]string{
		"service.name":           "checkout",
		"deployment.environment": "staging",
		"service.instance.id":    "pod-7",
		"team":                   "payments",
	} {
		if got[key] != want {
			t.Errorf("resource[%s] = %q, want %q", key, got[key], want)
		}
	}
}

// An empty attribute would group every pod together.
func TestWithInstanceIDIgnoresEmpty(t *testing.T) {
	p, rec := newTraceProvider(t, WithInstanceID(""))

	_, span := p.Tracer("test").Start(context.Background(), "op")
	span.End()

	got := resourceMap(t, onlySpan(t, flush(t, p, rec)))
	if id, ok := got["service.instance.id"]; ok {
		t.Errorf("service.instance.id = %q, want absent", id)
	}
}

// How a service runs with telemetry off, so every method must still work.
func TestProviderWithoutExporters(t *testing.T) {
	p, err := NewProvider("svc", "test", nil, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	ctx, span := p.Tracer("test").Start(context.Background(), "op")
	span.SetAttributes(hilton.Attr("k", "v"))
	span.End()

	p.Meter("test").Counter("requests").Add(ctx, 1)

	carrier := mapCarrier{}
	p.Propagator().Inject(ctx, carrier)

	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// diogen closes containers through io.Closer and never calls Shutdown, so Close
// has to flush too.
func TestProviderCloseFlushes(t *testing.T) {
	rec := &spanRecorder{}
	p, err := NewProvider("svc", "test", &traceExporter{exp: rec}, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, span := p.Tracer("test").Start(context.Background(), "op")
	span.End()

	closer, ok := p.(io.Closer)
	if !ok {
		t.Fatalf("provider %T does not implement io.Closer", p)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := len(rec.spans()); got != 1 {
		t.Errorf("Close exported %d spans, want 1", got)
	}
}
