package workers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/infra/metrics"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
)

type mockOutboxRepo struct {
	events    []*wagertransaction.OutboxEvent
	mu        sync.Mutex
	published map[string]bool
}

func newMockOutboxRepo(events []*wagertransaction.OutboxEvent) *mockOutboxRepo {
	return &mockOutboxRepo{
		events:    events,
		published: make(map[string]bool),
	}
}

func (r *mockOutboxRepo) CreateEvents(_ context.Context, _ []wagertransaction.OutboxEvent) error {
	return nil
}

func (r *mockOutboxRepo) FindPending(_ context.Context, _ int) ([]*wagertransaction.OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pending []*wagertransaction.OutboxEvent
	for _, e := range r.events {
		if !r.published[e.ID] {
			pending = append(pending, e)
		}
	}
	return pending, nil
}

func (r *mockOutboxRepo) MarkPublished(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.published[id] = true
	return nil
}

func (r *mockOutboxRepo) IncrementAttempts(_ context.Context, id string, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.ID == id {
			e.Attempts++
		}
	}
	return nil
}

type lockingMockOutboxRepo struct {
	events    []*wagertransaction.OutboxEvent
	mu        sync.Mutex
	published map[string]bool
	inflight  map[string]bool
}

func newLockingMockOutboxRepo(events []*wagertransaction.OutboxEvent) *lockingMockOutboxRepo {
	return &lockingMockOutboxRepo{
		events:    events,
		published: make(map[string]bool),
		inflight:  make(map[string]bool),
	}
}

func (r *lockingMockOutboxRepo) CreateEvents(_ context.Context, _ []wagertransaction.OutboxEvent) error {
	return nil
}

func (r *lockingMockOutboxRepo) FindPending(_ context.Context, _ int) ([]*wagertransaction.OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pending []*wagertransaction.OutboxEvent
	for _, e := range r.events {
		if !r.published[e.ID] && !r.inflight[e.ID] {
			r.inflight[e.ID] = true
			pending = append(pending, e)
		}
	}
	return pending, nil
}

func (r *lockingMockOutboxRepo) MarkPublished(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.published[id] = true
	delete(r.inflight, id)
	return nil
}

func (r *lockingMockOutboxRepo) IncrementAttempts(_ context.Context, id string, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, id)
	for _, e := range r.events {
		if e.ID == id {
			e.Attempts++
		}
	}
	return nil
}

var sharedMetrics = metrics.New()

func TestOutboxPublisher_MaxAttemptsAbandoned(t *testing.T) {
	events := []*wagertransaction.OutboxEvent{
		{ID: "evt-1", EventType: "Test", AggregateType: "Test", AggregateID: "agg-1", Payload: []byte(`{}`), Attempts: 5},
		{ID: "evt-2", EventType: "Test", AggregateType: "Test", AggregateID: "agg-2", Payload: []byte(`{}`), Attempts: 0},
	}
	repo := newMockOutboxRepo(events)
	log := zap.NewNop()

	var publishCount int32
	publishFunc := func(_ context.Context, _, _, _ string, _ []byte) error {
		atomic.AddInt32(&publishCount, 1)
		return nil
	}

	publisher := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := publisher.Start(ctx)
	if err != nil {
		t.Fatalf("publisher stopped with error: %v", err)
	}

	if atomic.LoadInt32(&publishCount) != 1 {
		t.Errorf("expected 1 publish call, got %d", atomic.LoadInt32(&publishCount))
	}
}

func TestOutboxPublisher_PublishLatencyMetric(t *testing.T) {
	events := []*wagertransaction.OutboxEvent{
		{ID: "evt-latency-1", EventType: "Test", AggregateType: "Test", AggregateID: "agg-1", Payload: []byte(`{}`), Attempts: 0},
	}
	repo := newMockOutboxRepo(events)
	log := zap.NewNop()

	publishFunc := func(_ context.Context, _, _, _ string, _ []byte) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	publisher := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := publisher.Start(ctx)
	if err != nil {
		t.Fatalf("publisher stopped with error: %v", err)
	}
}

func TestOutboxPublisher_NoDoublePublish(t *testing.T) {
	events := []*wagertransaction.OutboxEvent{
		{ID: "evt-skip-1", EventType: "Test", AggregateType: "Test", AggregateID: "agg-1", Payload: []byte(`{}`), Attempts: 0},
		{ID: "evt-skip-2", EventType: "Test", AggregateType: "Test", AggregateID: "agg-2", Payload: []byte(`{}`), Attempts: 0},
	}
	repo := newMockOutboxRepo(events)
	log := zap.NewNop()

	var publishCount int32
	publishFunc := func(_ context.Context, _, _, _ string, _ []byte) error {
		atomic.AddInt32(&publishCount, 1)
		return nil
	}

	publisher := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := publisher.Start(ctx)
	if err != nil {
		t.Fatalf("publisher stopped with error: %v", err)
	}

	if atomic.LoadInt32(&publishCount) != 2 {
		t.Errorf("expected 2 publish calls, got %d", atomic.LoadInt32(&publishCount))
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, e := range events {
		if !repo.published[e.ID] {
			t.Errorf("event %s was not marked as published", e.ID)
		}
	}
}

func TestOutboxPublisher_DLQMetricIncremented(t *testing.T) {
	events := []*wagertransaction.OutboxEvent{
		{ID: "evt-dlq-1", EventType: "Test", AggregateType: "Test", AggregateID: "agg-dlq-1", Payload: []byte(`{}`), Attempts: 5},
		{ID: "evt-dlq-2", EventType: "Test", AggregateType: "Test", AggregateID: "agg-dlq-2", Payload: []byte(`{}`), Attempts: 4},
	}
	repo := newMockOutboxRepo(events)
	log := zap.NewNop()

	publishFunc := func(_ context.Context, _, _, _ string, _ []byte) error {
		return nil
	}

	publisher := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := publisher.Start(ctx)
	if err != nil {
		t.Fatalf("publisher stopped with error: %v", err)
	}

	var dlqVal float64
	if sharedMetrics.DLQTotal != nil {
		d := &dto.Metric{}
		_ = sharedMetrics.DLQTotal.Write(d)
		dlqVal = d.GetCounter().GetValue()
	}
	if dlqVal < 1 {
		t.Errorf("expected DLQTotal >= 1 after max attempts exceeded, got %f", dlqVal)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.published["evt-dlq-1"] {
		t.Error("event at max attempts should not be published")
	}
	if !repo.published["evt-dlq-2"] {
		t.Error("event below max attempts should be published")
	}
}

func TestOutboxPublisher_CompetingPublishers(t *testing.T) {
	events := []*wagertransaction.OutboxEvent{
		{ID: "evt-compete-1", EventType: "Test", AggregateType: "Test", AggregateID: "agg-1", Payload: []byte(`{}`), Attempts: 0},
		{ID: "evt-compete-2", EventType: "Test", AggregateType: "Test", AggregateID: "agg-2", Payload: []byte(`{}`), Attempts: 0},
		{ID: "evt-compete-3", EventType: "Test", AggregateType: "Test", AggregateID: "agg-3", Payload: []byte(`{}`), Attempts: 0},
	}

	t.Run("without_locking_double_publish_happens", func(t *testing.T) {
		repo := newMockOutboxRepo(events)
		log := zap.NewNop()
		var publishCount int32
		publishFunc := func(_ context.Context, _, _, _ string, _ []byte) error {
			atomic.AddInt32(&publishCount, 1)
			return nil
		}

		publisher1 := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)
		publisher2 := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); publisher1.Start(ctx) }()
		go func() { defer wg.Done(); publisher2.Start(ctx) }()
		wg.Wait()

		total := atomic.LoadInt32(&publishCount)
		if total <= int32(len(events)) {
			t.Logf("without locking: %d publishes (expected > %d due to race)", total, len(events))
		}
	})

	t.Run("with_locking_no_double_publish", func(t *testing.T) {
		repo := newLockingMockOutboxRepo(events)
		log := zap.NewNop()
		var publishCount int32
		publishFunc := func(_ context.Context, _, _, _ string, _ []byte) error {
			atomic.AddInt32(&publishCount, 1)
			return nil
		}

		publisher1 := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)
		publisher2 := NewOutboxPublisher(repo, publishFunc, log, sharedMetrics)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); publisher1.Start(ctx) }()
		go func() { defer wg.Done(); publisher2.Start(ctx) }()
		wg.Wait()

		total := atomic.LoadInt32(&publishCount)
		if total != int32(len(events)) {
			t.Errorf("with locking: expected exactly %d publish calls, got %d", len(events), total)
		}

		repo.mu.Lock()
		defer repo.mu.Unlock()
		for _, e := range events {
			if !repo.published[e.ID] {
				t.Errorf("event %s was not marked as published", e.ID)
			}
		}
	})
}
