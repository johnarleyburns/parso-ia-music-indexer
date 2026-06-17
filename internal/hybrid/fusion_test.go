package hybrid

import (
	"math"
	"testing"
)

func TestFuseFeaturesCorrectDimensions(t *testing.T) {
	clap := make([]float32, DimCLAP)
	mfcc := make([]float32, DimMFCC)
	chroma := make([]float32, DimChroma)

	result := FuseFeatures(clap, mfcc, chroma)
	if len(result) != DimHybrid {
		t.Fatalf("expected %d dimensions, got %d", DimHybrid, len(result))
	}
}

func TestFuseFeaturesZeroInput(t *testing.T) {
	clap := make([]float32, DimCLAP)
	mfcc := make([]float32, DimMFCC)
	chroma := make([]float32, DimChroma)

	result := FuseFeatures(clap, mfcc, chroma)
	for i, v := range result {
		if v != 0 {
			t.Fatalf("expected zero at position %d, got %f", i, v)
		}
	}
}

func TestFuseFeaturesWeightsApplied(t *testing.T) {
	clap := make([]float32, DimCLAP)
	for i := range clap {
		clap[i] = 1.0
	}
	mfcc := make([]float32, DimMFCC)
	for i := range mfcc {
		mfcc[i] = 1.0
	}
	chroma := make([]float32, DimChroma)
	for i := range chroma {
		chroma[i] = 1.0
	}

	result := FuseFeatures(clap, mfcc, chroma)

	for i := 0; i < DimCLAP; i++ {
		if math.Abs(float64(result[i]-WeightCLAP)) > 1e-6 {
			t.Fatalf("clap position %d: expected %f, got %f", i, WeightCLAP, result[i])
		}
	}
	off := DimCLAP
	for i := 0; i < DimMFCC; i++ {
		if math.Abs(float64(result[off+i]-WeightMFCC)) > 1e-6 {
			t.Fatalf("mfcc position %d: expected %f, got %f", i, WeightMFCC, result[off+i])
		}
	}
	off += DimMFCC
	for i := 0; i < DimChroma; i++ {
		if math.Abs(float64(result[off+i]-WeightChroma)) > 1e-6 {
			t.Fatalf("chroma position %d: expected %f, got %f", i, WeightChroma, result[off+i])
		}
	}
}

func TestFuseFeaturesKnownValues(t *testing.T) {
	clap := make([]float32, DimCLAP)
	clap[0] = 0.5
	clap[511] = -0.3

	mfcc := make([]float32, DimMFCC)
	mfcc[0] = 2.0
	mfcc[39] = -1.0

	chroma := make([]float32, DimChroma)
	chroma[0] = 0.8
	chroma[11] = -0.5

	result := FuseFeatures(clap, mfcc, chroma)

	if math.Abs(float64(result[0]-0.5*WeightCLAP)) > 1e-6 {
		t.Fatalf("clap[0]: expected %f, got %f", 0.5*WeightCLAP, result[0])
	}
	if math.Abs(float64(result[511]-(-0.3)*WeightCLAP)) > 1e-6 {
		t.Fatalf("clap[511]: expected %f, got %f", -0.3*WeightCLAP, result[511])
	}

	mfccStart := DimCLAP
	if math.Abs(float64(result[mfccStart]-2.0*WeightMFCC)) > 1e-6 {
		t.Fatalf("mfcc[0]: expected %f, got %f", 2.0*WeightMFCC, result[mfccStart])
	}
	if math.Abs(float64(result[mfccStart+39]-(-1.0)*WeightMFCC)) > 1e-6 {
		t.Fatalf("mfcc[39]: expected %f, got %f", -1.0*WeightMFCC, result[mfccStart+39])
	}

	chromaStart := DimCLAP + DimMFCC
	if math.Abs(float64(result[chromaStart]-0.8*WeightChroma)) > 1e-6 {
		t.Fatalf("chroma[0]: expected %f, got %f", 0.8*WeightChroma, result[chromaStart])
	}
	if math.Abs(float64(result[chromaStart+11]-(-0.5)*WeightChroma)) > 1e-6 {
		t.Fatalf("chroma[11]: expected %f, got %f", -0.5*WeightChroma, result[chromaStart+11])
	}
}

func TestFuseFeaturesPanicsOnBadLength(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for wrong clap length")
		}
	}()
	FuseFeatures(make([]float32, 10), make([]float32, DimMFCC), make([]float32, DimChroma))
}
