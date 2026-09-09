package hilton

import (
	"context"
	"encoding/binary"
	"sync"
)

func Recorder() *Recording { return &Recording{} }

// Safe for concurrent use: the instruments are handed to middleware that runs
// per request, and a handler may fan out.
type Recording struct {
	mu     sync.Mutex
	issued uint64
	spans  []*RecordedSpan
	values []Measurement
	gauges []*gauge
}

type RecordedSpan struct {
	Instrumentation string
	Name            string
	Kind            SpanKind
	Attributes      []Attribute
	Links           []Link
	Errors          []error
	Status          StatusCode
	Description     string
	// Nil at the root of a trace.
	Parent SpanContext
	Ended  bool

	rec *Recording
	sc  spanContext
}

type InstrumentKind int

const (
	CounterInstrument InstrumentKind = iota
	UpDownCounterInstrument
	HistogramInstrument
	GaugeInstrument
)

// Value is float64 for both shapes: a counter adds int64 and a histogram
// records float64, and no test produces a count past float64's exact range.
type Measurement struct {
	Instrumentation string
	Instrument      string
	Kind            InstrumentKind
	Value           float64
	Attributes      []Attribute
}

func (r *Recording) Tracer(name string) Tracer { return tracer{rec: r, name: name} }
func (r *Recording) Meter(name string) Meter   { return meter{rec: r, name: name} }
func (r *Recording) Propagator() Propagator    { return propagator{} }

func (r *Recording) Shutdown(context.Context) error { return nil }

// Spans returns them in the order they were started, the unfinished included:
// a span that never ended is a defect a scenario should be able to name.
func (r *Recording) Spans() []*RecordedSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*RecordedSpan(nil), r.spans...)
}

// The last span with this name, because a retry starts a second one and the
// assertion is almost always about the outcome.
func (r *Recording) Span(name string) *RecordedSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.spans) - 1; i >= 0; i-- {
		if r.spans[i].Name == name {
			return r.spans[i]
		}
	}
	return nil
}

func (r *Recording) Measurements() []Measurement {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Measurement(nil), r.values...)
}

// Total sums what one instrument was given. A counter assertion is a sum, and
// writing it out at every call site is how a spy gets a count wrong.
func (r *Recording) Total(instrument string) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total float64
	for _, m := range r.values {
		if m.Instrument == instrument {
			total += m.Value
		}
	}
	return total
}

// Collect runs the registered gauge callbacks. A real provider owns collection
// timing; here the scenario does, because there is no interval to wait for.
func (r *Recording) Collect() {
	r.mu.Lock()
	registered := append([]*gauge(nil), r.gauges...)
	r.mu.Unlock()

	for _, g := range registered {
		g.observe(observer{rec: r, instrumentation: g.instrumentation, name: g.name})
	}
}

func (r *Recording) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans, r.values = nil, nil
}

// Attr answers what the span carries under a key, so an assertion does not walk
// the slice itself.
func (s *RecordedSpan) Attr(key string) (any, bool) {
	for _, a := range s.Attributes {
		if a.Key() == key {
			return a.Value(), true
		}
	}
	return nil, false
}

func (s *RecordedSpan) End() {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.Ended = true
}

func (s *RecordedSpan) SetName(name string) {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.Name = name
}

func (s *RecordedSpan) SetAttributes(attrs ...Attribute) {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.Attributes = append(s.Attributes, attrs...)
}

func (s *RecordedSpan) RecordError(err error) {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.Errors = append(s.Errors, err)
}

func (s *RecordedSpan) SetStatus(code StatusCode, description string) {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.Status, s.Description = code, description
}

func (s *RecordedSpan) SpanContext() SpanContext { return s.sc }

func (s *RecordedSpan) IsRecording() bool {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	return !s.Ended
}

type tracer struct {
	rec  *Recording
	name string
}

func (t tracer) Start(ctx context.Context, name string, opts ...SpanStartOption) (context.Context, Span) {
	var cfg SpanConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	span := &RecordedSpan{
		Instrumentation: t.name,
		Name:            name,
		Kind:            cfg.Kind,
		Attributes:      append([]Attribute(nil), cfg.Attrs...),
		Links:           append([]Link(nil), cfg.Links...),
		Parent:          parentOf(ctx),
		rec:             t.rec,
	}

	t.rec.mu.Lock()
	t.rec.issued++
	// The trace id is the parent's, so a scenario can tell one request's spans
	// from another's.
	span.sc = spanContext{traceID: traceOf(span.Parent, t.rec.issued), spanID: identifier8(t.rec.issued)}
	t.rec.spans = append(t.rec.spans, span)
	t.rec.mu.Unlock()

	return ContextWithSpan(ctx, span), span
}

// A local parent wins over a remote one: an extracted context is the caller's,
// and a span already open here is closer.
func parentOf(ctx context.Context) SpanContext {
	if sc := SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		return sc
	}
	if sc, ok := RemoteSpanContextFromContext(ctx); ok {
		return sc
	}
	return nil
}

func traceOf(parent SpanContext, issued uint64) [16]byte {
	if parent != nil && parent.IsValid() {
		return parent.TraceID()
	}
	return identifier16(issued)
}

func identifier16(n uint64) [16]byte {
	var id [16]byte
	binary.BigEndian.PutUint64(id[8:], n)
	return id
}

func identifier8(n uint64) [8]byte {
	var id [8]byte
	binary.BigEndian.PutUint64(id[:], n)
	return id
}

// Valid and sampled, so code that skips work on an unsampled span still runs.
type spanContext struct {
	traceID [16]byte
	spanID  [8]byte
}

func (s spanContext) TraceID() [16]byte { return s.traceID }
func (s spanContext) SpanID() [8]byte   { return s.spanID }
func (s spanContext) IsValid() bool     { return s.traceID != [16]byte{} && s.spanID != [8]byte{} }
func (s spanContext) IsSampled() bool   { return true }

type meter struct {
	rec  *Recording
	name string
}

func (m meter) Counter(name string, _ ...MeterOption) Counter {
	return instrument{rec: m.rec, instrumentation: m.name, name: name, kind: CounterInstrument}
}

func (m meter) UpDownCounter(name string, _ ...MeterOption) UpDownCounter {
	return instrument{rec: m.rec, instrumentation: m.name, name: name, kind: UpDownCounterInstrument}
}

func (m meter) Histogram(name string, _ ...MeterOption) Histogram {
	return instrument{rec: m.rec, instrumentation: m.name, name: name, kind: HistogramInstrument}
}

func (m meter) ObservableGauge(name string, observe func(Observer), _ ...MeterOption) (Registration, error) {
	g := &gauge{rec: m.rec, instrumentation: m.name, name: name, observe: observe}
	m.rec.mu.Lock()
	m.rec.gauges = append(m.rec.gauges, g)
	m.rec.mu.Unlock()
	return g, nil
}

type instrument struct {
	rec             *Recording
	instrumentation string
	name            string
	kind            InstrumentKind
}

func (i instrument) Add(_ context.Context, value int64, attrs ...Attribute) {
	i.rec.record(i.instrumentation, i.name, i.kind, float64(value), attrs)
}

func (i instrument) Record(_ context.Context, value float64, attrs ...Attribute) {
	i.rec.record(i.instrumentation, i.name, i.kind, value, attrs)
}

func (r *Recording) record(instrumentation, name string, kind InstrumentKind, value float64, attrs []Attribute) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, Measurement{
		Instrumentation: instrumentation,
		Instrument:      name,
		Kind:            kind,
		Value:           value,
		Attributes:      append([]Attribute(nil), attrs...),
	})
}

type gauge struct {
	rec             *Recording
	instrumentation string
	name            string
	observe         func(Observer)
}

func (g *gauge) Unregister() error {
	g.rec.mu.Lock()
	defer g.rec.mu.Unlock()
	for i, registered := range g.rec.gauges {
		if registered == g {
			g.rec.gauges = append(g.rec.gauges[:i], g.rec.gauges[i+1:]...)
			return nil
		}
	}
	return nil
}

type observer struct {
	rec             *Recording
	instrumentation string
	name            string
}

func (o observer) Observe(value int64, attrs ...Attribute) {
	o.rec.record(o.instrumentation, o.name, GaugeInstrument, float64(value), attrs)
}

// The real header format, through the package's own parser: a scenario that
// asserts on an outbound carrier is asserting on what the wire would carry.
type propagator struct{}

func (propagator) Inject(ctx context.Context, carrier Carrier) {
	sc := SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return
	}
	carrier.Set("traceparent", FormatTraceparent(sc))
}

func (propagator) Extract(ctx context.Context, carrier Carrier) context.Context {
	sc, ok := ParseTraceparent(carrier.Get("traceparent"))
	if !ok {
		return ctx
	}
	return ContextWithRemoteSpanContext(ctx, sc)
}
