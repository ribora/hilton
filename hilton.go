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

type Attribute struct {
	Key   string
	Value any
}

func StringAttr(key, value string) Attribute      { return Attribute{key, value} }
func IntAttr(key string, value int) Attribute     { return Attribute{key, value} }
func Int64Attr(key string, value int64) Attribute { return Attribute{key, value} }
func BoolAttr(key string, value bool) Attribute   { return Attribute{key, value} }

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
	SetAttributes(attrs ...Attribute)
	RecordError(err error)
	SetStatus(code StatusCode, description string)
	SpanContext() SpanContext
	IsRecording() bool
}

type SpanStartOption func(*SpanConfig)

type SpanConfig struct {
	Kind  SpanKind
	Attrs []Attribute
}

func WithSpanKind(k SpanKind) SpanStartOption {
	return func(c *SpanConfig) { c.Kind = k }
}

func WithAttributes(attrs ...Attribute) SpanStartOption {
	return func(c *SpanConfig) { c.Attrs = append(c.Attrs, attrs...) }
}

type Tracer interface {
	Start(ctx context.Context, name string, opts ...SpanStartOption) (context.Context, Span)
}

type MeterOption func(*MeterConfig)

type MeterConfig struct {
	Description string
	Unit        string
}

func WithDescription(d string) MeterOption { return func(c *MeterConfig) { c.Description = d } }
func WithUnit(u string) MeterOption        { return func(c *MeterConfig) { c.Unit = u } }

type Counter interface {
	Add(ctx context.Context, value int64, attrs ...Attribute)
}

type Histogram interface {
	Record(ctx context.Context, value float64, attrs ...Attribute)
}

type Meter interface {
	Counter(name string, opts ...MeterOption) Counter
	Histogram(name string, opts ...MeterOption) Histogram
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
