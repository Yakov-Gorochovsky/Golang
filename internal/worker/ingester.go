package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
)

// Ingester buffers incoming telemetry logs and bulk-inserts them using a concurrent worker pool.
type Ingester struct {
	logChan       chan model.TelemetryLog
	repo          repository.TelemetryRepository
	batchSize     int
	flushTimeout  time.Duration
	workerCount   int
	wg            sync.WaitGroup
	enqueuedCount atomic.Uint64
	droppedCount  atomic.Uint64
}

// NewIngester initializes and starts the ingestion worker pool.
// Optional workerCount sets the number of concurrent consumers (default: 1).
func NewIngester(repo repository.TelemetryRepository, batchSize int, timeoutSec time.Duration, workerCount ...int) *Ingester {
	numWorkers := 1
	if len(workerCount) > 0 && workerCount[0] > 0 {
		numWorkers = workerCount[0]
	}

	ing := &Ingester{
		logChan:      make(chan model.TelemetryLog, batchSize*numWorkers*2),
		repo:         repo,
		batchSize:    batchSize,
		flushTimeout: timeoutSec,
		workerCount:  numWorkers,
	}

	ing.wg.Add(numWorkers)
	for w := 0; w < numWorkers; w++ {
		go ing.startConsumer(w)
	}

	return ing
}

// Enqueue attempts to buffer a log event. Returns false immediately if the queue
// is saturated, enforcing non-blocking backpressure without stalling callers.
func (i *Ingester) Enqueue(log model.TelemetryLog) bool {
	select {
	case i.logChan <- log:
		i.enqueuedCount.Add(1)
		return true
	default:
		i.droppedCount.Add(1)
		return false
	}
}

// EnqueuedTotal returns the cumulative count of successfully buffered logs.
func (i *Ingester) EnqueuedTotal() uint64 {
	return i.enqueuedCount.Load()
}

// DroppedTotal returns the cumulative count of dropped logs due to buffer saturation.
func (i *Ingester) DroppedTotal() uint64 {
	return i.droppedCount.Load()
}

// QueueDepth returns the current number of logs queued in the channel buffer.
func (i *Ingester) QueueDepth() int {
	return len(i.logChan)
}

// WorkerCount returns the number of active consumers in the pool.
func (i *Ingester) WorkerCount() int {
	return i.workerCount
}

func (i *Ingester) startConsumer(workerID int) {
	defer i.wg.Done()

	buffer := make([]model.TelemetryLog, 0, i.batchSize)
	ticker := time.NewTicker(i.flushTimeout)
	defer ticker.Stop()

	for {
		select {
		case logEvent, ok := <-i.logChan:
			if !ok {
				i.flushBatch(buffer)
				return
			}

			buffer = append(buffer, logEvent)
			if len(buffer) >= i.batchSize {
				i.flushBatch(buffer)
				buffer = buffer[:0]
				ticker.Reset(i.flushTimeout)
			}

		case <-ticker.C:
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := i.repo.SaveLogsBatch(ctx, batch); err != nil {
		slog.Error("Failed to flush batch", "count", len(batch), "error", err)
		return
	}
	slog.Debug("Flushed batch", "count", len(batch))
}

// Stop drains remaining buffered events across all workers and shuts down the pool.
func (i *Ingester) Stop() {
	close(i.logChan)
	i.wg.Wait()
}
