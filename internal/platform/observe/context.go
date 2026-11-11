// Package observe provides bounded, process-local telemetry and local diagnostics.
package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type requestIDKey struct{}

// WithRequestID creates a new server identity; incoming headers are never used.
func WithRequestID(ctx context.Context) context.Context {
	var b [16]byte
	// crypto/rand.Read either fills the buffer or terminates the process on failure.
	_, _ = rand.Read(b[:])
	return context.WithValue(ctx, requestIDKey{}, hex.EncodeToString(b[:]))
}
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }
