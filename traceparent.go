package hilton

import (
	"encoding/hex"
	"fmt"
)

// Version 00 is fixed at exactly this by the spec, so length is a valid check.
const traceparentLen = 55

// FormatTraceparent encodes a SpanContext as a W3C traceparent header value.
// Returns empty string for invalid (zero) span contexts.
func FormatTraceparent(sc SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	tid := sc.TraceID()
	sid := sc.SpanID()
	flags := byte(0)
	if sc.IsSampled() {
		flags = 1
	}
	return fmt.Sprintf("00-%s-%s-%02x",
		hex.EncodeToString(tid[:]),
		hex.EncodeToString(sid[:]),
		flags,
	)
}

// ParseTraceparent parses a W3C traceparent header into a remote SpanContext.
// Only version 00 is accepted; (nil, false) on anything malformed.
func ParseTraceparent(tp string) (SpanContext, bool) {
	if len(tp) != traceparentLen {
		return nil, false
	}
	if tp[0] != '0' || tp[1] != '0' || tp[2] != '-' || tp[35] != '-' || tp[52] != '-' {
		return nil, false
	}
	var tid [16]byte
	var sid [8]byte
	if _, err := hex.Decode(tid[:], []byte(tp[3:35])); err != nil {
		return nil, false
	}
	if _, err := hex.Decode(sid[:], []byte(tp[36:52])); err != nil {
		return nil, false
	}
	var fb [1]byte
	if _, err := hex.Decode(fb[:], []byte(tp[53:55])); err != nil {
		return nil, false
	}
	sc := remoteSpanContext{traceID: tid, spanID: sid, sampled: fb[0]&1 == 1}
	if !sc.IsValid() {
		return nil, false
	}
	return sc, true
}

type remoteSpanContext struct {
	traceID [16]byte
	spanID  [8]byte
	sampled bool
}

func (r remoteSpanContext) TraceID() [16]byte { return r.traceID }
func (r remoteSpanContext) SpanID() [8]byte   { return r.spanID }
func (r remoteSpanContext) IsSampled() bool   { return r.sampled }

// Either id all-zero is invalid per W3C: a parent with no span is not a parent.
func (r remoteSpanContext) IsValid() bool {
	return r.traceID != [16]byte{} && r.spanID != [8]byte{}
}
