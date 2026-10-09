package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/Yakov-Gorochovsky/project/internal/repository"
)

// Ingester buffers incoming telemetry logs and bulk-inserts them into repository storage.
type Ingester struct {
	logChan      chan model.TelemetryLog
	repo         repository.TelemetryRepository
	batchSize    int
	flushTimeout time.Duration
	wg           sync.WaitGroup
}

// NewIngester initializes and starts the background ingestion worker.
func NewIngester(repo repository.TelemetryRepository, batchSize int, timeoutSec time.Duration) *Ingester {
	ing := &Ingester{
		logChan:      make(chan model.TelemetryLog, batchSize*2),
		repo:         repo,
		batchSize:    batchSize,
		flushTimeout: timeoutSec,
	}

	ing.wg.Add(1)
	go ing.startConsumer()

	return ing
}

// Enqueue adds a telemetry log to the processing buffer.
func (i *Ingester) Enqueue(log model.TelemetryLog) {
	i.logChan <- log
}

func (i *Ingester) startConsumer() {
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

// Stop drains remaining buffered events and shuts down the consumer.
func (i *Ingester) Stop() {
	close(i.logChan)
	i.wg.Wait()
}
