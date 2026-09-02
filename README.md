# hilton

Telemetry as an interface. Tracing and metrics reduced to a handful of Go
interfaces, a noop implementation that is always safe to call, and an
OpenTelemetry adapter in a separate package.

Instrumentation code — middleware, interceptors, a SQL wrapper — imports
`hilton` and nothing else. Only the service's wiring imports `hilton/otel`,
and only that import pulls in the OTel SDK, its exporters and gRPC.

```go
import "gitlab.com/gorib/hilton"

func NewTracing(provider hilton.Provider) Middleware {
    tracer := provider.Tracer("gitlab.com/gorib/http/server")
    ...
}
```

## Install

```bash
go get gitlab.com/gorib/hilton
```

## Quick start

Build a provider once, at start-up, and pass it to whatever needs
instrumenting:

```go
import (
    "gitlab.com/gorib/hilton"
    "gitlab.com/gorib/hilton/otel"
)

traceExp, err := otel.NewGrpcTraceExporter(cfg.OTLPEndpoint, otel.NewInsecureCredentials())
if err != nil {
    return err
}
metricExp, err := otel.NewGrpcMetricExporter(cfg.OTLPEndpoint, otel.NewInsecureCredentials())
if err != nil {
    return err
}

provider, err := otel.NewProvider("checkout", cfg.Env, traceExp, metricExp,
    otel.WithInstanceID(hostname),
    otel.WithSamplingRatio(0.1),
)
if err != nil {
    return err
}
defer provider.Shutdown(ctx)
```

An empty endpoint yields a `nil` exporter and no error, so the same wiring runs
with telemetry off — no branch at the call site. Nothing at all configured is
`hilton.Noop()`, which is also what `hilton.ProviderFromContext` falls back to.

Then instrument:

```go
tracer := provider.Tracer("myapp/orders")
counter := provider.Meter("myapp/orders").Counter("orders.placed",
    hilton.WithDescription("Orders accepted"),
    hilton.WithUnit("1"),
)

ctx, span := tracer.Start(ctx, "PlaceOrder",
    hilton.WithSpanKind(hilton.SpanKindServer),
    hilton.WithAttributes(hilton.Attr("order.channel", "web")),
)
defer span.End()

if err := place(ctx); err != nil {
    span.RecordError(err)
    span.SetStatus(hilton.StatusError, err.Error())
    return err
}
counter.Add(ctx, 1, hilton.Attr("order.channel", "web"))
```

## Why a facade at all

`go.opentelemetry.io/otel` already publishes interfaces, and using them
directly is the default answer. Two things pushed this one out:

**The dependency reaches everywhere.** Every package in gorib that records
anything — `http/server`, `grpc`, `amqp`, `sql`, `redis`, `elastic` — would
otherwise carry the OTel API in its own `go.mod`, and every one of those
versions has to agree with whatever SDK the service ends up with. Here exactly
one module names OTel, upgrades happen in one place, and a binary that never
imports `hilton/otel` does not link the SDK, its exporters or gRPC.

**otel's surface is much wider than what instrumentation needs.** `Int64Counter`
and `Float64Histogram` and eight more instrument kinds, each with observable
variants, sync and async; `attribute.KeyValue` constructors per type;
`TextMapPropagator` with its own carrier interface. What the interceptors in
this repository actually use is: start a span, set attributes, record an error,
add to a counter, record a duration, inject and extract. That is the whole
interface here, and it fits on one screen — `hilton.go` is about 100 lines.

The cost is real and worth naming:

- **A narrower interface is a narrower ceiling.** Gauges are int64 and
  observable only; there are no span events beyond what `RecordError` emits, no
  baggage API and no `ForceFlush`. Reaching past the facade means adding to it.
- **Ten attribute types, and no more.** Anything else is a compile error at the
  call site, which is the intent, but it does mean a `uint64` needs a cast.
- **One more layer to read** when a span comes out wrong.

If a service does not need to share instrumentation with the rest of gorib,
using the OTel API directly is less machinery.

## The interfaces

| Type | Purpose |
|---|---|
| `Provider` | root: hands out tracers and meters, owns the propagator and shutdown |
| `Tracer` | `Start(ctx, name, ...SpanStartOption) (context.Context, Span)` |
| `Span` | `End`, `SetName`, `SetAttributes`, `RecordError`, `SetStatus`, `SpanContext`, `IsRecording` |
| `SpanContext` | `TraceID`, `SpanID`, `IsValid`, `IsSampled` — ids as `[16]byte` / `[8]byte` |
| `Meter` | `Counter`, `UpDownCounter`, `Histogram`, `ObservableGauge` |
| `Counter` | `Add(ctx, int64, ...Attribute)` — monotonic |
| `UpDownCounter` | `Add(ctx, int64, ...Attribute)` — may decrease |
| `Histogram` | `Record(ctx, float64, ...Attribute)` |
| `Observer` | `Observe(int64, ...Attribute)` — inside a gauge callback |
| `Registration` | `Unregister() error` — undoes an `ObservableGauge` |
| `Propagator` | `Inject(ctx, Carrier)`, `Extract(ctx, Carrier) context.Context` |
| `Carrier` | `Get`, `Set`, `Keys` — headers, gRPC metadata, AMQP tables |

Span kinds are `SpanKindInternal` (zero value), `Server`, `Client`, `Producer`,
`Consumer`. Statuses are `StatusUnset`, `StatusOK`, `StatusError`.

Options: `WithSpanKind`, `WithAttributes` and `WithLinks` (both accumulate
across calls), `WithDescription`, `WithUnit`, `WithBoundaries`,
`WithSecondsBoundaries`.

### Histogram buckets

The SDK default bucket set is `[0, 5, 10, 25, … 10000]`, scaled for
milliseconds. Latency in gorib is recorded in **seconds**, so without explicit
boundaries every request under five seconds lands in one bucket and no
percentile can be recovered from it. Every duration histogram therefore says so:

```go
meter.Histogram("http.server.request.duration",
    hilton.WithUnit("s"),
    hilton.WithSecondsBoundaries(),
)
```

`WithSecondsBoundaries` is the OTel semconv set for seconds. Use
`WithBoundaries` directly when the scale is different — CLI commands run for
minutes, not milliseconds.

### Gauges

A gauge reports state the caller does not push, so it takes a callback the
provider invokes at collection time:

```go
reg, err := meter.ObservableGauge("db.client.connection.count",
    func(o hilton.Observer) {
        stats := pool.Stats()
        o.Observe(int64(stats.Idle), hilton.Attr("db.client.connection.state", "idle"))
        o.Observe(int64(stats.InUse), hilton.Attr("db.client.connection.state", "used"))
    },
    hilton.WithUnit("1"),
)
```

Both values come from one snapshot, so they cannot disagree. The callback is
held by the provider for as long as it is registered — `reg.Unregister()` on
shutdown, or it reads a closed pool. Wrapping the registration in an
`io.Closer` lets diogen unregister it with the rest of the container.

For a value the caller does push and can decrement — in-flight requests, open
connections it counts itself — use `UpDownCounter`, and decrement in a `defer`
so the count unwinds on a panic.

### Attributes

`Attr` is the only constructor — `Attribute`'s fields are unexported, so a value
of a type the adapter cannot map is a compile error rather than an attribute
that silently disappears. The constraint is that list of types:

```go
bool | []bool | int | []int | int64 | []int64 | float64 | []float64 | string | []string
```

Keep them low-cardinality. A matched route pattern (`/users/{id}`) belongs on a
span and in a metric; the id itself belongs on the span only.

### Links

A span has one parent but can have many causes. A batch consumer is the case
that needs it: the parent comes from one message, and the rest arrive as links,
each with the sampling decision its own traceparent carried.

```go
ctx, span := tracer.Start(ctx, "amqp process",
    hilton.WithSpanKind(hilton.SpanKindConsumer),
    hilton.WithLinks(hilton.Link{
        SpanContext: parsed,
        Attributes:  []hilton.Attribute{hilton.Attr("messaging.batch.index", i)},
    }),
)
```

A link with a nil or invalid `SpanContext` is dropped rather than recorded —
it would point the UI at a trace that does not exist.

## Context

```go
ctx = hilton.ContextWithProvider(ctx, provider)   // nil provider is a no-op
provider := hilton.ProviderFromContext(ctx)       // Noop() when absent

span := hilton.SpanFromContext(ctx)               // noop span when absent, never nil
```

Neither lookup returns nil, so a code path that was never instrumented — or a
service with telemetry switched off — needs no guard.

`Tracer.Start` puts its span in the context it returns, so nested spans chain
by passing `ctx` along.

## Propagation

`Propagator` moves trace context across a boundary through a `Carrier`, which
is whatever the transport uses for key-value metadata:

```go
ctx := prop.Extract(r.Context(), httpHeaderCarrier(r.Header))
ctx, span := tracer.Start(ctx, r.Method, hilton.WithSpanKind(hilton.SpanKindServer))
...
prop.Inject(ctx, httpHeaderCarrier(outbound.Header))
```

The otel adapter's propagator is composite: W3C trace context and baggage.

### When there is no carrier

Some transports hand you a traceparent as a bare string — read off a session,
a job payload, a database row — with nothing to build a `Carrier` from. For
those:

```go
if parent, ok := hilton.ParseTraceparent(s.Traceparent); ok {
    ctx = hilton.ContextWithRemoteSpanContext(ctx, parent)
}
ctx, span := tracer.Start(ctx, "process")  // adopts the incoming trace
```

`Start` honours the parent's sampling decision, so a caller that decided not to
sample does not get sampled downstream.

The other direction, used across gorib to carry the current trace into a
session that a non-HTTP transport will forward:

```go
if tp := hilton.FormatTraceparent(span.SpanContext()); tp != "" {
    s.Traceparent = tp
}
```

`FormatTraceparent` returns `""` for an unset span context, and
`ParseTraceparent` accepts version `00` only — 55 characters exactly, no
all-zero ids — so a malformed header is `ok == false` rather than a trace
rooted on a parent that cannot exist.

## The otel adapter

```go
func NewProvider(service, environment string, traceExp TraceExporter, metricExp MetricExporter, options ...ProviderOption) (hilton.Provider, error)
```

`service` and `environment` become `service.name` and
`deployment.environment` on every span and metric.

| Option | Effect |
|---|---|
| `WithInstanceID(id)` | `service.instance.id`; empty id is a no-op |
| `WithSamplingRatio(r)` | `ParentBased(TraceIDRatioBased(r))` — honours upstream, ratio applies to roots only; errors outside `[0, 1]` |
| `WithResource(attrs...)` | extra resource attributes |
| `WithErrorHandler(fn)` | route SDK failures somewhere visible; otherwise a refused exporter is silent |

Exporters come in gRPC and HTTP flavours for traces and metrics:

```go
otel.NewGrpcTraceExporter(endpoint, creds)
otel.NewHttpTraceExporter(endpoint, creds)
otel.NewGrpcMetricExporter(endpoint, creds)
otel.NewHttpMetricExporter(endpoint, creds)
```

`creds` is either `NewInsecureCredentials()` or `NewCredentials(headers)`. An
empty endpoint returns `(nil, nil)`, and `NewProvider` accepts nil exporters.
Traces are batched; metrics go through a periodic reader.

### Shutdown

Spans sit in a batcher until they are flushed, so a process that exits without
shutting the provider down loses its last spans.

```go
defer provider.Shutdown(ctx)
```

The concrete provider also implements `io.Closer`, which is the seam
`gitlab.com/gorib/di` collects: a provider built inside a diogen container is
closed by the generated `Shutdown` without anyone calling it by hand.

## Instrumenting

Conventions the gorib packages follow, worth copying:

- **Instrumentation name is the package path** — `provider.Tracer("gitlab.com/gorib/http/server")`.
  It lands on the span as the scope and answers "who recorded this".
- **Kind describes the boundary**, not the code: `Server` for an inbound
  request, `Client` for an outbound one, `Producer`/`Consumer` for a queue,
  `Internal` for everything else.
- **Record the error and set the status.** `RecordError` adds an exception
  event; it does not mark the span failed. Both, or the span reads as
  successful in the UI.
- **Name spans by the low-cardinality route**, setting it late if the router
  only matches after the span opens — `SetName` exists for exactly that.
- **Watch the middleware order.** Anything that maps an error to a status code
  has to run *inside* the tracing layer, or the metric records the status the
  tracing layer saw rather than the one the client got.

Counters and histograms are created once, when the layer is built, not per
call — `Meter.Counter` is not a lookup. A layer that only sees its provider in
the context, like a database hook or a client interceptor, has nowhere to build
them; `Instruments` is that seam:

```go
var instruments = hilton.NewInstruments(func(p hilton.Provider) state {
    return state{
        tracer:   p.Tracer(instrumentationName),
        duration: p.Meter(instrumentationName).Histogram("db.client.operation.duration",
            hilton.WithUnit("s"), hilton.WithSecondsBoundaries()),
    }
})

func query(ctx context.Context) {
    s := instruments.FromContext(ctx)
    ...
}
```

Building per call costs about 490ns and 16 allocations against 200ns and 6
through the cache, and an `ObservableGauge` cannot be registered per call at
all. One provider per process is the normal case, so the cache holds a single
entry and the fast path is one atomic load.

The meter deliberately strips the caller's context down to baggage before
recording: instruments outlive the request, so a cancelled request context must
not follow a value into the SDK.

## Testing

Code that takes a `hilton.Provider` needs nothing from this package to be
testable — `hilton.Noop()` satisfies it and records nothing.

To assert on what was actually recorded, wire the otel adapter to an in-memory
exporter and flush it. Note that spans are batched, so nothing arrives before
`Shutdown`, and `tracetest.InMemoryExporter` is unusable here because its
`Shutdown` clears what it collected — `otel/provider_test.go` has a small
recorder that does not.

```bash
make test     # go test ./...
make audit    # golangci-lint run
```

## Layout

```
hilton.go        the interfaces, options, Attr
context.go       provider / span / remote-parent context helpers
instruments.go   per-provider instrument cache for context-fed layers
noop.go          the always-safe implementation
traceparent.go   W3C traceparent encode and parse
otel/            the OpenTelemetry adapter — the only package importing the SDK
```
