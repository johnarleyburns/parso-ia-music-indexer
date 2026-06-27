package tui

import "testing"

func TestDrainActivityEventsCoalesces(t *testing.T) {
	ch := make(chan ActivityEvent, 64)
	for i := 0; i < 50; i++ {
		ch <- ActivityEvent{Count: i}
	}

	msg := drainActivityEvents(ch)()
	batch, ok := msg.(eventBatchMsg)
	if !ok {
		t.Fatalf("expected eventBatchMsg, got %T", msg)
	}
	if len(batch.Events) != 50 {
		t.Fatalf("expected 50 coalesced events, got %d", len(batch.Events))
	}
}

func TestDrainActivityEventsRespectsMax(t *testing.T) {
	ch := make(chan ActivityEvent, eventBatchMax*2)
	for i := 0; i < eventBatchMax+100; i++ {
		ch <- ActivityEvent{Count: i}
	}

	batch := drainActivityEvents(ch)().(eventBatchMsg)
	if len(batch.Events) != eventBatchMax {
		t.Fatalf("expected batch capped at %d, got %d", eventBatchMax, len(batch.Events))
	}
}

func TestDrainActivityEventsClosedChannel(t *testing.T) {
	ch := make(chan ActivityEvent)
	close(ch)
	if msg := drainActivityEvents(ch)(); msg != nil {
		t.Fatalf("expected nil on closed channel, got %T", msg)
	}
}
