package hilton

import "context"

type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

// Fields are unexported so Attr is the only way in: a literal could carry a type
// the adapter does not map, and an unmapped attribute is dropped silently.
type Attribute struct {
	key   string
	value any
}

func Attr[T bool | []bool | int | []int | int64 | []int64 | float64 | []float64 | string | []string](key string, value T) Attribute {
	return Attribute{key, value}
}

func (a Attribute) Key() string { return a.key }
func (a Attribute) Value() any  { return a.value }

// Carrier is a key-value store for trace context propagation
// (HTTP headers, gRPC metadata, AMQP message headers, etc.).
type Carrier interface {
	Get(key string) string
	Set(key, value string)
	Keys() []string
}

type SpanContext interface {
	TraceID() [16]byte
	SpanID() [8]byte
	IsValid() bool
	IsSampled() bool
}

type Span interface {
	End()
	SetName(name string)
	SetAttributes(attrs ...Attribute)
	RecordError(err error)
	SetStatus(code StatusCode, description string)
	SpanContext() SpanContext
	IsRecording() bool
}

// Link records a span this one is caused by without being its child — a batch
// consumer has many causes and can only have one parent.
type Link struct {
	SpanContext SpanContext
	Attributes  []Attribute
}

type SpanStartOption func(*SpanConfig)

type SpanConfig struct {
	Kind  SpanKind
	Attrs []Attribute
	Links []Link
}

func WithSpanKind(k SpanKind) SpanStartOption {
	return func(c *SpanConfig) { c.Kind = k }
}

func WithAttributes(attrs ...Attribute) SpanStartOption {
	return func(c *SpanConfig) { c.Attrs = append(c.Attrs, attrs...) }
}

func WithLinks(links ...Link) SpanStartOption {
	return func(c *SpanConfig) { c.Links = append(c.Links, links...) }
}

type Tracer interface {
	Start(ctx context.Context, name string, opts ...SpanStartOption) (context.Context, Span)
}

type MeterOption func(*MeterConfig)

type MeterConfig struct {
	Description string
	Unit        string
	Boundaries  []float64
}

func WithDescription(d string) MeterOption { return func(c *MeterConfig) { c.Description = d } }
func WithUnit(u string) MeterOption        { return func(c *MeterConfig) { c.Unit = u } }

// Without boundaries the SDK default applies, and it is millisecond-scaled:
// every sub-second observation lands in one bucket.
func WithBoundaries(b ...float64) MeterOption {
	return func(c *MeterConfig) { c.Boundaries = b }
}

// The semconv set for seconds.
func WithSecondsBoundaries() MeterOption {
	return WithBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10)
}

type Counter interface {
	Add(ctx context.Context, value int64, attrs ...Attribute)
}

type UpDownCounter interface {
	Add(ctx context.Context, value int64, attrs ...Attribute)
}

type Histogram interface {
	Record(ctx context.Context, value float64, attrs ...Attribute)
}

// Only valid inside a gauge callback — the provider owns collection timing.
type Observer interface {
	Observe(value int64, attrs ...Attribute)
}

// The provider holds the callback, so an instrument that outlives what it reads
// has to be unregistered.
type Registration interface {
	Unregister() error
}

type Meter interface {
	Counter(name string, opts ...MeterOption) Counter
	UpDownCounter(name string, opts ...MeterOption) UpDownCounter
	Histogram(name string, opts ...MeterOption) Histogram
	ObservableGauge(name string, observe func(Observer), opts ...MeterOption) (Registration, error)
}

type Propagator interface {
	Inject(ctx context.Context, carrier Carrier)
	Extract(ctx context.Context, carrier Carrier) context.Context
}

type Provider interface {
	Tracer(instrumentationName string) Tracer
	Meter(instrumentationName string) Meter
	Propagator() Propagator
	Shutdown(ctx context.Context) error
}
