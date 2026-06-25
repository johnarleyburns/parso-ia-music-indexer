package audio

import (
	"bytes"
	"fmt"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

func DecodeMP3(data []byte) (samples []float64, sampleRate int, err error) {
	defer func() {
		if r := recover(); r != nil {
			samples = nil
			sampleRate = 0
			err = fmt.Errorf("mp3 decode panic: %v", r)
		}
	}()

	reader := bytes.NewReader(data)
	decoder, decErr := mp3.NewDecoder(reader)
	if decErr != nil {
		return nil, 0, fmt.Errorf("mp3 init: %w", decErr)
	}

	sampleRate = decoder.SampleRate()

	estimatedSamples := len(data) * 5
	samples = make([]float64, 0, estimatedSamples)

	buf := make([]byte, 4096)
	for {
		n, err := decoder.Read(buf)
		if n > 0 {
			for i := 0; i < n; i += 2 {
				left := int16(buf[i]) | int16(buf[i+1])<<8
				samples = append(samples, float64(left)/32768.0)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("mp3 read: %w", err)
		}
	}

	if len(samples) == 0 {
		return nil, 0, fmt.Errorf("mp3 produced zero samples")
	}

	return samples, sampleRate, nil
}
