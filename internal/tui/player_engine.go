package tui

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

type readSeekCloser struct {
	*bytes.Reader
}

func (r *readSeekCloser) Close() error { return nil }

type PlayerEngine struct {
	streamer    beep.StreamSeekCloser
	ctrl        *beep.Ctrl
	volume      *effects.Volume
	format      beep.Format
	sampleRate  beep.SampleRate
	mu          sync.Mutex
	speakerInit bool
}

func NewPlayerEngine() *PlayerEngine {
	return &PlayerEngine{}
}

func (e *PlayerEngine) LoadAndPlay(mp3Data []byte, onDone func()) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stopLocked()

	reader := &readSeekCloser{bytes.NewReader(mp3Data)}
	streamer, format, err := mp3.Decode(reader)
	if err != nil {
		return fmt.Errorf("decode mp3: %w", err)
	}

	if !e.speakerInit {
		bufSize := format.SampleRate.N(time.Second / 10)
		if err := speaker.Init(format.SampleRate, bufSize); err != nil {
			streamer.Close()
			return fmt.Errorf("init speaker: %w", err)
		}
		e.sampleRate = format.SampleRate
		e.speakerInit = true
	}

	var s beep.Streamer = streamer
	if format.SampleRate != e.sampleRate {
		s = beep.Resample(4, format.SampleRate, e.sampleRate, streamer)
	}

	e.streamer = streamer
	e.format = format
	e.ctrl = &beep.Ctrl{Streamer: s, Paused: false}
	e.volume = &effects.Volume{
		Streamer: e.ctrl,
		Base:     2,
		Volume:   0,
		Silent:   false,
	}

	speaker.Play(beep.Seq(e.volume, beep.Callback(func() {
		if onDone != nil {
			onDone()
		}
	})))

	return nil
}

func (e *PlayerEngine) stopLocked() {
	if e.streamer != nil {
		speaker.Clear()
		e.streamer.Close()
		e.streamer = nil
		e.ctrl = nil
		e.volume = nil
	}
}

func (e *PlayerEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
}

func (e *PlayerEngine) TogglePause() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctrl == nil {
		return false
	}
	speaker.Lock()
	e.ctrl.Paused = !e.ctrl.Paused
	paused := e.ctrl.Paused
	speaker.Unlock()
	return paused
}

func (e *PlayerEngine) SetVolume(vol float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.volume == nil {
		return
	}
	speaker.Lock()
	if vol <= 0 {
		e.volume.Silent = true
	} else {
		e.volume.Silent = false
		e.volume.Volume = (vol - 1.0) * 5
	}
	speaker.Unlock()
}

func (e *PlayerEngine) Seek(delta time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.streamer == nil {
		return
	}
	speaker.Lock()
	pos := e.streamer.Position()
	length := e.streamer.Len()
	deltaSamples := e.format.SampleRate.N(delta)
	newPos := pos + deltaSamples
	if newPos < 0 {
		newPos = 0
	}
	if newPos >= length {
		newPos = length - 1
	}
	if newPos >= 0 {
		_ = e.streamer.Seek(newPos)
	}
	speaker.Unlock()
}

func (e *PlayerEngine) Position() (elapsed, total time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.streamer == nil {
		return 0, 0
	}
	speaker.Lock()
	pos := e.streamer.Position()
	length := e.streamer.Len()
	speaker.Unlock()
	elapsed = e.format.SampleRate.D(pos)
	total = e.format.SampleRate.D(length)
	return
}

func (e *PlayerEngine) HasTrack() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.streamer != nil
}

func (e *PlayerEngine) Close() {
	e.Stop()
}
