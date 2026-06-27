package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/hajimehoshi/go-mp3"
)

func findAllFrameSyncOffsets(data []byte, maxOffsets int) []int {
	var offsets []int
	for i := 0; i+3 < len(data) && len(offsets) < maxOffsets; i++ {
		if data[i] != 0xFF {
			continue
		}
		if data[i+1]&0xE0 != 0xE0 {
			continue
		}
		h := binary.BigEndian.Uint32(data[i : i+4])
		if !headerLooksValid(h) {
			continue
		}
		offsets = append(offsets, i)
	}
	return offsets
}

func headerLooksValid(h uint32) bool {
	if h&0xFFE00000 != 0xFFE00000 {
		return false
	}
	version := (h >> 19) & 0x3
	if version == 1 {
		return false
	}
	layer := (h >> 17) & 0x3
	if layer == 0 {
		return false
	}
	bitrateIndex := (h >> 12) & 0xF
	if bitrateIndex == 0xF || bitrateIndex == 0 {
		return false
	}
	samplingFreq := (h >> 10) & 0x3
	if samplingFreq == 3 {
		return false
	}
	emphasis := h & 0x3
	if emphasis == 2 {
		return false
	}
	return true
}

func isRecoverableMPEGError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	recoverable := []string{
		"is_pos was too big",
		"invalid index",
		"invalid side info",
		"framesize =",
		"invalid number of bits",
	}
	for _, p := range recoverable {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func decodeMP3FromData(data []byte, maxDurationSec float64) (samples []float64, sampleRate int, err error) {
	reader := bytes.NewReader(data)
	decoder, decErr := mp3.NewDecoder(reader)
	if decErr != nil {
		return nil, 0, decErr
	}

	sampleRate = decoder.SampleRate()

	estimatedSamples := len(data) * 5
	samples = make([]float64, 0, estimatedSamples)

	maxIters := int(maxDurationSec*float64(sampleRate)*2/2048) + 1
	iterCount := 0

	buf := make([]byte, 4096)
	for {
		if iterCount >= maxIters {
			return nil, 0, fmt.Errorf("mp3 decode exceeded duration ceiling: %d iterations (max %.0fs)", iterCount, maxDurationSec)
		}
		iterCount++

		n, readErr := decoder.Read(buf)
		if n > 0 {
			for i := 0; i < n; i += 2 {
				left := int16(buf[i]) | int16(buf[i+1])<<8
				samples = append(samples, float64(left)/32768.0)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, readErr
		}
		if n == 0 {
			return nil, 0, fmt.Errorf("mp3 decoder stalled: 0 bytes read without error or EOF")
		}
	}

	if len(samples) == 0 {
		return nil, 0, fmt.Errorf("mp3 produced zero samples")
	}

	return samples, sampleRate, nil
}

const maxFrameSyncAttempts = 3

func DecodeMP3(data []byte, metadataDurationSec float64) (samples []float64, sampleRate int, err error) {
	defer func() {
		if r := recover(); r != nil {
			samples = nil
			sampleRate = 0
			err = fmt.Errorf("mp3 decode panic: %v", r)
		}
	}()

	if len(data) < 4 {
		return nil, 0, fmt.Errorf("mp3 data too short (%d bytes)", len(data))
	}

	ceilingSec := metadataDurationSec
	if ceilingSec > 10800 {
		ceilingSec = 10800
	}

	offsets := findAllFrameSyncOffsets(data, maxFrameSyncAttempts+1)
	if len(offsets) == 0 {
		samples, sampleRate, err = decodeMP3FromData(data, ceilingSec)
		if err != nil {
			return nil, 0, fmt.Errorf("mp3 init: %w", err)
		}
		return samples, sampleRate, nil
	}

	var lastErr error
	for _, offset := range offsets {
		trimmed := data[offset:]
		if len(trimmed) < 4 {
			continue
		}
		samples, sampleRate, err = decodeMP3FromData(trimmed, ceilingSec)
		if err == nil {
			return samples, sampleRate, nil
		}
		if !isRecoverableMPEGError(err) {
			return nil, 0, fmt.Errorf("mp3 init: %w", err)
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, 0, fmt.Errorf("mp3 init: %w", lastErr)
	}
	return nil, 0, fmt.Errorf("mp3 init: no valid frames found")
}
