package worker

import (
	"context"
	"regexp"
	"sync"
)

// RawEvent represents an incoming audit event before sanitization.
type RawEvent struct {
	ID      string
	Details string
}

// ProcessedEvent represents an audit event after sanitization.
type ProcessedEvent struct {
	ID      string
	Details string
}

// AuditProcessor manages the concurrent sanitization of audit events.
type AuditProcessor struct {
	ctx        context.Context
	numWorkers int
}

// NewAuditProcessor creates a fresh AuditProcessor bound to the given context.
func NewAuditProcessor(ctx context.Context, numWorkers int) *AuditProcessor {
	return &AuditProcessor{
		ctx:        ctx,
		numWorkers: numWorkers,
	}
}

// emailRegex securely matches email addresses for masking.
var emailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

func maskEmail(details string) string {
	// The requirement specifies masking e.g., user@example.com to *@*.*
	return emailRegex.ReplaceAllString(details, "*@*.*")
}

// Start spawns exactly numWorkers goroutines to process incoming events.
// It returns a read-only channel where sanitized ProcessedEvents are emitted.
func (ap *AuditProcessor) Start(input <-chan RawEvent) <-chan ProcessedEvent {
	output := make(chan ProcessedEvent)
	var wg sync.WaitGroup

	for i := 0; i < ap.numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ap.ctx.Done():
					// Context is canceled, we stop accepting new events
					return
				case ev, ok := <-input:
					if !ok {
						// Input channel closed, worker shuts down gracefully
						return
					}
					// Event is now in the worker's hands, we proceed to process it.
					processed := ProcessedEvent{
						ID:      ev.ID,
						Details: maskEmail(ev.Details),
					}

					// Send the processed event to output.
					// We block here until it's sent to guarantee no in-flight data is lost.
					// The caller must actively drain the output channel until it's closed.
					output <- processed
				}
			}
		}()
	}

	// Coordinator goroutine: waits for all processing workers to finish
	// before safely closing the output channel, preventing deadlocks or leaks.
	go func() {
		wg.Wait()
		close(output)
	}()

	return output
}
