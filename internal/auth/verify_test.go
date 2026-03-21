// Package auth white-box tests for unexported implementation details.
//
// This file uses "package auth" (not "package auth_test") so that it can
// access unexported identifiers such as the verify method.  It coexists
// alongside authenticator_test.go (package auth_test) in the same directory —
// the standard Go pattern for mixed black-box / white-box test suites.
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"testing"
)

// signPayload is a test-only helper that produces a real ASN.1-DER ECDSA
// signature over the SHA-256 hash of data using the supplied private key.
// It mirrors exactly what the production verify() path must accept.
func signPayload(t *testing.T, priv *ecdsa.PrivateKey, data []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("signPayload: ecdsa.SignASN1 failed: %v", err)
	}
	return sig
}

// generateKey creates a fresh P-256 key pair.  Each sub-test that needs
// isolation should call this independently so key material is never shared.
func generateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generateKey: ecdsa.GenerateKey failed: %v", err)
	}
	return priv
}

// ---------------------------------------------------------------------------
// TestAuthenticator_verify — table-driven, no mocking, real crypto only.
// ---------------------------------------------------------------------------

func TestAuthenticator_verify(t *testing.T) {
	// Generate one key pair for the shared "happy path" and tamper cases.
	// The "wrong key" case generates its own pair inside the table.
	priv := generateKey(t)

	vehicleData := []byte("V2X|LAT=32.0853|LON=34.7818|SPEED=90|T=1711051200")
	validSig := signPayload(t, priv, vehicleData)

	tests := []struct {
		name    string
		setup   func(t *testing.T) (*Authenticator, Payload)
		want    bool
	}{
		{
			// Golden path: data was signed by the key the Authenticator holds.
			// verify() must return true without error.
			name: "valid signature over unchanged data returns true",
			setup: func(t *testing.T) (*Authenticator, Payload) {
				t.Helper()
				a := &Authenticator{PubKey: &priv.PublicKey}
				p := Payload{Data: vehicleData, Signature: validSig}
				return a, p
			},
			want: true,
		},
		{
			// Adversary replaces the signature with random noise.
			// verify() must return false — pure garbage bytes are not a valid
			// ASN.1 ECDSA signature schema and must be rejected.
			name: "random bytes as signature returns false",
			setup: func(t *testing.T) (*Authenticator, Payload) {
				t.Helper()
				noise := make([]byte, 64)
				if _, err := rand.Read(noise); err != nil {
					t.Fatalf("rand.Read: %v", err)
				}
				a := &Authenticator{PubKey: &priv.PublicKey}
				p := Payload{Data: vehicleData, Signature: noise}
				return a, p
			},
			want: false,
		},
		{
			// Adversary presents a structurally valid ECDSA signature but one
			// that was produced by a completely different private key.
			// verify() must return false regardless of ASN.1 validity.
			name: "signature from a different key returns false",
			setup: func(t *testing.T) (*Authenticator, Payload) {
				t.Helper()
				attacker := generateKey(t) // different key pair — never matches priv
				attackerSig := signPayload(t, attacker, vehicleData)
				a := &Authenticator{PubKey: &priv.PublicKey}
				p := Payload{Data: vehicleData, Signature: attackerSig}
				return a, p
			},
			want: false,
		},
		{
			// Adversary keeps the original signature but modifies a single
			// byte of the data after signing (bit-flip / injection attack).
			// The SHA-256 digest will differ, so verify() must return false.
			name: "tampered data with original signature returns false",
			setup: func(t *testing.T) (*Authenticator, Payload) {
				t.Helper()
				// Deep-copy so we don't mutate vehicleData for other cases.
				tampered := make([]byte, len(vehicleData))
				copy(tampered, vehicleData)
				tampered[len(tampered)-1] ^= 0xFF // flip last byte
				a := &Authenticator{PubKey: &priv.PublicKey}
				p := Payload{Data: tampered, Signature: validSig}
				return a, p
			},
			want: false,
		},
		{
			// Boundary: empty Data slice with a valid-looking signature.
			// The digest of empty bytes differs from any real payload, so
			// verify() must return false.
			name: "empty data with signature over non-empty data returns false",
			setup: func(t *testing.T) (*Authenticator, Payload) {
				t.Helper()
				a := &Authenticator{PubKey: &priv.PublicKey}
				p := Payload{Data: []byte{}, Signature: validSig}
				return a, p
			},
			want: false,
		},
		{
			// Boundary: nil Signature field must not panic — verify() should
			// treat it as a definitively invalid signature and return false.
			name: "nil signature returns false without panic",
			setup: func(t *testing.T) (*Authenticator, Payload) {
				t.Helper()
				a := &Authenticator{PubKey: &priv.PublicKey}
				p := Payload{Data: vehicleData, Signature: nil}
				return a, p
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, p := tc.setup(t)
			got := a.verify(p)
			if got != tc.want {
				t.Errorf("verify() = %v, want %v", got, tc.want)
			}
		})
	}
}
