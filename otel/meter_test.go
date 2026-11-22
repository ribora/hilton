package otel

import (
	"context"
	"slices"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/ribora/hilton"
)

// Holds only the last collection: the SDK reuses the ResourceMetrics it passes
// in, and the periodic reader collects once, on Shutdown.
type recordingExporter struct {
	mu sync.Mutex
	rm metricdata.ResourceMetrics
}

func (e *recordingExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(k)
}

func (e *recordingExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}

func (e *recordingExporter) Export(_ context.Context, rm *metricdata.ResourceMetrics) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rm = *rm
	return nil
}

func (e *recordingExporter) ForceFlush(context.Context) error { return nil }
func (e *recordingExporter) Shutdown(context.Context) error   { return nil }

func (e *recordingExporter) metric(t *testing.T, name string) metricdata.Metrics {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, scope := range e.rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("metric %q was not exported", name)
	return metricdata.Metrics{}
}

func histogramPoint(t *testing.T, exp *recordingExporter, name string) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	m := exp.metric(t, name)
	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("metric %q: Data = %T, want Histogram[float64]", name, m.Data)
	}
	if len(hist.DataPoints) != 1 {
		t.Fatalf("metric %q: got %d data points, want 1", name, len(hist.DataPoints))
	}
	return hist.DataPoints[0]
}

func occupiedBuckets(counts []uint64) int {
	var n int
	for _, c := range counts {
		if c > 0 {
			n++
		}
	}
	return n
}

func newMeterProvider(t *testing.T) (hilton.Provider, *recordingExporter) {
	t.Helper()
	exp := &recordingExporter{}
	p, err := NewProvider("svc", "test", nil, &metricExporter{exp: exp})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p, exp
}

// Description and unit are only carried on the instrument, so a dropped
// MeterOption is invisible until someone reads the metric in a dashboard.
func TestMeterCounter(t *testing.T) {
	p, exp := newMeterProvider(t)
	ctx := context.Background()

	c := p.Meter("test").Counter("requests",
		hilton.WithDescription("requests served"),
		hilton.WithUnit("1"),
	)
	c.Add(ctx, 2, hilton.Attr("route", "/health"))
	c.Add(ctx, 3, hilton.Attr("route", "/health"))

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	m := exp.metric(t, "requests")
	if m.Description != "requests served" {
		t.Errorf("Description = %q, want %q", m.Description, "requests served")
	}
	if m.Unit != "1" {
		t.Errorf("Unit = %q, want 1", m.Unit)
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("Data = %T, want Sum[int64]", m.Data)
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("got %d data points, want 1 — the attribute set should have collapsed them", len(sum.DataPoints))
	}
	dp := sum.DataPoints[0]
	if dp.Value != 5 {
		t.Errorf("Value = %d, want 5", dp.Value)
	}
	if got, ok := dp.Attributes.Value("route"); !ok || got.String() != "/health" {
		t.Errorf("route = %v, want /health", got)
	}
}

func TestMeterHistogram(t *testing.T) {
	p, exp := newMeterProvider(t)
	ctx := context.Background()

	h := p.Meter("test").Histogram("latency",
		hilton.WithDescription("request latency"),
		hilton.WithUnit("s"),
	)
	h.Record(ctx, 0.25, hilton.Attr("route", "/health"))
	h.Record(ctx, 0.75, hilton.Attr("route", "/health"))

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	m := exp.metric(t, "latency")
	if m.Unit != "s" {
		t.Errorf("Unit = %q, want s", m.Unit)
	}

	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("Data = %T, want Histogram[float64]", m.Data)
	}
	if len(hist.DataPoints) != 1 {
		t.Fatalf("got %d data points, want 1", len(hist.DataPoints))
	}
	dp := hist.DataPoints[0]
	if dp.Count != 2 {
		t.Errorf("Count = %d, want 2", dp.Count)
	}
	if dp.Sum != 1 {
		t.Errorf("Sum = %v, want 1", dp.Sum)
	}
}

// Latency is in seconds against a millisecond-scaled default, so without
// boundaries every realistic request lands in one bucket and no percentile
// survives.
func TestMeterHistogramBoundaries(t *testing.T) {
	latencies := []float64{0.002, 0.008, 0.031, 0.12, 0.44, 0.98, 2.5, 7}

	t.Run("sdk default collapses", func(t *testing.T) {
		p, exp := newMeterProvider(t)
		ctx := context.Background()

		h := p.Meter("test").Histogram("d", hilton.WithUnit("s"))
		for _, v := range latencies {
			h.Record(ctx, v)
		}
		if err := p.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}

		dp := histogramPoint(t, exp, "d")
		if got := occupiedBuckets(dp.BucketCounts); got > 2 {
			t.Skipf("SDK default is no longer millisecond-scaled: %d buckets occupied", got)
		}
	})

	t.Run("seconds boundaries spread", func(t *testing.T) {
		p, exp := newMeterProvider(t)
		ctx := context.Background()

		h := p.Meter("test").Histogram("d", hilton.WithUnit("s"), hilton.WithSecondsBoundaries())
		for _, v := range latencies {
			h.Record(ctx, v)
		}
		if err := p.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}

		dp := histogramPoint(t, exp, "d")
		if got := occupiedBuckets(dp.BucketCounts); got < 6 {
			t.Errorf("only %d of %d buckets occupied (counts %v), want the samples spread",
				got, len(dp.BucketCounts), dp.BucketCounts)
		}
	})
}

// Explicit boundaries must reach the instrument verbatim.
func TestMeterHistogramExplicitBoundaries(t *testing.T) {
	p, exp := newMeterProvider(t)
	ctx := context.Background()

	want := []float64{0.25, 0.5, 1}
	h := p.Meter("test").Histogram("d", hilton.WithBoundaries(want...))
	h.Record(ctx, 0.4)
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if got := histogramPoint(t, exp, "d").Bounds; !slices.Equal(got, want) {
		t.Errorf("Bounds = %v, want %v", got, want)
	}
}

// Must map to a non-monotonic sum: a Counter would swallow the decrement.
func TestMeterUpDownCounter(t *testing.T) {
	p, exp := newMeterProvider(t)
	ctx := context.Background()

	c := p.Meter("test").UpDownCounter("active_requests",
		hilton.WithDescription("in-flight requests"),
		hilton.WithUnit("1"),
	)
	c.Add(ctx, 1, hilton.Attr("route", "/health"))
	c.Add(ctx, 1, hilton.Attr("route", "/health"))
	c.Add(ctx, -1, hilton.Attr("route", "/health"))

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	m := exp.metric(t, "active_requests")
	if m.Description != "in-flight requests" {
		t.Errorf("Description = %q, want %q", m.Description, "in-flight requests")
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("Data = %T, want Sum[int64]", m.Data)
	}
	if sum.IsMonotonic {
		t.Error("IsMonotonic = true, want false for an UpDownCounter")
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("got %d data points, want 1", len(sum.DataPoints))
	}
	if got := sum.DataPoints[0].Value; got != 1 {
		t.Errorf("Value = %d, want 1", got)
	}
}

// The callback must run at collection, not at registration — otherwise the
// gauge reports whatever the state was when it was wired.
func TestMeterObservableGauge(t *testing.T) {
	p, exp := newMeterProvider(t)
	ctx := context.Background()

	var observed int
	reg, err := p.Meter("test").ObservableGauge("pool.connections",
		func(o hilton.Observer) {
			observed++
			o.Observe(7, hilton.Attr("pool", "main"))
		},
		hilton.WithDescription("open connections"),
		hilton.WithUnit("1"),
	)
	if err != nil {
		t.Fatalf("ObservableGauge: %v", err)
	}
	if observed != 0 {
		t.Errorf("callback ran %d times at registration, want 0", observed)
	}

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if observed == 0 {
		t.Fatal("callback never ran during collection")
	}

	m := exp.metric(t, "pool.connections")
	if m.Unit != "1" {
		t.Errorf("Unit = %q, want 1", m.Unit)
	}
	gauge, ok := m.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("Data = %T, want Gauge[int64]", m.Data)
	}
	if len(gauge.DataPoints) != 1 {
		t.Fatalf("got %d data points, want 1", len(gauge.DataPoints))
	}
	dp := gauge.DataPoints[0]
	if dp.Value != 7 {
		t.Errorf("Value = %d, want 7", dp.Value)
	}
	if got, ok := dp.Attributes.Value("pool"); !ok || got.String() != "main" {
		t.Errorf("pool = %v, want main", got)
	}
	if err := reg.Unregister(); err != nil {
		t.Errorf("Unregister: %v", err)
	}
}

// The callback must stop being consulted after Unregister, or it reads a pool
// the container has already closed.
func TestObservableGaugeUnregister(t *testing.T) {
	p, exp := newMeterProvider(t)
	ctx := context.Background()

	var observed int
	reg, err := p.Meter("test").ObservableGauge("pool.connections", func(o hilton.Observer) {
		observed++
		o.Observe(7)
	})
	if err != nil {
		t.Fatalf("ObservableGauge: %v", err)
	}
	if err := reg.Unregister(); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if observed != 0 {
		t.Errorf("callback ran %d times after Unregister, want 0", observed)
	}
	for _, scope := range exp.rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "pool.connections" {
				t.Error("an unregistered gauge was still exported")
			}
		}
	}
}

// Instruments outlive the request, so a cancelled context must still record;
// baggage is the one thing worth carrying across.
func TestSafeBaggageCtx(t *testing.T) {
	member, err := baggage.NewMember("tenant", "acme")
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	b, err := baggage.New(member)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(baggage.ContextWithBaggage(context.Background(), b))
	cancel()

	safe := safeBaggageCtx(ctx)
	if err := safe.Err(); err != nil {
		t.Errorf("safe context carries cancellation: %v", err)
	}
	if got := baggage.FromContext(safe).Member("tenant").Value(); got != "acme" {
		t.Errorf("baggage tenant = %q, want acme", got)
	}
}

func TestSafeBaggageCtxWithoutBaggage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	safe := safeBaggageCtx(ctx)
	if err := safe.Err(); err != nil {
		t.Errorf("safe context carries cancellation: %v", err)
	}
	if got := baggage.FromContext(safe).Len(); got != 0 {
		t.Errorf("baggage has %d members, want 0", got)
	}
}

// Same thing through the public API, since safeBaggageCtx is unexported.
func TestMeterRecordsOnCancelledContext(t *testing.T) {
	p, exp := newMeterProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	p.Meter("test").Counter("requests").Add(ctx, 1)
	cancel()
	p.Meter("test").Counter("requests").Add(ctx, 1)

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	sum, ok := exp.metric(t, "requests").Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("Data = %T, want Sum[int64]", exp.metric(t, "requests").Data)
	}
	if len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 2 {
		t.Errorf("data points = %v, want one point of value 2", sum.DataPoints)
	}
}
