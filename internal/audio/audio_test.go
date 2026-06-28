package audio

import (
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"testing"
	"time"
)

func writeWav(path string, samples []float64, sampleRate, bitsPerSample, numChannels int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	bytesPerSample := bitsPerSample / 8
	blockAlign := numChannels * bytesPerSample
	dataSize := len(samples) * blockAlign
	fileSize := 36 + dataSize

	f.Write([]byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(fileSize))
	f.Write([]byte("WAVE"))
	f.Write([]byte("fmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(numChannels))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate*blockAlign))
	binary.Write(f, binary.LittleEndian, uint16(blockAlign))
	binary.Write(f, binary.LittleEndian, uint16(bitsPerSample))
	f.Write([]byte("data"))
	binary.Write(f, binary.LittleEndian, uint32(dataSize))

	maxVal := float64(int(1)<<(bitsPerSample-1) - 1)
	for _, s := range samples {
		clipped := math.Max(-1, math.Min(1, s))
		val := int16(clipped * maxVal)
		binary.Write(f, binary.LittleEndian, val)
	}
	return nil
}

func TestGenerateFixtures(t *testing.T) {
	t.Skip("run manually to generate test fixtures")

	sr := 48000
	dur := 1.0
	n := int(float64(sr) * dur)

	sine := make([]float64, n)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	writeWav("../data/testdata/sine_440hz_1s.wav", sine, sr, 16, 1)

	silence := make([]float64, n)
	writeWav("../data/testdata/silence_1s.wav", silence, sr, 16, 1)

	noise := make([]float64, n)
	for i := range noise {
		noise[i] = (float64(i%9973)/9973.0 - 0.5) * 2.0
	}
	writeWav("../data/testdata/white_noise_1s.wav", noise, sr, 16, 1)
}

func TestDecodeWav(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.wav"
	sr := 48000
	n := 4800
	sine := make([]float64, n)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	if err := writeWav(path, sine, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != n {
		t.Fatalf("expected %d samples, got %d", n, len(samples))
	}
	for i := range samples {
		if math.Abs(samples[i]-sine[i]) > 0.01 {
			t.Errorf("sample[%d]: expected %.4f, got %.4f", i, sine[i], samples[i])
			break
		}
	}
}

func TestMFCCPool(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sine.wav"
	sr := 48000
	dur := 2.0
	n := int(float64(sr) * dur)
	sine := make([]float64, n)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	if err := writeWav(path, sine, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}

	mfcc := ComputeMFCCPool(samples, sr)
	if len(mfcc) != 40 {
		t.Fatalf("expected 40-dim MFCC, got %d", len(mfcc))
	}

	nonZero := false
	for _, v := range mfcc {
		if v != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Error("expected non-zero MFCC values for sine wave")
	}
}

func TestMFCCPoolSilence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/silence.wav"
	sr := 48000
	n := sr * 2
	silence := make([]float64, n)
	if err := writeWav(path, silence, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}

	mfcc := ComputeMFCCPool(samples, sr)
	if len(mfcc) != 40 {
		t.Fatalf("expected 40-dim MFCC, got %d", len(mfcc))
	}
}

func TestChromaPool(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sine.wav"
	sr := 48000
	dur := 2.0
	n := int(float64(sr) * dur)
	sine := make([]float64, n)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	if err := writeWav(path, sine, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}

	chroma := ComputeChromaPool(samples, sr)
	if len(chroma) != 12 {
		t.Fatalf("expected 12-dim chroma, got %d", len(chroma))
	}

	total := float32(0)
	for _, v := range chroma {
		total += v
	}
	if total <= 0 {
		t.Error("expected non-zero total chroma energy")
	}
}

func TestChromaPoolSilence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/silence.wav"
	sr := 48000
	n := sr * 2
	silence := make([]float64, n)
	if err := writeWav(path, silence, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}

	chroma := ComputeChromaPool(samples, sr)
	if len(chroma) != 12 {
		t.Fatalf("expected 12-dim chroma, got %d", len(chroma))
	}

	for i, v := range chroma {
		if v > 0.001 {
			t.Errorf("chroma[%d] should be near zero for silence, got %f", i, v)
		}
	}
}

func TestSNRSine(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sine.wav"
	sr := 48000
	dur := 2.0
	n := int(float64(sr) * dur)
	sine := make([]float64, n)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	if err := writeWav(path, sine, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}

	snr := CalculateSNR(samples)
	if snr < 30 {
		t.Errorf("expected high SNR for clean sine, got %.2f dB", snr)
	}
}

func TestSNRNoise(t *testing.T) {
	dir := t.TempDir()
	sr := 48000
	dur := 2.0
	n := int(float64(sr) * dur)

	clean := make([]float64, n)
	for i := range clean {
		clean[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	cleanPath := dir + "/clean.wav"
	writeWav(cleanPath, clean, sr, 16, 1)

	cleanSamples, _ := DecodeWav(cleanPath)
	cleanSNR := CalculateSNR(cleanSamples)
	if cleanSNR < 30 {
		t.Errorf("clean sine should have high SNR, got %.1f dB", cleanSNR)
	}
}

func TestSNRRange(t *testing.T) {
	sr := 48000
	n := sr
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = (float64(i%7919)/7919.0 - 0.5) * 2.0
	}
	snr := CalculateSNR(samples)
	if snr < 0 || snr > 40 {
		t.Errorf("SNR %.1f out of [0,40] range", snr)
	}
}

func TestSpectralCentroid(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sine.wav"
	sr := 48000
	dur := 2.0
	n := int(float64(sr) * dur)
	sine := make([]float64, n)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	if err := writeWav(path, sine, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}

	centroid := CalculateSpectralCentroid(samples, sr)
	if centroid < 300 || centroid > 1200 {
		t.Errorf("centroid for 440Hz sine should be near 440 Hz, got %.0f Hz", centroid)
	}
}

func TestCrestFactor(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sine.wav"
	sr := 48000
	dur := 2.0
	n := int(float64(sr) * dur)
	sine := make([]float64, n)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 440 * float64(i) / float64(sr))
	}
	if err := writeWav(path, sine, sr, 16, 1); err != nil {
		t.Fatal(err)
	}

	samples, err := DecodeWav(path)
	if err != nil {
		t.Fatal(err)
	}

	cf := CalculateCrestFactor(samples)
	if cf < 1.0 {
		t.Errorf("crest factor should be >= 1.0, got %.2f", cf)
	}
	expected := math.Sqrt2
	if math.Abs(cf-expected) > 0.1 {
		t.Errorf("crest factor for sine wave should be ~%.2f, got %.2f", expected, cf)
	}
}

func TestCrestFactorClipped(t *testing.T) {
	n := 48000
	samples := make([]float64, n)
	for i := range samples {
		s := math.Sin(2 * math.Pi * 440 * float64(i) / 48000)
		if s > 0.5 {
			s = 0.5
		}
		if s < -0.5 {
			s = -0.5
		}
		samples[i] = s
	}

	cf := CalculateCrestFactor(samples)
	if cf > 1.8 {
		t.Errorf("clipped audio should have low crest factor, got %.2f", cf)
	}
}

func TestCompositeScoreHighQuality(t *testing.T) {
	score := CalculateCompositeScore(35, 6000, 8.0)
	if score < 0.7 {
		t.Errorf("expected high score, got %.3f", score)
	}
}

func TestCompositeScoreKillSwitch(t *testing.T) {
	score := CalculateCompositeScore(9.0, 7000, 5.0)
	if score != 0.0 {
		t.Errorf("kill switch should force 0.0, got %.3f", score)
	}
}

func TestCompositeScoreInRange(t *testing.T) {
	testCases := []struct {
		snr, centroid, crest float64
	}{
		{0, 0, 1.0},
		{40, 8000, 10.0},
		{20, 4000, 3.0},
		{15, 2000, 2.0},
	}
	for _, tc := range testCases {
		score := CalculateCompositeScore(tc.snr, tc.centroid, tc.crest)
		if score < 0.0 || score > 1.0 {
			t.Errorf("score %.3f out of [0,1] for snr=%.0f centroid=%.0f crest=%.1f",
				score, tc.snr, tc.centroid, tc.crest)
		}
	}
}

func TestQualityScoreTier(t *testing.T) {
	q := QualityScore{Composite: 0.85}
	if q.Tier() != "high_fidelity" {
		t.Errorf("expected high_fidelity, got %s", q.Tier())
	}
	q = QualityScore{Composite: 0.5}
	if q.Tier() != "historical_vintage" {
		t.Errorf("expected historical_vintage, got %s", q.Tier())
	}
	q = QualityScore{Composite: 0.1}
	if q.Tier() != "unusable" {
		t.Errorf("expected unusable, got %s", q.Tier())
	}
}

func TestDecodeMP3NoHang(t *testing.T) {
	inputs := [][]byte{
		nil,
		{},
		{0xFF},
		{0xFF, 0xFB},
		{0xFF, 0xFB, 0x90, 0x00},
		make([]byte, 10000),
	}
	rng := rand.New(rand.NewSource(0))
	for i := 0; i < 50; i++ {
		size := rng.Intn(50000) + 1
		data := make([]byte, size)
		rng.Read(data)
		inputs = append(inputs, data)
	}

	for _, data := range inputs {
		done := make(chan struct{})
		go func(d []byte) {
			defer close(done)
			DecodeMP3(d, 1800)
		}(data)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("DecodeMP3 hung on %d-byte input", len(data))
		}
	}
}

func TestMaxDecodedSamplesCeiling(t *testing.T) {
	// Linear in input size and independent of any track duration.
	if got := maxDecodedSamples(0); got != 1 {
		t.Errorf("maxDecodedSamples(0) = %d, want 1", got)
	}
	if maxDecodedSamples(2000) != 2*maxDecodedSamples(1000)-1 {
		t.Errorf("ceiling should scale linearly with input size")
	}

	// A ~1.6MB analyzer window (cfg.MaxBytes default) must never trip the ceiling
	// for a realistic low-bitrate track. 128 kbps -> ~100s of audio; decoded as
	// interleaved 16-bit stereo that is ~100 * 44100 * 2 samples. The old ceiling
	// was sized from the full-track metadata duration and tripped here.
	const windowBytes = 1_600_000
	ceiling := maxDecodedSamples(windowBytes)

	realisticSamples := 100 * 44100 * 2
	if realisticSamples >= ceiling {
		t.Errorf("realistic decode (%d samples) must stay under ceiling (%d)", realisticSamples, ceiling)
	}

	// The ceiling must also cover the worst legal MPEG case: 48 kHz at 8 kbps,
	// the maximum samples-per-byte ratio. This is the tight upper bound.
	worstCaseSeconds := float64(windowBytes) * 8 / 8000
	worstCaseSamples := int(worstCaseSeconds * 48000 * 2)
	if worstCaseSamples > ceiling {
		t.Errorf("ceiling (%d) must cover worst-case valid decode (%d samples)", ceiling, worstCaseSamples)
	}
}
