# Context
Our V2X collision avoidance system receives thousands of telemetry broadcasts per second. Before processing them, we must cryptographically verify that the messages haven't been spoofed. We need a highly concurrent, memory-safe worker pool to verify ECDSA (Elliptic Curve Digital Signature Algorithm) signatures on the fly without blocking the ingestion thread.

# Acceptance Criteria
- **[Feature 1 - Structs]**: Define a `Payload` struct containing the raw data and its ECDSA signature.
- **[Feature 2 - Constructor]**: Create `NewAuthenticator(ctx context.Context, numWorkers int, pubKey *ecdsa.PublicKey) *Authenticator`. It must hold the required channels, `sync.WaitGroup`, and an atomic counter for dropped messages.
- **[Feature 3 - Pipeline]**: Implement a `Start(input <-chan Payload) <-chan Payload` method. It must immediately spin up exactly `numWorkers` goroutines.
- **[Feature 4 - Worker Logic]**: Workers range over the `input` channel. For each payload, use `crypto/ecdsa` and `crypto/sha256` to verify the signature against the `pubKey`.
  - If valid: pass the payload to the output channel.
  - If invalid: increment a `SpoofedDropped` atomic counter and discard the message.
- **[Feature 5 - Graceful Shutdown]**: When the `ctx` is canceled OR the `input` channel is closed, workers must finish their current verification, call `wg.Done()`, and exit. A separate background coordinator must `wg.Wait()` and then safely close the output channel to prevent deadlocks.

# Technical Constraints
- **Language**: Go 1.21+
- **Libraries**: Use ONLY the standard library (`crypto/ecdsa`, `crypto/sha256`, `sync`, `context`, `sync/atomic`). NO third-party packages.
- **Testing**: 100% coverage required. You MUST generate real `ecdsa.PrivateKey` / `ecdsa.PublicKey` pairs on the fly inside the tests to sign test payloads. Do not attempt to "mock" the crypto package. Tests must run with the `-race` flag and simulate a context cancellation.

# Out of Scope
- Do NOT implement the UDP network listener or ingestions layers.
- Do NOT implement database storage.
- Do NOT implement the actual collision detection math.
