package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
)

// Ingester is responsible for receiving single logs concurrently and
// bulk-inserting them into the database using an internal buffer.
type Ingester struct {
	logChan chan model.TelemetryLog
	repo    repository.TelemetryRepository

	batchSize    int
	flushTimeout time.Duration

	wg sync.WaitGroup
}

// NewIngester creates and starts a background worker.
func NewIngester(repo repository.TelemetryRepository, batchSize int, timeoutSec time.Duration) *Ingester {
	ing := &Ingester{
		// Buffered channel: HTTP handlers won't block immediately if there's a spike.
		logChan:      make(chan model.TelemetryLog, batchSize*2),
		repo:         repo,
		batchSize:    batchSize,
		flushTimeout: timeoutSec,
	}

	// Start the background consumer
	ing.wg.Add(1)
	go ing.startConsumer()

	return ing
}

// Enqueue is called by the HTTP handler. It is non-blocking (up to the channel capacity).
func (i *Ingester) Enqueue(log model.TelemetryLog) {
	i.logChan <- log
}

// startConsumer runs continuously in the background, grouping logs into batches.
func (i *Ingester) startConsumer() {
	defer i.wg.Done()

	buffer := make([]model.TelemetryLog, 0, i.batchSize)
	ticker := time.NewTicker(i.flushTimeout)
	defer ticker.Stop()

	for {
		select {
		case logEvent, ok := <-i.logChan:
			if !ok {
				// Channel closed during graceful shutdown. Flush remaining and exit.
				i.flushBatch(buffer)
				return
			}

			buffer = append(buffer, logEvent)

			// Flush if buffer is full
			if len(buffer) >= i.batchSize {
				i.flushBatch(buffer)
				// Reset buffer efficiently by keeping the underlying array
				buffer = buffer[:0]
				ticker.Reset(i.flushTimeout) // Reset ticker since we just flushed
			}

		case <-ticker.C:
			// Flush if timeout reached, even if buffer isn't full
			if len(buffer) > 0 {
				i.flushBatch(buffer)
				buffer = buffer[:0]
			}
		}
	}
}

func (i *Ingester) flushBatch(batch []model.TelemetryLog) {
	if len(batch) == 0 {
		return
	}

	// Ensure we don't block the worker forever on a stalled DB
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := i.repo.SaveLogsBatch(ctx, batch); err != nil {
		// Log the error using the new structured logger
		slog.Error("Failed to flush batch", "count", len(batch), "error", err)
	} else {
		slog.Info("Successfully flushed batch", "count", len(batch))
	}
}

// Stop closes the channel and waits for the final flush to complete.
func (i *Ingester) Stop() {
	close(i.logChan)
	i.wg.Wait()
}
