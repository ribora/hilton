package hilton

import (
	"context"
	"sync"
	"testing"
)

func TestInstrumentsBuildsOncePerProvider(t *testing.T) {
	var built int
	ins := NewInstruments(func(p Provider) Counter {
		built++
		return p.Meter("test").Counter("c")
	})

	p := Noop()
	first := ins.For(p)
	second := ins.For(p)

	if built != 1 {
		t.Errorf("built %d times for one provider, want 1", built)
	}
	if first != second {
		t.Error("second call returned a different instrument")
	}
}

// Serving one provider's instruments to another would make a test that swaps in
// its own recorder silently measure nothing.
func TestInstrumentsRebuildsForAnotherProvider(t *testing.T) {
	var seen []Provider
	ins := NewInstruments(func(p Provider) Provider {
		seen = append(seen, p)
		return p
	})

	first, second := Noop(), &countingProvider{}
	if got := ins.For(first); got != first {
		t.Error("For returned another provider's value")
	}
	if got := ins.For(second); got != second {
		t.Error("second provider got the first one's value")
	}
	if len(seen) != 2 {
		t.Errorf("built %d times for two providers, want 2", len(seen))
	}
}

// == panics on such a provider, so the cache has to degrade to rebuilding
// rather than crash the request.
func TestInstrumentsToleratesUncomparableProvider(t *testing.T) {
	var built int
	ins := NewInstruments(func(Provider) int {
		built++
		return built
	})

	p := uncomparableProvider{tags: map[string]string{"k": "v"}}
	ins.For(p)
	ins.For(p)

	if built != 2 {
		t.Errorf("built %d times, want 2 — an uncomparable provider cannot be cached", built)
	}
}

func TestInstrumentsFromContext(t *testing.T) {
	ins := NewInstruments(func(p Provider) Provider { return p })

	if got := ins.FromContext(context.Background()); got == nil {
		t.Fatal("FromContext returned nil for an empty context")
	}
	if _, ok := ins.FromContext(context.Background()).(noopProvider); !ok {
		t.Error("FromContext did not fall back to the noop provider")
	}

	want := Noop()
	if got := ins.FromContext(ContextWithProvider(context.Background(), want)); got != want {
		t.Errorf("FromContext = %v, want the stored provider", got)
	}
}

// For is unsynchronised, so concurrent first calls must still all come back
// usable.
func TestInstrumentsConcurrent(t *testing.T) {
	ins := NewInstruments(func(p Provider) Counter { return p.Meter("test").Counter("c") })
	p := Noop()

	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			if c := ins.For(p); c == nil {
				t.Error("For returned nil")
			}
		})
	}
	wg.Wait()
}

func TestSameProviderNil(t *testing.T) {
	if !sameProvider(nil, nil) {
		t.Error("sameProvider(nil, nil) = false")
	}
	if sameProvider(nil, Noop()) || sameProvider(Noop(), nil) {
		t.Error("sameProvider matched nil against a provider")
	}
}

type countingProvider struct{ noopProvider }

type uncomparableProvider struct {
	noopProvider
	tags map[string]string
}
