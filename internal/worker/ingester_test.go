package worker_test

import (
	"context"
	"errors"
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

func TestIngester_Bottleneck_SingleWorkerDrainSpeed(t *testing.T) {
	flushDelay := 5 * time.Millisecond
	repo := &mockRepo{flushDelay: flushDelay}

	batchSize := 50
	flushTimeout := 2 * time.Second
	ingester := worker.NewIngester(repo, batchSize, flushTimeout)

	totalLogs := 1000
	start := time.Now()

	for i := 0; i < totalLogs; i++ {
		for !ingester.Enqueue(model.TelemetryLog{DeviceID: "bench", EventType: "test"}) {
			time.Sleep(1 * time.Millisecond)
		}
	}

	ingester.Stop()
	elapsed := time.Since(start)

	rate := float64(repo.TotalFlushed()) / elapsed.Seconds()
	t.Logf("[PHASE 2 BOTTLENECK EVIDENCE] 1 Worker processed %d logs in %v (rate: %.0f logs/sec)",
		repo.TotalFlushed(), elapsed, rate)

	if elapsed < 80*time.Millisecond {
		t.Errorf("expected single worker to be constrained by sequential flush latency")
	}
}

func TestIngester_WorkerPool_ScalingSpeedup(t *testing.T) {
	flushDelay := 5 * time.Millisecond
	totalLogs := 1000
	batchSize := 50
	flushTimeout := 2 * time.Second

	// Run with 1 worker
	repo1 := &mockRepo{flushDelay: flushDelay}
	ingester1 := worker.NewIngester(repo1, batchSize, flushTimeout, 1)
	start1 := time.Now()
	for i := 0; i < totalLogs; i++ {
		for !ingester1.Enqueue(model.TelemetryLog{DeviceID: "dev", EventType: "test"}) {
			time.Sleep(500 * time.Microsecond)
		}
	}
	ingester1.Stop()
	dur1 := time.Since(start1)

	// Run with 4 workers
	repo4 := &mockRepo{flushDelay: flushDelay}
	ingester4 := worker.NewIngester(repo4, batchSize, flushTimeout, 4)
	start4 := time.Now()
	for i := 0; i < totalLogs; i++ {
		for !ingester4.Enqueue(model.TelemetryLog{DeviceID: "dev", EventType: "test"}) {
			time.Sleep(500 * time.Microsecond)
		}
	}
	ingester4.Stop()
	dur4 := time.Since(start4)

	rate1 := float64(totalLogs) / dur1.Seconds()
	rate4 := float64(totalLogs) / dur4.Seconds()
	speedup := float64(dur1) / float64(dur4)

	t.Logf("[PHASE 2 EVIDENCE] 1 Worker: %v (%.0f logs/sec) | 4 Workers: %v (%.0f logs/sec) | Speedup: %.2fx",
		dur1, rate1, dur4, rate4, speedup)

	if repo1.TotalFlushed() != totalLogs {
		t.Fatalf("expected repo1 to have %d logs, got %d", totalLogs, repo1.TotalFlushed())
	}
	if repo4.TotalFlushed() != totalLogs {
		t.Fatalf("expected repo4 to have %d logs, got %d", totalLogs, repo4.TotalFlushed())
	}

	if speedup < 2.0 {
		t.Errorf("expected 4 workers to provide at least 2x speedup over 1 worker, got %.2fx", speedup)
	}
}

type flakyBatchRepo struct {
	mu          sync.Mutex
	failCount   int
	maxFails    int
	flushedLogs []model.TelemetryLog
}

func (f *flakyBatchRepo) SaveLog(ctx context.Context, log model.TelemetryLog) error {
	return nil
}

func (f *flakyBatchRepo) SaveLogsBatch(ctx context.Context, logs []model.TelemetryLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCount < f.maxFails {
		f.failCount++
		return errors.New("transient database connection reset")
	}
	f.flushedLogs = append(f.flushedLogs, logs...)
	return nil
}

func (f *flakyBatchRepo) TotalFlushed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.flushedLogs)
}

func TestIngester_Bottleneck_DataLossOnTransientStorageFailure(t *testing.T) {
	// Simulate a database experiencing 2 transient errors (e.g. connection reset) before recovering.
	flakyRepo := &flakyBatchRepo{maxFails: 2}
	batchSize := 20
	flushTimeout := 50 * time.Millisecond

	ingester := worker.NewIngester(flakyRepo, batchSize, flushTimeout, 1)

	for i := 0; i < batchSize; i++ {
		ingester.Enqueue(model.TelemetryLog{
			DeviceID:  "device-resilience",
			EventType: "heartbeat",
		})
	}

	// Stop triggers final batch flush
	ingester.Stop()

	t.Logf("[HARDENING EVIDENCE 1] Transient failures simulated: %d, logs successfully flushed: %d / %d",
		flakyRepo.failCount, flakyRepo.TotalFlushed(), batchSize)

	if flakyRepo.TotalFlushed() != batchSize {
		t.Fatalf("DATA LOSS DETECTED: expected %d logs preserved after transient failure, got %d (loss rate: %.1f%%)",
			batchSize, flakyRepo.TotalFlushed(),
			float64(batchSize-flakyRepo.TotalFlushed())/float64(batchSize)*100.0)
	}
}
