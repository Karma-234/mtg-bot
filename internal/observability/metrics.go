package observability

import (
	"sync"
	"time"
)

// Metrics holds counters and gauges for operational visibility.
type Metrics struct {
	mu sync.RWMutex
	// Poll cycle timing
	PollCycleDurationMS *Histogram
	PollCycleCount      *Counter

	// Webhook processing
	WebhookLatencyMS *Histogram
	WebhookCount     *Counter
	WebhookErrors    *Counter

	// Queue operations
	QueueDepth      *Gauge
	QueueLagSeconds *Histogram

	// Retry tracking
	RetryCount     *Counter
	RetryExhausted *Counter

	// Payment intent state transitions
	PaymentIntentCreated     *Counter
	PaymentIntentTransferred *Counter
	PaymentIntentFailed      *Counter
}

// Counter tracks occurrences of an event.
type Counter struct {
	name  string
	mu    sync.Mutex
	value int64
}

// Gauge tracks a value that can go up and down.
type Gauge struct {
	name string

	mu    sync.Mutex
	value int64
}

// Histogram records values that should be aggregated over time (min, max, avg, percentiles).
type Histogram struct {
	buckets []int64 // Duration in milliseconds
	name    string
	mu      sync.Mutex
}

// NewMetrics creates a new Metrics instance with all histograms initialized.
func NewMetrics() *Metrics {
	return &Metrics{
		PollCycleDurationMS:      &Histogram{name: "poll_cycle_duration_ms", buckets: []int64{}},
		PollCycleCount:           &Counter{name: "poll_cycle_count", value: 0},
		WebhookLatencyMS:         &Histogram{name: "webhook_latency_ms", buckets: []int64{}},
		WebhookCount:             &Counter{name: "webhook_count", value: 0},
		WebhookErrors:            &Counter{name: "webhook_errors", value: 0},
		QueueDepth:               &Gauge{name: "queue_depth", value: 0},
		QueueLagSeconds:          &Histogram{name: "queue_lag_seconds", buckets: []int64{}},
		RetryCount:               &Counter{name: "retry_count", value: 0},
		RetryExhausted:           &Counter{name: "retry_exhausted", value: 0},
		PaymentIntentCreated:     &Counter{name: "payment_intent_created", value: 0},
		PaymentIntentTransferred: &Counter{name: "payment_intent_transferred", value: 0},
		PaymentIntentFailed:      &Counter{name: "payment_intent_failed", value: 0},
	}
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
}

// Add increments the counter by the given value.
func (c *Counter) Add(delta int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value += delta
}

// Value returns the current counter value.
func (c *Counter) Value() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(value int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = value
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value++
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.value > 0 {
		g.value--
	}
}

// Value returns the current gauge value.
func (g *Gauge) Value() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.value
}

// Record adds a value to the histogram.
func (h *Histogram) Record(value int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buckets = append(h.buckets, value)
}

// RecordDuration records elapsed time in milliseconds.
func (h *Histogram) RecordDuration(start time.Time) {
	elapsed := time.Since(start).Milliseconds()
	h.Record(elapsed)
}

// Stats returns min, max, avg, and count for the histogram.
func (h *Histogram) Stats() (min, max, avg int64, count int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.buckets) == 0 {
		return 0, 0, 0, 0
	}

	min = h.buckets[0]
	max = h.buckets[0]
	sum := int64(0)

	for _, v := range h.buckets {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}

	avg = sum / int64(len(h.buckets))
	count = len(h.buckets)
	return min, max, avg, count
}

// Reset clears the histogram buckets
func (h *Histogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buckets = []int64{}
}

// Global metrics instance for singleton access
var globalMetrics *Metrics
var metricsOnce sync.Once

// Global returns the global metrics instance.
func Global() *Metrics {
	metricsOnce.Do(func() {
		globalMetrics = NewMetrics()
	})
	return globalMetrics
}
