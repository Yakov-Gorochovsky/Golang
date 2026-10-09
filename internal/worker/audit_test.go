package worker

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAuditProcessor_SpawnsExactWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	numWorkers := 7
	processor := NewAuditProcessor(ctx, numWorkers)

	input := make(chan RawEvent)

	// Track goroutines before start
	initialGoroutines := runtime.NumGoroutine()

	output := processor.Start(input)

	// Small sleep to ensure all goroutines are scheduled and parked on select.
	time.Sleep(50 * time.Millisecond)

	currentGoroutines := runtime.NumGoroutine()

	// We expect exactly `numWorkers` + 1 (for the coordinator WaitGroup routine)
	expectedSpawned := numWorkers + 1
	actualSpawned := currentGoroutines - initialGoroutines

	if actualSpawned != expectedSpawned {
		t.Errorf("Expected processor to spawn exactly %d goroutines, but got %d", expectedSpawned, actualSpawned)
	}

	// Clean up by closing input to let workers exit cleanly
	close(input)
	<-output // Wait for coordinator to close output
}

func TestAuditProcessor_DataSanitization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	processor := NewAuditProcessor(ctx, 2)
	input := make(chan RawEvent, 5)
	output := processor.Start(input)

	input <- RawEvent{ID: "evt_1", Details: "User email: john.doe@example.com logged in"}
	input <- RawEvent{ID: "evt_2", Details: "No PII in this message"}
	input <- RawEvent{ID: "evt_3", Details: "Multiple alerts: admin@server.net and root@box.org failed"}

	close(input)

	var results []ProcessedEvent
	for ev := range output {
		results = append(results, ev)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 events processed, got %d", len(results))
	}

	// We'll iterate and check based on expected outcomes because worker execution order is non-deterministic.
	foundMaskedJohn := false
	foundNoPII := false
	foundMultiple := false

	for _, ev := range results {
		if ev.ID == "evt_1" && ev.Details == "User email: *@*.* logged in" {
			foundMaskedJohn = true
		}
		if ev.ID == "evt_2" && ev.Details == "No PII in this message" {
			foundNoPII = true
		}
		if ev.ID == "evt_3" && ev.Details == "Multiple alerts: *@*.* and *@*.* failed" {
			foundMultiple = true
		}
	}

	if !foundMaskedJohn {
		t.Errorf("Failed to mask single email securely")
	}
	if !foundNoPII {
		t.Errorf("Altered event that contained no PII")
	}
	if !foundMultiple {
		t.Errorf("Failed to mask multiple emails in the same record")
	}
}

func TestAuditProcessor_GracefulShutdownAndNoDataLoss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	processor := NewAuditProcessor(ctx, 3)

	// Unbuffered input channel perfectly ensures an event is confirmed
	// to be "in a worker's hands" the moment the send operation resolves.
	input := make(chan RawEvent)
	output := processor.Start(input)

	// We will send 3 events (one for each worker).
	var sendWg sync.WaitGroup
	sendWg.Add(3)

	go func() {
		input <- RawEvent{ID: "evt_1", Details: "one@test.com"}
		sendWg.Done()
	}()
	go func() {
		input <- RawEvent{ID: "evt_2", Details: "two@test.com"}
		sendWg.Done()
	}()
	go func() {
		input <- RawEvent{ID: "evt_3", Details: "three@test.com"}
		sendWg.Done()
	}()

	// Wait for all 3 events to be received by the 3 workers.
	sendWg.Wait()

	// NOW, we cancel the context.
	// The requirement is to finish processing ANY events currently in hands,
	// and to safely close the output channel.
	cancel()

	// We must now be able to read exactly 3 events from the output channel.
	// A timeout guarantees the test fails if workers deadlock.
	received := 0
	timeout := time.After(2 * time.Second)

loop:
	for {
		select {
		case ev, ok := <-output:
			if !ok {
				break loop // Output channel safely closed
			}
			if ev.Details != "*@*.*" {
				t.Errorf("Unexpected detail payload: %s", ev.Details)
			}
			received++
		case <-timeout:
			t.Fatalf("Test timed out waiting for goroutines to flush in-flight events and close output channel")
		}
	}

	if received != 3 {
		t.Errorf("Expected exactly 3 processed events before shutdown, but got %d", received)
	}

	// Because we read until the channel closed (ok == false),
	// this automatically proves the Graceful Shutdown logic closed the output safely.
}
