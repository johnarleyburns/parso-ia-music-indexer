package audio

func ComputeChromaPool(samples []float64, sampleRate int) []float32 {
	return ComputeQualityAndChroma(samples, sampleRate).Chroma
}
