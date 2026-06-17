package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func DecodeWav(filePath string) ([]float64, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open wav: %w", err)
	}
	defer f.Close()

	var riff [4]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, fmt.Errorf("read riff header: %w", err)
	}
	if string(riff[:]) != "RIFF" {
		return nil, fmt.Errorf("not a RIFF file")
	}

	var fileSize uint32
	if err := binary.Read(f, binary.LittleEndian, &fileSize); err != nil {
		return nil, fmt.Errorf("read file size: %w", err)
	}

	var wave [4]byte
	if _, err := io.ReadFull(f, wave[:]); err != nil {
		return nil, fmt.Errorf("read wave header: %w", err)
	}
	if string(wave[:]) != "WAVE" {
		return nil, fmt.Errorf("not a WAVE file")
	}

	var fmtChunk [4]byte
	if _, err := io.ReadFull(f, fmtChunk[:]); err != nil {
		return nil, fmt.Errorf("read fmt chunk: %w", err)
	}
	if string(fmtChunk[:]) != "fmt " {
		return nil, fmt.Errorf("expected fmt chunk")
	}

	var fmtSize uint32
	if err := binary.Read(f, binary.LittleEndian, &fmtSize); err != nil {
		return nil, fmt.Errorf("read fmt size: %w", err)
	}

	var audioFormat uint16
	if err := binary.Read(f, binary.LittleEndian, &audioFormat); err != nil {
		return nil, fmt.Errorf("read audio format: %w", err)
	}
	if audioFormat != 1 {
		return nil, fmt.Errorf("only PCM format supported, got %d", audioFormat)
	}

	var numChannels uint16
	if err := binary.Read(f, binary.LittleEndian, &numChannels); err != nil {
		return nil, fmt.Errorf("read num channels: %w", err)
	}

	var sampleRate uint32
	if err := binary.Read(f, binary.LittleEndian, &sampleRate); err != nil {
		return nil, fmt.Errorf("read sample rate: %w", err)
	}

	var byteRate uint32
	if err := binary.Read(f, binary.LittleEndian, &byteRate); err != nil {
		return nil, fmt.Errorf("read byte rate: %w", err)
	}

	var blockAlign uint16
	if err := binary.Read(f, binary.LittleEndian, &blockAlign); err != nil {
		return nil, fmt.Errorf("read block align: %w", err)
	}

	var bitsPerSample uint16
	if err := binary.Read(f, binary.LittleEndian, &bitsPerSample); err != nil {
		return nil, fmt.Errorf("read bits per sample: %w", err)
	}

	_ = byteRate
	_ = blockAlign

	if fmtSize > 16 {
		skip := make([]byte, fmtSize-16)
		if _, err := io.ReadFull(f, skip); err != nil {
			return nil, fmt.Errorf("skip extra fmt bytes: %w", err)
		}
	}

	for {
		var chunkID [4]byte
		if _, err := io.ReadFull(f, chunkID[:]); err != nil {
			return nil, fmt.Errorf("find data chunk: %w", err)
		}

		var chunkSize uint32
		if err := binary.Read(f, binary.LittleEndian, &chunkSize); err != nil {
			return nil, fmt.Errorf("read chunk size: %w", err)
		}

		if string(chunkID[:]) == "data" {
			totalSamples := int(chunkSize) / int(bitsPerSample/8)
			samples := make([]float64, totalSamples/int(numChannels))

			bytesPerSample := int(bitsPerSample / 8)
			buf := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, buf); err != nil {
				return nil, fmt.Errorf("read data: %w", err)
			}

			for i := 0; i < len(samples); i++ {
				offset := i * int(numChannels) * bytesPerSample
				var sum int32
				for ch := 0; ch < int(numChannels); ch++ {
					sampleOffset := offset + ch*bytesPerSample
					switch bitsPerSample {
					case 8:
						sum += int32(buf[sampleOffset]) - 128
					case 16:
						val := int16(binary.LittleEndian.Uint16(buf[sampleOffset:]))
						sum += int32(val)
					case 24:
						val := int32(buf[sampleOffset]) | int32(buf[sampleOffset+1])<<8 | int32(buf[sampleOffset+2])<<16
						if val&0x800000 != 0 {
							val |= ^0xFFFFFF
						}
						sum += val
					case 32:
						val := int32(binary.LittleEndian.Uint32(buf[sampleOffset:]))
						sum += val
					default:
						val := int16(binary.LittleEndian.Uint16(buf[sampleOffset:]))
						sum += int32(val)
					}
				}
				avg := float64(sum) / float64(numChannels)
				maxVal := float64(int(1) << (bitsPerSample - 1))
				samples[i] = avg / maxVal
			}
			return samples, nil
		}

		skip := make([]byte, chunkSize)
		if _, err := io.ReadFull(f, skip); err != nil {
			return nil, fmt.Errorf("skip chunk: %w", err)
		}
	}
}
