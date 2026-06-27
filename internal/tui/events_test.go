package tui

import (
	"testing"
	"time"
)

func TestEmitDropsNewestWhenFull(t *testing.T) {
	ch := make(chan ActivityEvent, 2)
	Emit(ch, ActivityEvent{Message: "a"})
	Emit(ch, ActivityEvent{Message: "b"})
	Emit(ch, ActivityEvent{Message: "c"}) // buffer full: must not block, must drop "c"

	if len(ch) != 2 {
		t.Fatalf("expected 2 buffered events, got %d", len(ch))
	}
	if got := (<-ch).Message; got != "a" {
		t.Fatalf("expected oldest 'a' retained, got %q", got)
	}
	if got := (<-ch).Message; got != "b" {
		t.Fatalf("expected 'b' retained, got %q", got)
	}
}

func TestEmitNeverBlocks(t *testing.T) {
	ch := make(chan ActivityEvent, 1)
	Emit(ch, ActivityEvent{Message: "fill"})

	done := make(chan struct{})
	go func() {
		Emit(ch, ActivityEvent{Message: "overflow"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit blocked on a full channel")
	}
}

func TestStartEventDecouplerForwards(t *testing.T) {
	in := make(chan ActivityEvent, 8)
	out := StartEventDecoupler(in)

	for i := 0; i < 5; i++ {
		in <- ActivityEvent{Count: i}
	}

	deadline := time.After(2 * time.Second)
	seen := 0
	for seen < 5 {
		select {
		case _, ok := <-out:
			if !ok {
				t.Fatalf("decoupler closed early after %d events", seen)
			}
			seen++
		case <-deadline:
			t.Fatalf("decoupler only forwarded %d/5 events", seen)
		}
	}

	close(in)
}
