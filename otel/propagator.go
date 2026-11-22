package otel

import (
	"context"

	"go.opentelemetry.io/otel/propagation"

	"github.com/ribora/hilton"
)

type propagator struct {
	p propagation.TextMapPropagator
}

func (p *propagator) Inject(ctx context.Context, carrier hilton.Carrier) {
	p.p.Inject(ctx, carrierBridge{carrier})
}

func (p *propagator) Extract(ctx context.Context, carrier hilton.Carrier) context.Context {
	return p.p.Extract(ctx, carrierBridge{carrier})
}

type carrierBridge struct{ c hilton.Carrier }

func (b carrierBridge) Get(key string) string { return b.c.Get(key) }
func (b carrierBridge) Set(key, value string) { b.c.Set(key, value) }
func (b carrierBridge) Keys() []string        { return b.c.Keys() }

var _ propagation.TextMapCarrier = carrierBridge{}
