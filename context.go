package hilton

import "context"

type contextKey struct{}

func ContextWithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, contextKey{}, span)
}

func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(contextKey{}).(Span); ok && s != nil {
		return s
	}
	return noopSpan{}
}

type remoteKey struct{}

func ContextWithRemoteSpanContext(ctx context.Context, sc SpanContext) context.Context {
	return context.WithValue(ctx, remoteKey{}, sc)
}

func RemoteSpanContextFromContext(ctx context.Context) (SpanContext, bool) {
	sc, ok := ctx.Value(remoteKey{}).(SpanContext)
	return sc, ok && sc != nil && sc.IsValid()
}

type providerKey struct{}

func ContextWithProvider(ctx context.Context, p Provider) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, providerKey{}, p)
}

func ProviderFromContext(ctx context.Context) Provider {
	if p, ok := ctx.Value(providerKey{}).(Provider); ok && p != nil {
		return p
	}
	return Noop()
}
