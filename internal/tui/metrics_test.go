package tui

import (
	"math"
	"testing"
	"time"
)

func TestAnalyzerTimeBreakdownEmpty(t *testing.T) {
	m := NewMetrics()
	if _, _, _, hasData := m.AnalyzerTimeBreakdown(); hasData {
		t.Fatal("expected hasData=false with no records")
	}
}

func TestAnalyzerTimeBreakdownPercentages(t *testing.T) {
	m := NewMetrics()
	m.RecordNetworkTime(1 * time.Second)
	m.RecordNetworkTime(1 * time.Second)
	m.RecordCLAPTime(6 * time.Second)
	m.RecordProcessingTime(2 * time.Second)

	net, clap, proc, hasData := m.AnalyzerTimeBreakdown()
	if !hasData {
		t.Fatal("expected hasData=true after recording times")
	}
	if math.Abs(net-20) > 0.01 {
		t.Errorf("network: expected 20%%, got %.2f", net)
	}
	if math.Abs(clap-60) > 0.01 {
		t.Errorf("clap: expected 60%%, got %.2f", clap)
	}
	if math.Abs(proc-20) > 0.01 {
		t.Errorf("processing: expected 20%%, got %.2f", proc)
	}
	if math.Abs((net+clap+proc)-100) > 0.01 {
		t.Errorf("percentages should sum to 100, got %.2f", net+clap+proc)
	}
}
