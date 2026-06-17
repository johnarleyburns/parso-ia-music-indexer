package audio

import (
	"bytes"
	"fmt"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

func DecodeMP3(data []byte) ([]float64, int, error) {
	reader := bytes.NewReader(data)
	decoder, err := mp3.NewDecoder(reader)
	if err != nil {
		return nil, 0, fmt.Errorf("mp3 init: %w", err)
	}

	sampleRate := decoder.SampleRate()

	var samples []float64
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
