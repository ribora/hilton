package hilton

import (
	"context"
	"reflect"
	"sync/atomic"
)

// For layers whose Provider arrives per call rather than at construction. One
// provider per process is the normal case, so a single entry beats a map.
type Instruments[T any] struct {
	build func(Provider) T
	cur   atomic.Pointer[instrumentEntry[T]]
}

func NewInstruments[T any](build func(Provider) T) *Instruments[T] {
	return &Instruments[T]{build: build}
}

// Unsynchronised on purpose: concurrent first callers may each build, and all
// of them get a usable value.
func (i *Instruments[T]) For(p Provider) T {
	if e := i.cur.Load(); e != nil && sameProvider(e.provider, p) {
		return e.value
	}
	value := i.build(p)
	i.cur.Store(&instrumentEntry[T]{provider: p, value: value})
	return value
}

func (i *Instruments[T]) FromContext(ctx context.Context) T {
	return i.For(ProviderFromContext(ctx))
}

type instrumentEntry[T any] struct {
	provider Provider
	value    T
}

// Not plain ==: an outside Provider may be a struct that == panics on.
func sameProvider(a, b Provider) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb || !ta.Comparable() {
		return false
	}
	return a == b
}
