package audio

import (
	"math"
	"sort"

	"github.com/madelynnblue/go-dsp/fft"
)

const (
	qualityFrameSize = 2048

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

func CalculateSNR(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}

	numFrames := len(samples) / qualityFrameSize
	if numFrames < 1 {
		numFrames = 1
	}

	var totalSignalPower, totalNoisePower float64
	totalBins := 0

	for f := 0; f < numFrames; f++ {
		start := f * qualityFrameSize
		end := start + qualityFrameSize
		if end > len(samples) {
			end = len(samples)
		}
		blockLen := end - start
		block := make([]float64, blockLen)
		copy(block, samples[start:end])

		for i := range block {
			block[i] *= hannWindow(i, blockLen)
		}

		nfft := 1
		for nfft < blockLen {
			nfft *= 2
		}
		padded := make([]float64, nfft)
		copy(padded, block)

		spectrum := fft.FFTReal(padded)
		halfSize := nfft / 2

		totalPower := 0.0
		mags := make([]float64, halfSize)
		for i := 0; i < halfSize; i++ {
			mag := math.Hypot(real(spectrum[i]), imag(spectrum[i]))
			mags[i] = mag
			totalPower += mag * mag
		}

		if totalPower < 1e-20 {
			continue
		}

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

	if totalBins == 0 || totalNoisePower < 1e-20 {
		return snrMaxDB
	}

	if totalSignalPower < 1e-20 {
		return 0
	}

	snr := 10 * math.Log10(totalSignalPower / totalNoisePower)
	if snr < 0 {
		return 0
	}
	if snr > snrMaxDB {
		return snrMaxDB
	}
	return snr
}

func hannWindow(i, n int) float64 {
	return 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
}

func CalculateSpectralCentroid(samples []float64, sampleRate int) float64 {
	numFrames := len(samples) / qualityFrameSize
	if numFrames == 0 {
		return 0
	}

	var totalCentroid float64
	validFrames := 0

	for f := 0; f < numFrames; f++ {
		block := make([]float64, qualityFrameSize)
		copy(block, samples[f*qualityFrameSize:(f+1)*qualityFrameSize])

		for i := range block {
			block[i] *= hannWindow(i, qualityFrameSize)
		}

		spectrum := fft.FFTReal(block)
		halfSize := qualityFrameSize / 2

		var weightedSum, magSum float64
		for i := 0; i < halfSize; i++ {
			mag := math.Hypot(real(spectrum[i]), imag(spectrum[i]))
			freq := float64(i) * float64(sampleRate) / float64(qualityFrameSize)
			weightedSum += freq * mag
			magSum += mag
		}

		if magSum > 1e-12 {
			totalCentroid += weightedSum / magSum
			validFrames++
		}
	}

	if validFrames == 0 {
		return 0
	}

	return totalCentroid / float64(validFrames)
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
