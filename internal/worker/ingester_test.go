package worker_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yakov-Gorochovsky/project/internal/model"
	"github.com/Yakov-Gorochovsky/project/internal/worker"
)

type mockRepo struct {
	mu           sync.Mutex
	flushedLogs  []model.TelemetryLog
	flushCount   int
	flushDelay   time.Duration
	saveLogErr   error
	saveBatchErr error
}

func (m *mockRepo) SaveLog(ctx context.Context, log model.TelemetryLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveLogErr != nil {
		return m.saveLogErr
	}
	m.flushedLogs = append(m.flushedLogs, log)
	return nil
}

func (m *mockRepo) SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error {
	if m.flushDelay > 0 {
		time.Sleep(m.flushDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveBatchErr != nil {
		return m.saveBatchErr
	}
	m.flushCount++
	m.flushedLogs = append(m.flushedLogs, logs...)
	return nil
}

func (m *mockRepo) TotalFlushed() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.flushedLogs)
}

func (m *mockRepo) FlushCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushCount
}

func TestIngester_FlushOnBatchSize(t *testing.T) {
	repo := &mockRepo{}
	batchSize := 10
	flushTimeout := 5 * time.Second // High timeout so only size triggers flush

	ingester := worker.NewIngester(repo, batchSize, flushTimeout)
	defer ingester.Stop()

	for i := 0; i < batchSize; i++ {
		ingester.Enqueue(model.TelemetryLog{
			DeviceID:  "dev-1",
			EventType: "ping",
			Payload:   map[string]interface{}{"i": i},
		})
	}

	// Give the consumer a brief moment to process the batch
	time.Sleep(100 * time.Millisecond)

	if repo.TotalFlushed() != batchSize {
		t.Fatalf("expected %d flushed logs, got %d", batchSize, repo.TotalFlushed())
	}
	if repo.FlushCalls() != 1 {
		t.Fatalf("expected 1 flush call, got %d", repo.FlushCalls())
	}
}

func TestIngester_FlushOnTimeout(t *testing.T) {
	repo := &mockRepo{}
	batchSize := 100
	flushTimeout := 50 * time.Millisecond

	ingester := worker.NewIngester(repo, batchSize, flushTimeout)
	defer ingester.Stop()

	// Send only 5 items (less than batchSize)
	for i := 0; i < 5; i++ {
		ingester.Enqueue(model.TelemetryLog{
			DeviceID:  "dev-2",
			EventType: "ping",
			Payload:   map[string]interface{}{"i": i},
		})
	}

	// Wait for the ticker to trigger
	time.Sleep(150 * time.Millisecond)

	if repo.TotalFlushed() != 5 {
		t.Fatalf("expected 5 flushed logs by timeout, got %d", repo.TotalFlushed())
	}
}

func TestIngester_GracefulShutdownDrainsQueue(t *testing.T) {
	repo := &mockRepo{}
	batchSize := 100
	flushTimeout := 10 * time.Second

	ingester := worker.NewIngester(repo, batchSize, flushTimeout)

	// Enqueue items without reaching batchSize or timeout
	totalSent := 25
	for i := 0; i < totalSent; i++ {
		ingester.Enqueue(model.TelemetryLog{
			DeviceID:  "dev-3",
			EventType: "sensor",
			Payload:   map[string]interface{}{"val": i},
		})
	}

	// Stop must wait for remaining buffer to flush completely
	ingester.Stop()

	if repo.TotalFlushed() != totalSent {
		t.Fatalf("expected all %d logs flushed on Stop, got %d", totalSent, repo.TotalFlushed())
	}
}

type atomicRepo struct {
	totalFlushed atomic.Int64
}

func (a *atomicRepo) SaveLog(ctx context.Context, log model.TelemetryLog) error {
	a.totalFlushed.Add(1)
	return nil
}

func (a *atomicRepo) SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error {
	a.totalFlushed.Add(int64(len(logs)))
	return nil
}

func BenchmarkIngester_Enqueue(b *testing.B) {
	repo := &atomicRepo{}
	batchSize := 500
	flushTimeout := 50 * time.Millisecond

	ingester := worker.NewIngester(repo, batchSize, flushTimeout)
	defer ingester.Stop()

	sampleLog := model.TelemetryLog{
		DeviceID:  "bench-dev",
		EventType: "status",
		Payload:   map[string]interface{}{"status": "healthy"},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ingester.Enqueue(sampleLog)
	}
}

func TestIngester_NonBlockingBackpressureUnderSaturation(t *testing.T) {
	flushDelay := 100 * time.Millisecond
	repo := &mockRepo{flushDelay: flushDelay}

	// Buffer capacity is batchSize * 2 = 4
	batchSize := 2
	flushTimeout := 1 * time.Second
	ingester := worker.NewIngester(repo, batchSize, flushTimeout)
	defer ingester.Stop()

	// Send 2 items to trigger flushBatch, which sleeps for 200ms
	for i := 0; i < 2; i++ {
		if !ingester.Enqueue(model.TelemetryLog{DeviceID: "init"}) {
			t.Fatalf("expected initial warmup item #%d to succeed", i)
		}
	}

	// Brief pause to ensure the consumer picked up the 2 items and entered flushBatch
	time.Sleep(20 * time.Millisecond)

	// Now fill the channel buffer completely (capacity = 4)
	for i := 0; i < 4; i++ {
		if !ingester.Enqueue(model.TelemetryLog{DeviceID: "fill-chan"}) {
			t.Fatalf("expected channel buffer fill #%d to succeed", i)
		}
	}

	// Channel is now 100% saturated. Test non-blocking load shedding.
	start := time.Now()
	ok := ingester.Enqueue(model.TelemetryLog{
		DeviceID:  "overflow-req",
		EventType: "burst",
		Payload:   map[string]interface{}{"dropped": true},
	})
	elapsed := time.Since(start)

	t.Logf("[PHASE 1 EVIDENCE] Saturated Enqueue completed in %v, returned: %v", elapsed, ok)

	if ok {
		t.Fatalf("expected Enqueue to return false when channel buffer is saturated")
	}

	if elapsed > 1*time.Millisecond {
		t.Fatalf("expected non-blocking return (<1ms), but blocked for %v", elapsed)
	}

	if ingester.DroppedTotal() != 1 {
		t.Fatalf("expected DroppedTotal == 1, got %d", ingester.DroppedTotal())
	}

	if ingester.EnqueuedTotal() != 6 {
		t.Fatalf("expected EnqueuedTotal == 6, got %d", ingester.EnqueuedTotal())
	}
}

func BenchmarkIngester_Enqueue_Saturated(b *testing.B) {
	repo := &mockRepo{flushDelay: 50 * time.Millisecond}
	batchSize := 2
	flushTimeout := 1 * time.Second

	ingester := worker.NewIngester(repo, batchSize, flushTimeout)
	defer ingester.Stop()

	// Pre-saturate
	for i := 0; i < 6; i++ {
		ingester.Enqueue(model.TelemetryLog{DeviceID: "fill"})
	}

	sampleLog := model.TelemetryLog{DeviceID: "overflow"}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ingester.Enqueue(sampleLog)
	}
}
