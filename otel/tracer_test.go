package otel

import (
	"context"
	"errors"
	"testing"

	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"gitlab.com/gorib/hilton"
)

const (
	sampledTraceparent   = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	unsampledTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
)

// The one test that walks all the way out to the exporter.
func TestTracerRecordsSpan(t *testing.T) {
	p, rec := newTraceProvider(t)

	ctx, span := p.Tracer("test").Start(context.Background(), "handle",
		hilton.WithSpanKind(hilton.SpanKindServer),
		hilton.WithAttributes(hilton.Attr("http.request.method", "GET")),
	)
	if got := hilton.SpanFromContext(ctx); got != span {
		t.Errorf("SpanFromContext = %v, want the started span", got)
	}
	if !span.IsRecording() {
		t.Error("IsRecording = false for a sampled span")
	}
	span.SetAttributes(hilton.Attr("http.response.status_code", 500))
	span.RecordError(errors.New("boom"))
	span.SetStatus(hilton.StatusError, "boom")
	span.End()

	stub := onlySpan(t, flush(t, p, rec))

	if stub.Name != "handle" {
		t.Errorf("Name = %q, want handle", stub.Name)
	}
	if stub.SpanKind != oteltrace.SpanKindServer {
		t.Errorf("SpanKind = %v, want %v", stub.SpanKind, oteltrace.SpanKindServer)
	}
	if stub.InstrumentationScope.Name != "test" {
		t.Errorf("scope = %q, want test", stub.InstrumentationScope.Name)
	}
	attrs := attrMap(t, stub)
	if attrs["http.request.method"] != "GET" {
		t.Errorf("http.request.method = %q, want GET", attrs["http.request.method"])
	}
	if attrs["http.response.status_code"] != "500" {
		t.Errorf("http.response.status_code = %q, want 500", attrs["http.response.status_code"])
	}
	if stub.Status.Code != otelcodes.Error || stub.Status.Description != "boom" {
		t.Errorf("Status = %v/%q, want Error/boom", stub.Status.Code, stub.Status.Description)
	}
	if len(stub.Events) != 1 || stub.Events[0].Name != "exception" {
		t.Errorf("Events = %v, want one exception event", stub.Events)
	}
}

// How the gRPC and AMQP layers reach a transport with no carrier, so a live
// span has to survive Format and Parse both ways.
func TestSpanContextRoundTripsThroughTraceparent(t *testing.T) {
	p, _ := newTraceProvider(t)

	_, span := p.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	sc := span.SpanContext()
	if !sc.IsValid() {
		t.Fatal("IsValid = false for a live span")
	}
	if !sc.IsSampled() {
		t.Error("IsSampled = false under the default sampler")
	}

	tp := hilton.FormatTraceparent(sc)
	if tp == "" {
		t.Fatal("FormatTraceparent returned empty for a live span")
	}
	parsed, ok := hilton.ParseTraceparent(tp)
	if !ok {
		t.Fatalf("ParseTraceparent(%q) rejected our own output", tp)
	}
	if parsed.TraceID() != sc.TraceID() {
		t.Errorf("TraceID = %x, want %x", parsed.TraceID(), sc.TraceID())
	}
	if parsed.SpanID() != sc.SpanID() {
		t.Errorf("SpanID = %x, want %x", parsed.SpanID(), sc.SpanID())
	}
	if parsed.IsSampled() != sc.IsSampled() {
		t.Errorf("IsSampled = %v, want %v", parsed.IsSampled(), sc.IsSampled())
	}
}

// A span the sampler dropped has a valid context but no traceparent worth
// forwarding as sampled — the flag has to come out as 00.
func TestUnsampledSpanContext(t *testing.T) {
	p, _ := newTraceProvider(t, WithSamplingRatio(0))

	_, span := p.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	sc := span.SpanContext()
	if sc.IsSampled() {
		t.Error("IsSampled = true under a zero sampling ratio")
	}
	if tp := hilton.FormatTraceparent(sc); tp != "" && tp[53:] != "00" {
		t.Errorf("traceparent flags = %s, want 00", tp[53:])
	}
}

// A batch consumer has many causes and one parent, so the rest have to arrive
// as links or their traces lose the connection entirely.
func TestSpanLinks(t *testing.T) {
	p, rec := newTraceProvider(t)

	first, ok := hilton.ParseTraceparent(sampledTraceparent)
	if !ok {
		t.Fatal("test fixture is not a valid traceparent")
	}
	second, ok := hilton.ParseTraceparent("00-8bf92f3577b34da6a3ce929d0e0e4736-11f067aa0ba902b7-00")
	if !ok {
		t.Fatal("second fixture is not a valid traceparent")
	}

	_, span := p.Tracer("test").Start(context.Background(), "batch",
		hilton.WithSpanKind(hilton.SpanKindConsumer),
		hilton.WithLinks(
			hilton.Link{SpanContext: first, Attributes: []hilton.Attribute{hilton.Attr("index", 0)}},
			hilton.Link{SpanContext: second},
		),
	)
	span.End()

	stub := onlySpan(t, flush(t, p, rec))
	if len(stub.Links) != 2 {
		t.Fatalf("recorded %d links, want 2", len(stub.Links))
	}

	if got := stub.Links[0].SpanContext.TraceID(); got != oteltrace.TraceID(first.TraceID()) {
		t.Errorf("link 0 trace = %s, want %x", got, first.TraceID())
	}
	if !stub.Links[0].SpanContext.IsRemote() {
		t.Error("link 0 is not marked remote")
	}
	if !stub.Links[0].SpanContext.IsSampled() {
		t.Error("link 0 lost the sampled flag")
	}
	if len(stub.Links[0].Attributes) != 1 {
		t.Errorf("link 0 attributes = %v, want one", stub.Links[0].Attributes)
	}

	if got := stub.Links[1].SpanContext.SpanID(); got != oteltrace.SpanID(second.SpanID()) {
		t.Errorf("link 1 span = %s, want %x", got, second.SpanID())
	}
	// The unsampled flag has to survive too, or a link claims its target was
	// recorded when it was not.
	if stub.Links[1].SpanContext.IsSampled() {
		t.Error("link 1 gained a sampled flag it did not have")
	}
}

// An invalid link would point the UI at a trace that does not exist.
func TestSpanLinksSkipInvalid(t *testing.T) {
	p, rec := newTraceProvider(t)

	valid, ok := hilton.ParseTraceparent(sampledTraceparent)
	if !ok {
		t.Fatal("test fixture is not a valid traceparent")
	}

	_, span := p.Tracer("test").Start(context.Background(), "batch",
		hilton.WithLinks(
			hilton.Link{},
			hilton.Link{SpanContext: valid},
		),
	)
	span.End()

	stub := onlySpan(t, flush(t, p, rec))
	if len(stub.Links) != 1 {
		t.Fatalf("recorded %d links, want only the valid one", len(stub.Links))
	}
}

// An HTTP server opens the span before the router has matched the route.
func TestSpanSetName(t *testing.T) {
	p, rec := newTraceProvider(t)

	_, span := p.Tracer("test").Start(context.Background(), "HTTP GET")
	span.SetName("GET /users/{id}")
	span.End()

	if got := onlySpan(t, flush(t, p, rec)).Name; got != "GET /users/{id}" {
		t.Errorf("Name = %q, want GET /users/{id}", got)
	}
}

func TestToOtelKind(t *testing.T) {
	for _, tc := range []struct {
		in   hilton.SpanKind
		want oteltrace.SpanKind
	}{
		{hilton.SpanKindInternal, oteltrace.SpanKindInternal},
		{hilton.SpanKindServer, oteltrace.SpanKindServer},
		{hilton.SpanKindClient, oteltrace.SpanKindClient},
		{hilton.SpanKindProducer, oteltrace.SpanKindProducer},
		{hilton.SpanKindConsumer, oteltrace.SpanKindConsumer},
		{hilton.SpanKind(99), oteltrace.SpanKindInternal},
	} {
		if got := toOtelKind(tc.in); got != tc.want {
			t.Errorf("toOtelKind(%d) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestToOtelStatus(t *testing.T) {
	for _, tc := range []struct {
		in   hilton.StatusCode
		want otelcodes.Code
	}{
		{hilton.StatusUnset, otelcodes.Unset},
		{hilton.StatusOK, otelcodes.Ok},
		{hilton.StatusError, otelcodes.Error},
		{hilton.StatusCode(99), otelcodes.Unset},
	} {
		if got := toOtelStatus(tc.in); got != tc.want {
			t.Errorf("toOtelStatus(%d) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Must be marked remote, or a ParentBased sampler cannot tell it from a local
// parent.
func TestRemoteSpanContextBecomesParent(t *testing.T) {
	p, rec := newTraceProvider(t)

	parent, ok := hilton.ParseTraceparent(sampledTraceparent)
	if !ok {
		t.Fatal("test fixture is not a valid traceparent")
	}
	ctx := hilton.ContextWithRemoteSpanContext(context.Background(), parent)

	_, span := p.Tracer("test").Start(ctx, "child")
	span.End()

	stub := onlySpan(t, flush(t, p, rec))

	if stub.SpanContext.TraceID() != oteltrace.TraceID(parent.TraceID()) {
		t.Errorf("TraceID = %s, want %x", stub.SpanContext.TraceID(), parent.TraceID())
	}
	if stub.Parent.SpanID() != oteltrace.SpanID(parent.SpanID()) {
		t.Errorf("Parent.SpanID = %s, want %x", stub.Parent.SpanID(), parent.SpanID())
	}
	if !stub.Parent.IsRemote() {
		t.Error("Parent.IsRemote = false, want true")
	}
}

// Regression: forcing FlagsSampled made a ParentBased sampler record every
// trace an unsampled caller sent.
func TestRemoteParentSamplingDecisionIsHonoured(t *testing.T) {
	for _, tc := range []struct {
		name string
		tp   string
		want int
	}{
		{"sampled parent", sampledTraceparent, 1},
		{"unsampled parent", unsampledTraceparent, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, rec := newTraceProvider(t, WithSamplingRatio(0))

			parent, ok := hilton.ParseTraceparent(tc.tp)
			if !ok {
				t.Fatalf("%q is not a valid traceparent", tc.tp)
			}
			ctx := hilton.ContextWithRemoteSpanContext(context.Background(), parent)

			_, span := p.Tracer("test").Start(ctx, "child")
			span.End()

			if got := len(flush(t, p, rec)); got != tc.want {
				t.Errorf("exported %d spans, want %d", got, tc.want)
			}
		})
	}
}

// Without a remote parent the ratio applies to the root, which is the other half
// of the ParentBased contract.
func TestRootSpanObeysSamplingRatio(t *testing.T) {
	p, rec := newTraceProvider(t, WithSamplingRatio(0))

	_, span := p.Tracer("test").Start(context.Background(), "root")
	if span.IsRecording() {
		t.Error("IsRecording = true under a zero sampling ratio")
	}
	span.End()

	if got := len(flush(t, p, rec)); got != 0 {
		t.Errorf("exported %d spans, want 0", got)
	}
}

// The local half of the same contract — no traceparent involved.
func TestLocalParentChains(t *testing.T) {
	p, rec := newTraceProvider(t)

	tr := p.Tracer("test")
	ctx, parent := tr.Start(context.Background(), "parent")
	_, child := tr.Start(ctx, "child")
	child.End()
	parent.End()

	stubs := flush(t, p, rec)
	if len(stubs) != 2 {
		t.Fatalf("exported %d spans, want 2", len(stubs))
	}
	byName := map[string]int{}
	for i, stub := range stubs {
		byName[stub.Name] = i
	}
	c, pa := stubs[byName["child"]], stubs[byName["parent"]]
	if c.Parent.SpanID() != pa.SpanContext.SpanID() {
		t.Errorf("child parent = %s, want %s", c.Parent.SpanID(), pa.SpanContext.SpanID())
	}
	if c.Parent.IsRemote() {
		t.Error("a local parent is marked remote")
	}
	if c.SpanContext.TraceID() != pa.SpanContext.TraceID() {
		t.Errorf("child trace = %s, want %s", c.SpanContext.TraceID(), pa.SpanContext.TraceID())
	}
}
