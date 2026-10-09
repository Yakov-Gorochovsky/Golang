// Package auth provides ECDSA signature verification for V2X telemetry
// payloads using a concurrent worker-pool authenticator.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// Payload carries a single V2X telemetry broadcast together with its
// detached ECDSA signature. Both slices are intentionally untyped []byte so
// that the authenticator remains agnostic to any higher-level framing format.
type Payload struct {
	// Data is the raw, serialized telemetry bytes that were signed.
	Data []byte

	// Signature is the DER-encoded ECDSA signature over Data.
	Signature []byte
}

// Authenticator owns the concurrent verification pipeline for incoming
// Payload messages. Fields are exported for Phase 1 test access; encapsulation
// will be tightened in a later refactoring pass as noted in the plan.
//
// Memory-layout note: SpoofedDropped is placed first so that it is guaranteed
// to be 64-bit aligned on all platforms — a requirement for atomic operations
// on 32-bit architectures (https://pkg.go.dev/sync/atomic#pkg-note-BUG).
type Authenticator struct {
	// SpoofedDropped counts payloads whose signatures failed verification.
	// Must only be accessed through sync/atomic to avoid data races.
	SpoofedDropped int64

	// PubKey is the ECDSA public key used to verify every incoming payload.
	PubKey *ecdsa.PublicKey

	// NumWorkers is the number of concurrent goroutines in the pipeline.
	NumWorkers int

	// Ctx is the parent context; cancellation triggers graceful shutdown.
	Ctx context.Context

	// Wg tracks in-flight worker goroutines for safe shutdown sequencing.
	Wg sync.WaitGroup

	// Input receives unverified payloads from the ingestion layer.
	Input chan Payload

	// Output forwards verified payloads to downstream consumers.
	Output chan Payload
}

// verify checks whether p.Signature is a valid ASN.1-DER ECDSA signature
// over the SHA-256 hash of p.Data using the Authenticator's public key.
//
// It returns false — never panics — for any of the following:
//   - nil or malformed signature bytes (ecdsa.VerifyASN1 handles this safely)
//   - a valid signature produced by a different private key
//   - data that was modified after signing
func (a *Authenticator) verify(p Payload) bool {
	// Add defense-in-depth length checks to prevent CPU DoS and GC pressure.
	if len(p.Data) == 0 || len(p.Data) > 1024 {
		return false
	}
	if len(p.Signature) == 0 || len(p.Signature) > 80 {
		return false
	}

	digest := sha256.Sum256(p.Data)
	return ecdsa.VerifyASN1(a.PubKey, digest[:], p.Signature)
}

// NewAuthenticator constructs a ready-to-use Authenticator. It initialises
// the Output channel so that Start can begin forwarding verified payloads
// immediately without a separate setup step.
func NewAuthenticator(ctx context.Context, numWorkers int, pubKey *ecdsa.PublicKey) *Authenticator {
	if pubKey == nil || pubKey.Curve != elliptic.P256() {
		panic("auth: public key must use the P-256 curve to match SHA-256")
	}

	return &Authenticator{
		Ctx:        ctx,
		NumWorkers: numWorkers,
		PubKey:     pubKey,
		Output:     make(chan Payload),
	}
}

// Start launches exactly NumWorkers goroutines that range over input,
// calling verify on each payload. Valid payloads are forwarded to Output;
// invalid ones increment SpoofedDropped atomically.
//
// A separate background coordinator calls Wg.Wait() once all workers have
// returned, then closes Output — this signals downstream consumers (and the
// test's for-range loop) that the pipeline is fully drained.
//
// The returned channel is the same as a.Output, exposed as a receive-only
// type so callers cannot accidentally send into it.
func (a *Authenticator) Start(input <-chan Payload) <-chan Payload {
	for range a.NumWorkers {
		a.Wg.Add(1)
		go func() {
			defer a.Wg.Done()
			for {
				select {
				case p, ok := <-input:
					if !ok {
						return
					}
					if a.verify(p) {
						select {
						case a.Output <- p:
						case <-a.Ctx.Done():
							return
						}
					} else {
						atomic.AddInt64(&a.SpoofedDropped, 1)
					}
				case <-a.Ctx.Done():
					return
				}
			}
		}()
	}

	// Coordinator: waits for all workers to exit then closes the output channel.
	// This must be a separate goroutine — it must not block Start's caller.
	go func() {
		a.Wg.Wait()
		close(a.Output)
	}()

	return a.Output
}
