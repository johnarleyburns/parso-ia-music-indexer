package audio

import (
	"math"

	"github.com/madelynnblue/go-dsp/fft"
)

const chromaBlockSize = 2048

func ComputeChromaPool(samples []float64) []float32 {
	chroma := make([]float32, 12)
	numBlocks := len(samples) / chromaBlockSize
	if numBlocks == 0 {
		return chroma
	}

	for b := 0; b < numBlocks; b++ {
		block := make([]float64, chromaBlockSize)
		copy(block, samples[b*chromaBlockSize:(b+1)*chromaBlockSize])

		for i := range block {
			block[i] *= hannWindow(i, chromaBlockSize)
		}

		fftOutput := fft.FFTReal(block)

		for i := 0; i < chromaBlockSize/2; i++ {
			magnitude := math.Hypot(real(fftOutput[i]), imag(fftOutput[i]))
			freq := float64(i) * 48000.0 / float64(chromaBlockSize)
			if freq > 20.0 {
				midiNote := 12*math.Log2(freq/440.0) + 69.0
				noteBin := int(math.Round(midiNote)) % 12
				if noteBin < 0 {
					noteBin += 12
				}
				chroma[noteBin] += float32(magnitude)
			}
		}
	}

	for i := 0; i < 12; i++ {
		chroma[i] /= float32(numBlocks)
	}

	return chroma
}
