package audio

import (
	"math"

	"github.com/zrma/go-mfcc/mfcc"
)

func ComputeMFCCPool(samples []float64, sampleRate int) []float32 {
	cfg := mfcc.DefaultConfig()
	cfg.NumCoefficients = 20

	extractor, err := mfcc.NewExtractor(sampleRate, cfg)
	if err != nil {
		return make([]float32, 40)
	}

	rawCoefficients, err := extractor.Calculate(samples)
	if err != nil {
		return make([]float32, 40)
	}

	numFrames := len(rawCoefficients)
	if numFrames == 0 {
		return make([]float32, 40)
	}

	pooled := make([]float32, 40)

	for _, frame := range rawCoefficients {
		for i, coef := range frame {
			if i < 20 {
				pooled[i] += float32(coef)
			}
		}
	}
	for i := 0; i < 20; i++ {
		pooled[i] /= float32(numFrames)
	}

	for _, frame := range rawCoefficients {
		for i, coef := range frame {
			if i < 20 {
				diff := float32(coef) - pooled[i]
				pooled[i+20] += diff * diff
			}
		}
	}
	for i := 20; i < 40; i++ {
		pooled[i] = float32(math.Sqrt(float64(pooled[i] / float32(numFrames))))
	}

	return pooled
}
