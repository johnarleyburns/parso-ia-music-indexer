package hybrid

const (
	WeightCLAP   = 0.60
	WeightMFCC   = 0.25
	WeightChroma = 0.15
)

const (
	DimCLAP   = 512
	DimMFCC   = 40
	DimChroma = 12
	DimHybrid = DimCLAP + DimMFCC + DimChroma
)

func FuseFeatures(clap, mfcc, chroma []float32) []float32 {
	if len(clap) != DimCLAP {
		panic("clap vector must be 512-dimensional")
	}
	if len(mfcc) != DimMFCC {
		panic("mfcc vector must be 40-dimensional")
	}
	if len(chroma) != DimChroma {
		panic("chroma vector must be 12-dimensional")
	}

	hybrid := make([]float32, 0, DimHybrid)

	for _, v := range clap {
		hybrid = append(hybrid, v*WeightCLAP)
	}
	for _, v := range mfcc {
		hybrid = append(hybrid, v*WeightMFCC)
	}
	for _, v := range chroma {
		hybrid = append(hybrid, v*WeightChroma)
	}

	return hybrid
}
