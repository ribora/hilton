package hilton

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpanKeepsWhatTheMiddlewareSetOnIt(t *testing.T) {
	rec := Recorder()

	ctx, span := rec.Tracer("grpc").Start(t.Context(), "Decide", WithSpanKind(SpanKindServer))
	span.SetAttributes(Attr("rpc.method", "Decide"))
	span.SetStatus(StatusError, "not found")
	span.RecordError(errors.New("no rule"))
	span.End()
	_ = ctx

	recorded := rec.Span("Decide")
	require.NotNil(t, recorded)
	assert.Equal(t, "grpc", recorded.Instrumentation)
	assert.Equal(t, SpanKindServer, recorded.Kind)
	assert.Equal(t, StatusError, recorded.Status)
	assert.Equal(t, "not found", recorded.Description)
	assert.Len(t, recorded.Errors, 1)
	assert.True(t, recorded.Ended)

	method, ok := recorded.Attr("rpc.method")
	require.True(t, ok)
	assert.Equal(t, "Decide", method)
}

func TestAnUnfinishedSpanIsStillReported(t *testing.T) {
	rec := Recorder()

	_, span := rec.Tracer("grpc").Start(t.Context(), "Decide")

	assert.True(t, span.IsRecording())
	require.Len(t, rec.Spans(), 1)
	assert.False(t, rec.Spans()[0].Ended)
}

func TestSpanNameAnswersWithTheLastOne(t *testing.T) {
	rec := Recorder()
	tracer := rec.Tracer("grpc")

	_, first := tracer.Start(t.Context(), "Decide")
	first.SetStatus(StatusOK, "")
	first.End()
	_, second := tracer.Start(t.Context(), "Decide")
	second.SetStatus(StatusError, "retry failed")
	second.End()

	assert.Equal(t, StatusError, rec.Span("Decide").Status)
}

func TestSpanWithNoSuchNameIsNil(t *testing.T) {
	assert.Nil(t, Recorder().Span("nothing started this"))
}

func TestAChildKeepsTheTraceAndNamesItsParent(t *testing.T) {
	rec := Recorder()
	tracer := rec.Tracer("grpc")

	ctx, parent := tracer.Start(t.Context(), "Decide")
	_, child := tracer.Start(ctx, "repository.Get")

	assert.Equal(t, parent.SpanContext().TraceID(), child.SpanContext().TraceID())
	assert.NotEqual(t, parent.SpanContext().SpanID(), child.SpanContext().SpanID())
	require.NotNil(t, rec.Span("repository.Get").Parent)
	assert.Equal(t, parent.SpanContext().SpanID(), rec.Span("repository.Get").Parent.SpanID())
}

func TestARootSpanHasNoParent(t *testing.T) {
	rec := Recorder()

	_, span := rec.Tracer("grpc").Start(t.Context(), "Decide")

	assert.Nil(t, rec.Span("Decide").Parent)
	assert.True(t, span.SpanContext().IsValid())
}

// The extracted context is the caller's, and a span opened here is closer.
func TestAnExtractedContextBecomesTheParentWhenNothingIsOpenLocally(t *testing.T) {
	rec := Recorder()
	carrier := headers{}

	ctx, span := rec.Tracer("client").Start(t.Context(), "Call")
	rec.Propagator().Inject(ctx, carrier)
	span.End()

	remote := rec.Propagator().Extract(context.Background(), carrier)
	_, server := rec.Tracer("server").Start(remote, "Serve")

	assert.Equal(t, span.SpanContext().TraceID(), server.SpanContext().TraceID())
	assert.Equal(t, span.SpanContext().SpanID(), rec.Span("Serve").Parent.SpanID())
}

func TestInjectWritesNothingWithoutASpan(t *testing.T) {
	carrier := headers{}

	Recorder().Propagator().Inject(context.Background(), carrier)

	assert.Empty(t, carrier.Keys())
}

func TestCountersAndHistogramsAreKeptWithTheirAttributes(t *testing.T) {
	rec := Recorder()
	meter := rec.Meter("grpc")

	meter.Counter("rpc.server.calls").Add(t.Context(), 1, Attr("rpc.code", "OK"))
	meter.Counter("rpc.server.calls").Add(t.Context(), 2, Attr("rpc.code", "OK"))
	meter.Histogram("rpc.server.duration").Record(t.Context(), 0.25)

	assert.Equal(t, float64(3), rec.Total("rpc.server.calls"))

	measurements := rec.Measurements()
	require.Len(t, measurements, 3)
	assert.Equal(t, CounterInstrument, measurements[0].Kind)
	assert.Equal(t, "rpc.code", measurements[0].Attributes[0].Key())
	assert.Equal(t, HistogramInstrument, measurements[2].Kind)
	assert.InDelta(t, 0.25, measurements[2].Value, 1e-9)
}

func TestAnUpDownCounterGoesBothWays(t *testing.T) {
	rec := Recorder()

	pool := rec.Meter("sql").UpDownCounter("db.connections")
	pool.Add(t.Context(), 3)
	pool.Add(t.Context(), -2)

	assert.Equal(t, float64(1), rec.Total("db.connections"))
}

func TestTotalOfAnInstrumentNobodyTouchedIsZero(t *testing.T) {
	assert.Zero(t, Recorder().Total("db.connections"))
}

// A real provider owns collection timing; here the scenario asks.
func TestAGaugeIsReadWhenTheScenarioCollects(t *testing.T) {
	rec := Recorder()
	_, err := rec.Meter("sql").ObservableGauge("db.idle", func(o Observer) {
		o.Observe(7, Attr("pool", "main"))
	})
	require.NoError(t, err)

	assert.Empty(t, rec.Measurements(), "nothing is observed until Collect")

	rec.Collect()

	require.Len(t, rec.Measurements(), 1)
	assert.Equal(t, GaugeInstrument, rec.Measurements()[0].Kind)
	assert.Equal(t, float64(7), rec.Measurements()[0].Value)
}

func TestAnUnregisteredGaugeIsNotCollected(t *testing.T) {
	rec := Recorder()
	registration, err := rec.Meter("sql").ObservableGauge("db.idle", func(o Observer) { o.Observe(7) })
	require.NoError(t, err)

	require.NoError(t, registration.Unregister())
	rec.Collect()

	assert.Empty(t, rec.Measurements())
}

func TestResetEmptiesWhatWasRecorded(t *testing.T) {
	rec := Recorder()
	_, span := rec.Tracer("grpc").Start(t.Context(), "Decide")
	span.End()
	rec.Meter("grpc").Counter("rpc.server.calls").Add(t.Context(), 1)

	rec.Reset()

	assert.Empty(t, rec.Spans())
	assert.Empty(t, rec.Measurements())
}

func TestTheRecorderSatisfiesProvider(t *testing.T) {
	var p Provider = Recorder()

	require.NoError(t, p.Shutdown(t.Context()))
	assert.NotNil(t, p.Tracer("x"))
	assert.NotNil(t, p.Meter("x"))
	assert.NotNil(t, p.Propagator())
}

type headers map[string]string

func (h headers) Get(key string) string { return h[key] }
func (h headers) Set(key, value string) { h[key] = value }

func (h headers) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	return keys
}
