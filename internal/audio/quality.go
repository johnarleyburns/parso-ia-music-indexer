package audio

import (
	"math"
	"sort"

	"github.com/madelynnblue/go-dsp/fft"
)

const (
	frameSize = 2048

	snrMinDB     = 0.0
	snrMaxDB     = 40.0
	centroidMin  = 0.0
	centroidMax  = 8000.0
	crestMin     = 1.0
	crestMax     = 10.0

	weightSNR      = 0.50
	weightCentroid = 0.30
	weightCrest    = 0.20

	snrKillThreshold = 10.0
)

// QualityChroma holds SNR, spectral centroid, and chroma features computed
// from a single unified FFT pass over 2048-sample frames.
type QualityChroma struct {
	SNR        float64
	CentroidHz float64
	Chroma     []float32
}

// ComputeQualityAndChroma performs a single FFT per 2048-sample frame,
// extracting SNR, spectral centroid, and chroma energies from each spectrum.
// This replaces three separate FFT passes (CalculateSNR, CalculateSpectralCentroid,
// ComputeChromaPool) with one.
func ComputeQualityAndChroma(samples []float64, sampleRate int) QualityChroma {
	result := QualityChroma{
		Chroma: make([]float32, 12),
	}

	numFrames := len(samples) / frameSize
	if numFrames == 0 {
		return result
	}

	var totalSignalPower, totalNoisePower float64
	var totalCentroidSum float64
	totalBins := 0
	validCentroidFrames := 0

	mags := make([]float64, frameSize/2)

	for f := 0; f < numFrames; f++ {
		start := f * frameSize
		block := samples[start : start+frameSize]

		windowed := make([]float64, frameSize)
		for i, v := range block {
			windowed[i] = v * hannWindow(i, frameSize)
		}

		spectrum := fft.FFTReal(windowed)
		halfSize := frameSize / 2

		// Compute magnitude spectrum once
		totalPower := 0.0
		for i := 0; i < halfSize; i++ {
			mag := math.Hypot(real(spectrum[i]), imag(spectrum[i]))
			mags[i] = mag
			totalPower += mag * mag
		}

		// --- SNR (signal/noise estimation via bottom-half-of-spectrum) ---
		if totalPower >= 1e-20 {
			sorted := make([]float64, halfSize)
			copy(sorted, mags)
			sort.Float64s(sorted)

			noiseCount := halfSize / 2
			if noiseCount < 1 {
				noiseCount = 1
			}

			var noisePower float64
			for i := 0; i < noiseCount; i++ {
				noisePower += sorted[i] * sorted[i]
			}

			signalPower := totalPower - noisePower
			totalSignalPower += signalPower
			totalNoisePower += noisePower
			totalBins++
		}

		// --- Spectral Centroid ---
		var weightedFreq, frameMagSum float64
		for i := 0; i < halfSize; i++ {
			mag := mags[i]
			freq := float64(i) * float64(sampleRate) / float64(frameSize)
			weightedFreq += freq * mag
			frameMagSum += mag
		}
		if frameMagSum > 1e-12 {
			totalCentroidSum += weightedFreq / frameMagSum
			validCentroidFrames++
		}

		// --- Chroma ---
		for i := 0; i < halfSize; i++ {
			mag := mags[i]
			freq := float64(i) * float64(sampleRate) / float64(frameSize)
			if freq > 20.0 {
				midiNote := 12*math.Log2(freq/440.0) + 69.0
				noteBin := int(math.Round(midiNote)) % 12
				if noteBin < 0 {
					noteBin += 12
				}
				result.Chroma[noteBin] += float32(mag)
			}
		}
	}

	// Finalize SNR
	if totalBins == 0 || totalNoisePower < 1e-20 {
		result.SNR = snrMaxDB
	} else if totalSignalPower < 1e-20 {
		result.SNR = 0
	} else {
		snr := 10 * math.Log10(totalSignalPower/totalNoisePower)
		if snr < 0 {
			snr = 0
		}
		if snr > snrMaxDB {
			snr = snrMaxDB
		}
		result.SNR = snr
	}

	// Finalize Centroid
	if validCentroidFrames > 0 {
		result.CentroidHz = totalCentroidSum / float64(validCentroidFrames)
	}

	// Finalize Chroma (average per frame)
	for i := 0; i < 12; i++ {
		result.Chroma[i] /= float32(numFrames)
	}

	return result
}

func CalculateSNR(samples []float64) float64 {
	return ComputeQualityAndChroma(samples, 48000).SNR
}

func CalculateSpectralCentroid(samples []float64, sampleRate int) float64 {
	return ComputeQualityAndChroma(samples, sampleRate).CentroidHz
}

func hannWindow(i, n int) float64 {
	return 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
}

func CalculateCrestFactor(samples []float64) float64 {
	if len(samples) == 0 {
		return 1.0
	}

	var peak, sumSq float64
	for _, s := range samples {
		abs := math.Abs(s)
		if abs > peak {
			peak = abs
		}
		sumSq += s * s
	}

	rms := math.Sqrt(sumSq / float64(len(samples)))
	if rms < 1e-12 {
		return 1.0
	}

	return peak / rms
}

func CalculateCompositeScore(snrDB, centroidHz, crestFactor float64) float64 {
	if snrDB < snrKillThreshold {
		return 0.0
	}

	snrNorm := clampNorm(snrDB, snrMinDB, snrMaxDB)
	centroidNorm := clampNorm(centroidHz, centroidMin, centroidMax)
	crestNorm := clampNorm(crestFactor, crestMin, crestMax)

	score := weightSNR*snrNorm + weightCentroid*centroidNorm + weightCrest*crestNorm
	return math.Max(0, math.Min(1.0, score))
}

func clampNorm(value, minVal, maxVal float64) float64 {
	if maxVal <= minVal {
		return 0
	}
	clamped := math.Max(minVal, math.Min(maxVal, value))
	return (clamped - minVal) / (maxVal - minVal)
}
