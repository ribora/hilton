package hilton

import (
	"encoding/hex"
	"strings"
	"testing"
)

const (
	validTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	wantTraceID      = "4bf92f3577b34da6a3ce929d0e0e4736"
	wantSpanID       = "00f067aa0ba902b7"
)

// Hand-indexed parsing: one shifted offset reparents a trace onto a stranger.
func TestTraceparentRoundTrip(t *testing.T) {
	for _, tp := range []string{
		validTraceparent,
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00",
	} {
		sc, ok := ParseTraceparent(tp)
		if !ok {
			t.Fatalf("ParseTraceparent(%q) rejected a valid header", tp)
		}
		if got := FormatTraceparent(sc); got != tp {
			t.Errorf("round trip = %q, want %q", got, tp)
		}
	}
}

func TestParseTraceparentFields(t *testing.T) {
	sc, ok := ParseTraceparent(validTraceparent)
	if !ok {
		t.Fatalf("ParseTraceparent(%q) rejected a valid header", validTraceparent)
	}

	tid := sc.TraceID()
	if got := hex.EncodeToString(tid[:]); got != wantTraceID {
		t.Errorf("TraceID = %s, want %s", got, wantTraceID)
	}
	sid := sc.SpanID()
	if got := hex.EncodeToString(sid[:]); got != wantSpanID {
		t.Errorf("SpanID = %s, want %s", got, wantSpanID)
	}
	if !sc.IsSampled() {
		t.Error("IsSampled = false, want true for flags 01")
	}
	if !sc.IsValid() {
		t.Error("IsValid = false for a well-formed header")
	}
}

// Only the low bit of the flags byte is the sampling decision; the rest belong
// to future versions and must not make a header unparseable.
func TestParseTraceparentFlagBits(t *testing.T) {
	for _, tc := range []struct {
		flags   string
		sampled bool
	}{
		{"00", false},
		{"01", true},
		{"02", false},
		{"03", true},
		{"ff", true},
		{"fe", false},
	} {
		tp := validTraceparent[:53] + tc.flags
		sc, ok := ParseTraceparent(tp)
		if !ok {
			t.Fatalf("ParseTraceparent(%q) rejected flags %s", tp, tc.flags)
		}
		if sc.IsSampled() != tc.sampled {
			t.Errorf("flags %s: IsSampled = %v, want %v", tc.flags, sc.IsSampled(), tc.sampled)
		}
	}
}

func TestParseTraceparentRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		tp   string
	}{
		{"empty", ""},
		{"truncated", validTraceparent[:54]},
		{"trailing garbage", validTraceparent + "0"},
		{"trailing dash", validTraceparent + "-"},
		{"unsupported version", "01" + validTraceparent[2:]},
		{"version ff", "ff" + validTraceparent[2:]},
		{"first separator", replaceAt(validTraceparent, 2, 'x')},
		{"second separator", replaceAt(validTraceparent, 35, 'x')},
		{"third separator", replaceAt(validTraceparent, 52, 'x')},
		{"non-hex trace id", replaceAt(validTraceparent, 4, 'z')},
		{"non-hex span id", replaceAt(validTraceparent, 40, 'z')},
		{"non-hex flags", replaceAt(validTraceparent, 53, 'z')},
		{"all-zero trace id", "00-" + strings.Repeat("0", 32) + "-" + wantSpanID + "-01"},
		{"all-zero span id", "00-" + wantTraceID + "-" + strings.Repeat("0", 16) + "-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc, ok := ParseTraceparent(tc.tp)
			if ok {
				t.Errorf("ParseTraceparent(%q) accepted it", tc.tp)
			}
			if sc != nil {
				t.Errorf("ParseTraceparent(%q) returned %v alongside ok=false", tc.tp, sc)
			}
		})
	}
}

// Emitting an all-zero header would hand the next service a parent that cannot
// exist.
func TestFormatTraceparentSkipsInvalid(t *testing.T) {
	for name, sc := range map[string]SpanContext{
		"noop":        noopSpanContext{},
		"zero":        remoteSpanContext{},
		"no span id":  remoteSpanContext{traceID: [16]byte{1}},
		"no trace id": remoteSpanContext{spanID: [8]byte{1}},
	} {
		if got := FormatTraceparent(sc); got != "" {
			t.Errorf("%s: FormatTraceparent = %q, want empty", name, got)
		}
	}
}

func replaceAt(s string, i int, c byte) string {
	b := []byte(s)
	b[i] = c
	return string(b)
}
