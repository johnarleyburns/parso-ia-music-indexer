package audio

import (
	"math"
	"math/rand"
	"testing"
)

func generateSamples(durationSec, sampleRate int) []float64 {
	n := durationSec * sampleRate
	rng := rand.New(rand.NewSource(42))
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = (rng.Float64() - 0.5) * 2.0
		samples[i] += 0.3 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate))
		samples[i] += 0.2 * math.Sin(2*math.Pi*880*float64(i)/float64(sampleRate))
	}
	return samples
}

var benchSamples30s = generateSamples(30, 44100)
var benchSamples15s = generateSamples(15, 44100)
var benchSamples10s = generateSamples(10, 44100)

var benchResultQC QualityChroma
var benchResultMFCC []float32
var benchResultCF float64

func BenchmarkComputeQualityAndChroma_30s(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchResultQC = ComputeQualityAndChroma(benchSamples30s, 44100)
	}
}

func BenchmarkComputeQualityAndChroma_15s(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchResultQC = ComputeQualityAndChroma(benchSamples15s, 44100)
	}
}

func BenchmarkComputeMFCCPool_15s(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchResultMFCC = ComputeMFCCPool(benchSamples15s, 44100)
	}
}

func BenchmarkCalculateCrestFactor_30s(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchResultCF = CalculateCrestFactor(benchSamples30s)
	}
}

func BenchmarkCalculateSNR_30s(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchResultCF = CalculateSNR(benchSamples30s)
	}
}

func BenchmarkCalculateSpectralCentroid_30s(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchResultCF = CalculateSpectralCentroid(benchSamples30s, 44100)
	}
}

func BenchmarkComputeChromaPool_30s(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchResultMFCC = ComputeChromaPool(benchSamples30s, 44100)
	}
}
