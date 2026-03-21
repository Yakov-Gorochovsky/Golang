package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/auth"
)

// ---------------------------------------------------------------------------
// Payload struct tests
// ---------------------------------------------------------------------------

func TestPayload_FieldTypes(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		signature []byte
	}{
		{
			name:      "zero-value payload has nil slices",
			data:      nil,
			signature: nil,
		},
		{
			name:      "payload with non-empty data and nil signature",
			data:      []byte("telemetry-broadcast"),
			signature: nil,
		},
		{
			name:      "payload with nil data and non-empty signature",
			data:      nil,
			signature: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		},
		{
			name:      "payload with both fields populated",
			data:      []byte("v2x-collision-data"),
			signature: []byte{0x01, 0x02, 0x03},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := auth.Payload{
				Data:      tc.data,
				Signature: tc.signature,
			}

			if len(p.Data) != len(tc.data) {
				t.Errorf("Data length: got %d, want %d", len(p.Data), len(tc.data))
			}
			if len(p.Signature) != len(tc.signature) {
				t.Errorf("Signature length: got %d, want %d", len(p.Signature), len(tc.signature))
			}
		})
	}
}

func TestPayload_ZeroValue(t *testing.T) {
	var p auth.Payload

	if p.Data != nil {
		t.Errorf("expected Data to be nil on zero-value Payload, got %v", p.Data)
	}
	if p.Signature != nil {
		t.Errorf("expected Signature to be nil on zero-value Payload, got %v", p.Signature)
	}
}

// ---------------------------------------------------------------------------
// Authenticator struct zero-initialization (field existence) tests
// ---------------------------------------------------------------------------

// generateTestKey is a helper that creates a fresh ECDSA key pair for tests
// that require a *ecdsa.PublicKey. It does NOT test crypto logic — that belongs
// to Phase 2.
func generateTestKey(t *testing.T) *ecdsa.PublicKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}
	return &priv.PublicKey
}

func TestAuthenticator_ZeroValue(t *testing.T) {
	// A zero-value Authenticator must be safely addressable and its exported
	// fields must reflect their respective zero values. This test verifies the
	// struct shape required by Phase 1 without invoking any constructor logic.
	var a auth.Authenticator

	// SpoofedDropped must be an atomic int64 (or uint64); zero value is 0.
	if got := atomic.LoadInt64(&a.SpoofedDropped); got != 0 {
		t.Errorf("SpoofedDropped: got %d, want 0", got)
	}
}

func TestAuthenticator_StructFields(t *testing.T) {
	pubKey := generateTestKey(t)

	tests := []struct {
		name       string
		numWorkers int
		pubKey     *ecdsa.PublicKey
	}{
		{
			name:       "single worker configuration",
			numWorkers: 1,
			pubKey:     pubKey,
		},
		{
			name:       "multi-worker configuration",
			numWorkers: 8,
			pubKey:     pubKey,
		},
		{
			name:       "zero workers configuration",
			numWorkers: 0,
			pubKey:     pubKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Directly instantiate the struct to verify field assignability.
			// Constructor (NewAuthenticator) is a Phase 3 concern.
			a := auth.Authenticator{
				PubKey:     tc.pubKey,
				NumWorkers: tc.numWorkers,
				Ctx:        ctx,
			}

			if a.PubKey != tc.pubKey {
				t.Errorf("PubKey: got %v, want %v", a.PubKey, tc.pubKey)
			}
			if a.NumWorkers != tc.numWorkers {
				t.Errorf("NumWorkers: got %d, want %d", a.NumWorkers, tc.numWorkers)
			}
			if a.Ctx == nil {
				t.Error("Ctx must not be nil after assignment")
			}
			// Wg must be addressable (proven by TestStart_ConcurrentPipeline).
			// We verify it is usable without copying the lock.
			a.Wg.Add(0) // no-op; confirms the field is accessible and not nil-guarded

			// SpoofedDropped must be addressable as an int64 for atomic ops.
			atomic.AddInt64(&a.SpoofedDropped, 1)
			if got := atomic.LoadInt64(&a.SpoofedDropped); got != 1 {
				t.Errorf("SpoofedDropped after atomic add: got %d, want 1", got)
			}
		})
	}
}

func TestAuthenticator_ChannelFields(t *testing.T) {
	// Verify that the channel fields on Authenticator can hold the correct
	// directional types (chan Payload) and are nil on zero-value struct.
	var a auth.Authenticator

	// input and output channels must be nil (zero value) when uninitialized.
	if a.Input != nil {
		t.Error("Input channel should be nil on zero-value Authenticator")
	}
	if a.Output != nil {
		t.Error("Output channel should be nil on zero-value Authenticator")
	}

	// Verify assignability with the correct element type.
	input := make(chan auth.Payload, 1)
	output := make(chan auth.Payload, 1)
	a.Input = input
	a.Output = output

	if a.Input == nil {
		t.Error("Input channel should not be nil after assignment")
	}
	if a.Output == nil {
		t.Error("Output channel should not be nil after assignment")
	}
}

// ---------------------------------------------------------------------------
// Phase 3 — Constructor and Start pipeline tests (public API)
// ---------------------------------------------------------------------------

// sign is a test-local helper that produces a real ASN1-DER ECDSA signature.
// Mirrors the production path: SHA-256 digest ➜ ecdsa.SignASN1.
func sign(t *testing.T, priv *ecdsa.PrivateKey, data []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("sign: ecdsa.SignASN1: %v", err)
	}
	return sig
}

// newKey produces a fresh P-256 key pair for use inside a single test.
func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("newKey: %v", err)
	}
	return priv
}

// TestNewAuthenticator verifies that the constructor correctly seeds every
// field so that the returned *Authenticator is ready for use by Start.
func TestNewAuthenticator(t *testing.T) {
	priv := newKey(t)

	tests := []struct {
		name       string
		numWorkers int
	}{
		{"single worker", 1},
		{"four workers", 4},
		{"sixteen workers", 16},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			a := auth.NewAuthenticator(ctx, tc.numWorkers, &priv.PublicKey)

			if a == nil {
				t.Fatal("NewAuthenticator returned nil")
			}
			if a.PubKey != &priv.PublicKey {
				t.Error("PubKey not stored correctly")
			}
			if a.NumWorkers != tc.numWorkers {
				t.Errorf("NumWorkers: got %d, want %d", a.NumWorkers, tc.numWorkers)
			}
			if a.Ctx == nil {
				t.Error("Ctx must not be nil")
			}
			// The constructor must initialise the output channel so that Start
			// can immediately begin forwarding verified payloads.
			if a.Output == nil {
				t.Error("Output channel must be non-nil after NewAuthenticator")
			}
			// SpoofedDropped must be zeroed — no drops before any work starts.
			if got := atomic.LoadInt64(&a.SpoofedDropped); got != 0 {
				t.Errorf("SpoofedDropped: got %d, want 0", got)
			}
		})
	}
}

// TestStart_ConcurrentPipeline is the primary stress test for Phase 3.
//
// It:
//  1. Generates a real ECDSA key pair.
//  2. Builds a corpus of payloads: valid signed, tampered-data, and
//     wrong-key signed payloads interleaved in a deterministic pattern.
//  3. Feeds them into the input channel as fast as possible from a goroutine.
//  4. Drains the output channel (which Start must close when input is closed).
//  5. Asserts that received == expectedValid and SpoofedDropped == expectedBad.
func TestStart_ConcurrentPipeline(t *testing.T) {
	const (
		total        = 300  // total payloads in the corpus
		validEvery   = 3    // every Nth payload is valid; others are malformed
		numWorkers   = 8
		testTimeout  = 10 * time.Second
	)

	priv := newKey(t)
	attacker := newKey(t) // different key — its sigs must be rejected

	// Build corpus ────────────────────────────────────────────────────────────
	type payloadKind int
	const (
		kindValid    payloadKind = iota // signed with correct key, data unchanged
		kindTampered                    // correct-key sig, but data flipped
		kindWrongKey                    // structurally valid sig, wrong key
	)

	type entry struct {
		payload auth.Payload
		kind    payloadKind
	}

	corpus := make([]entry, total)
	var expectedValid, expectedBad int64

	for i := range corpus {
		data := []byte{byte(i >> 8), byte(i), 'V', '2', 'X'}
		switch {
		case i%validEvery == 0:
			corpus[i] = entry{
				payload: auth.Payload{Data: data, Signature: sign(t, priv, data)},
				kind:    kindValid,
			}
			expectedValid++
		case i%2 == 1:
			// Sign first, then corrupt one byte — makes the digest mismatch.
			sig := sign(t, priv, data)
			data[len(data)-1] ^= 0xFF
			corpus[i] = entry{
				payload: auth.Payload{Data: data, Signature: sig},
				kind:    kindTampered,
			}
			expectedBad++
		default:
			corpus[i] = entry{
				payload: auth.Payload{Data: data, Signature: sign(t, attacker, data)},
				kind:    kindWrongKey,
			}
			expectedBad++
		}
	}

	// Wire up the authenticator ───────────────────────────────────────────────
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	a := auth.NewAuthenticator(ctx, numWorkers, &priv.PublicKey)

	// Unbuffered input channel forces the workers to pull immediately.
	input := make(chan auth.Payload)
	output := a.Start(input)

	// Feed goroutine ──────────────────────────────────────────────────────────
	go func() {
		for _, e := range corpus {
			input <- e.payload
		}
		close(input) // signals workers to drain and exit
	}()

	// Drain output channel ────────────────────────────────────────────────────
	// Start must close the output channel after all workers finish so that
	// this range terminates without an explicit timeout select.
	var gotValid int64
	for range output {
		gotValid++
	}

	// Assertions ──────────────────────────────────────────────────────────────
	if gotValid != expectedValid {
		t.Errorf("output channel: received %d valid payloads, want %d", gotValid, expectedValid)
	}

	gotBad := atomic.LoadInt64(&a.SpoofedDropped)
	if gotBad != expectedBad {
		t.Errorf("SpoofedDropped: got %d, want %d", gotBad, expectedBad)
	}

	// Sanity: valid + bad must equal total processed.
	if gotValid+gotBad != total {
		t.Errorf("gotValid(%d) + gotBad(%d) = %d, want %d", gotValid, gotBad, gotValid+gotBad, total)
	}
}

