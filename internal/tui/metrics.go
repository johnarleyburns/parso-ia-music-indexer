package tui

import (
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	metricsWindow   = 60 * time.Second
	IABandwidthLimit = 1_048_576
	IAAPIRateLimit   = 100.0 / 60.0
)

type byteRecord struct {
	ts    time.Time
	bytes int64
}

type Metrics struct {
	mu sync.Mutex

	apiCalls             []time.Time
	byteTransfers        []byteRecord
	resolverCompletions  []time.Time
	analyzerCompletions  []time.Time
	cleanerCompletions   []time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) RecordAPICall() {
	m.mu.Lock()
	m.apiCalls = append(m.apiCalls, time.Now())
	m.mu.Unlock()
}

func (m *Metrics) RecordBytes(n int64) {
	m.mu.Lock()
	m.byteTransfers = append(m.byteTransfers, byteRecord{ts: time.Now(), bytes: n})
	m.mu.Unlock()
}

func (m *Metrics) RecordResolverCompletion() {
	m.mu.Lock()
	m.resolverCompletions = append(m.resolverCompletions, time.Now())
	m.mu.Unlock()
}

func (m *Metrics) RecordAnalyzerCompletion() {
	m.mu.Lock()
	m.analyzerCompletions = append(m.analyzerCompletions, time.Now())
	m.mu.Unlock()
}

func (m *Metrics) RecordCleanerCompletion() {
	m.mu.Lock()
	m.cleanerCompletions = append(m.cleanerCompletions, time.Now())
	m.mu.Unlock()
}

func (m *Metrics) prune(now time.Time) {
	cutoff := now.Add(-metricsWindow)

	i := 0
	for i < len(m.apiCalls) && m.apiCalls[i].Before(cutoff) {
		i++
	}
	m.apiCalls = m.apiCalls[i:]

	j := 0
	for j < len(m.byteTransfers) && m.byteTransfers[j].ts.Before(cutoff) {
		j++
	}
	m.byteTransfers = m.byteTransfers[j:]

	k := 0
	for k < len(m.resolverCompletions) && m.resolverCompletions[k].Before(cutoff) {
		k++
	}
	m.resolverCompletions = m.resolverCompletions[k:]

	l := 0
	for l < len(m.analyzerCompletions) && m.analyzerCompletions[l].Before(cutoff) {
		l++
	}
	m.analyzerCompletions = m.analyzerCompletions[l:]

	n := 0
	for n < len(m.cleanerCompletions) && m.cleanerCompletions[n].Before(cutoff) {
		n++
	}
	m.cleanerCompletions = m.cleanerCompletions[n:]
}

func (m *Metrics) APIRate() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)
	if len(m.apiCalls) == 0 {
		return 0
	}
	elapsed := now.Sub(m.apiCalls[0]).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return float64(len(m.apiCalls)) / elapsed
}

func (m *Metrics) Bandwidth() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)
	if len(m.byteTransfers) == 0 {
		return 0
	}
	var total int64
	for _, r := range m.byteTransfers {
		total += r.bytes
	}
	elapsed := now.Sub(m.byteTransfers[0].ts).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return float64(total) / elapsed
}

func (m *Metrics) ResolverRate() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)
	if len(m.resolverCompletions) == 0 {
		return 0
	}
	elapsed := now.Sub(m.resolverCompletions[0]).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return float64(len(m.resolverCompletions)) / elapsed
}

func (m *Metrics) AnalyzerRate() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)
	if len(m.analyzerCompletions) == 0 {
		return 0
	}
	elapsed := now.Sub(m.analyzerCompletions[0]).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return float64(len(m.analyzerCompletions)) / elapsed
}

func (m *Metrics) CleanerRate() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)
	if len(m.cleanerCompletions) == 0 {
		return 0
	}
	elapsed := now.Sub(m.cleanerCompletions[0]).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return float64(len(m.cleanerCompletions)) / elapsed
}

type InstrumentedTransport struct {
	Base    http.RoundTripper
	metrics *Metrics
}

func NewInstrumentedTransport(metrics *Metrics) *InstrumentedTransport {
	return &InstrumentedTransport{
		Base:    http.DefaultTransport,
		metrics: metrics,
	}
}

func (t *InstrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.metrics.RecordAPICall()
	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	resp.Body = &countingReadCloser{ReadCloser: resp.Body, metrics: t.metrics}
	return resp, nil
}

type countingReadCloser struct {
	io.ReadCloser
	metrics *Metrics
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 {
		c.metrics.RecordBytes(int64(n))
	}
	return n, err
}
