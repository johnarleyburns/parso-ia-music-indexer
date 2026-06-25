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

type durationRecord struct {
	ts time.Time
	d  time.Duration
}

type Metrics struct {
	mu sync.Mutex

	apiCalls             []time.Time
	byteTransfers        []byteRecord
	resolverCompletions  []time.Time
	analyzerCompletions  []time.Time
	cleanerCompletions   []time.Time
	enhancerCompletions  []time.Time

	networkTimes    []durationRecord
	clapTimes       []durationRecord
	processingTimes []durationRecord
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

func (m *Metrics) RecordEnhancerCompletion() {
	m.mu.Lock()
	m.enhancerCompletions = append(m.enhancerCompletions, time.Now())
	m.mu.Unlock()
}

func (m *Metrics) RecordNetworkTime(d time.Duration) {
	m.mu.Lock()
	m.networkTimes = append(m.networkTimes, durationRecord{ts: time.Now(), d: d})
	m.mu.Unlock()
}

func (m *Metrics) RecordCLAPTime(d time.Duration) {
	m.mu.Lock()
	m.clapTimes = append(m.clapTimes, durationRecord{ts: time.Now(), d: d})
	m.mu.Unlock()
}

func (m *Metrics) RecordProcessingTime(d time.Duration) {
	m.mu.Lock()
	m.processingTimes = append(m.processingTimes, durationRecord{ts: time.Now(), d: d})
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

	o := 0
	for o < len(m.enhancerCompletions) && m.enhancerCompletions[o].Before(cutoff) {
		o++
	}
	m.enhancerCompletions = m.enhancerCompletions[o:]

	m.networkTimes = pruneDurations(m.networkTimes, cutoff)
	m.clapTimes = pruneDurations(m.clapTimes, cutoff)
	m.processingTimes = pruneDurations(m.processingTimes, cutoff)
}

func pruneDurations(recs []durationRecord, cutoff time.Time) []durationRecord {
	i := 0
	for i < len(recs) && recs[i].ts.Before(cutoff) {
		i++
	}
	return recs[i:]
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

func (m *Metrics) EnhancerRate() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)
	if len(m.enhancerCompletions) == 0 {
		return 0
	}
	elapsed := now.Sub(m.enhancerCompletions[0]).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return float64(len(m.enhancerCompletions)) / elapsed
}

// AnalyzerTimeBreakdown returns the share of analyzer time spent on network
// (streaming audio), CLAP (sidecar embedding), and local timbre processing
// (decode + quality + feature extraction) over the metrics window, as
// percentages summing to ~100. hasData is false when no analyzer work has been
// recorded in the window.
func (m *Metrics) AnalyzerTimeBreakdown() (netPct, clapPct, procPct float64, hasData bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)

	var net, clap, proc time.Duration
	for _, r := range m.networkTimes {
		net += r.d
	}
	for _, r := range m.clapTimes {
		clap += r.d
	}
	for _, r := range m.processingTimes {
		proc += r.d
	}

	total := net + clap + proc
	if total <= 0 {
		return 0, 0, 0, false
	}
	tf := float64(total)
	return float64(net) / tf * 100, float64(clap) / tf * 100, float64(proc) / tf * 100, true
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
