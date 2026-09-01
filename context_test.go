package hilton

import (
	"context"
	"testing"
)

// Callers invoke Span methods unguarded, so nil is never an acceptable answer.
func TestSpanFromContextFallsBackToNoop(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"empty":       context.Background(),
		"stored nil":  ContextWithSpan(context.Background(), nil),
		"wrong value": context.WithValue(context.Background(), contextKey{}, "not a span"),
	} {
		got := SpanFromContext(ctx)
		if got == nil {
			t.Fatalf("%s: SpanFromContext returned nil", name)
		}
		if _, ok := got.(noopSpan); !ok {
			t.Errorf("%s: SpanFromContext = %T, want noopSpan", name, got)
		}
		got.SetAttributes(Attr("k", "v"))
		got.End()
	}
}

func TestSpanFromContextRoundTrip(t *testing.T) {
	want := &fakeSpan{}
	ctx := ContextWithSpan(context.Background(), want)
	if got := SpanFromContext(ctx); got != want {
		t.Errorf("SpanFromContext = %v, want %v", got, want)
	}
}

func TestProviderFromContextFallsBackToNoop(t *testing.T) {
	got := ProviderFromContext(context.Background())
	if _, ok := got.(noopProvider); !ok {
		t.Errorf("ProviderFromContext = %T, want noopProvider", got)
	}
}

func TestProviderFromContextRoundTrip(t *testing.T) {
	want := Noop()
	ctx := ContextWithProvider(context.Background(), want)
	if got := ProviderFromContext(ctx); got != want {
		t.Errorf("ProviderFromContext = %v, want %v", got, want)
	}
}

// The fallback already covers "not configured"; storing nil adds nothing.
func TestContextWithProviderIgnoresNil(t *testing.T) {
	parent := context.Background()
	if got := ContextWithProvider(parent, nil); got != parent {
		t.Error("ContextWithProvider(ctx, nil) wrapped the context")
	}
}

// Gated on IsValid, not presence: otherwise every unparented request joins one
// bogus all-zero trace.
func TestRemoteSpanContextFromContext(t *testing.T) {
	valid, ok := ParseTraceparent(validTraceparent)
	if !ok {
		t.Fatal("test fixture is not a valid traceparent")
	}

	for _, tc := range []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"absent", context.Background(), false},
		{"valid", ContextWithRemoteSpanContext(context.Background(), valid), true},
		{"zero", ContextWithRemoteSpanContext(context.Background(), remoteSpanContext{}), false},
		{"noop", ContextWithRemoteSpanContext(context.Background(), noopSpanContext{}), false},
		{"nil", ContextWithRemoteSpanContext(context.Background(), nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RemoteSpanContextFromContext(tc.ctx)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if ok && got.TraceID() != valid.TraceID() {
				t.Errorf("TraceID = %x, want %x", got.TraceID(), valid.TraceID())
			}
		})
	}
}
