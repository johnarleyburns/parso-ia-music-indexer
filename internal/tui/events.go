package tui

import "time"

type EventType string

const (
	EventQueueAdded           EventType = "queue_added"
	EventCoordStarted         EventType = "coordinator_started"
	EventCoordStopped         EventType = "coordinator_stopped"
	EventCoordProgress        EventType = "coordinator_progress"
	EventCollectionStarted    EventType = "collection_started"
	EventCollectionProgress   EventType = "collection_progress"
	EventCollectionCompleted  EventType = "collection_completed"
	EventCollectionFailed     EventType = "collection_failed"
	EventWorkerStarted        EventType = "worker_started"
	EventWorkerStopped        EventType = "worker_stopped"
	EventAnalysisStarted      EventType = "analysis_started"
	EventAnalysisComplete     EventType = "analysis_complete"
	EventAnalysisFailed       EventType = "analysis_failed"
	EventAlbumResolving       EventType = "album_resolving"
	EventAlbumResolved        EventType = "album_resolved"
	EventAlbumFailed          EventType = "album_failed"
	EventAlbumUnavailable     EventType = "album_unavailable"
	EventAnalysisUnavailable  EventType = "analysis_unavailable"
	EventCleanerStarted       EventType = "cleaner_started"
	EventCleanerBatch         EventType = "cleaner_batch"
	EventInfo                 EventType = "info"
)

type ActivityEvent struct {
	Timestamp    time.Time `json:"timestamp"`
	Type         EventType `json:"event"`
	Identifier   string    `json:"identifier,omitempty"`
	CollectionID string    `json:"collection_id,omitempty"`
	Message      string    `json:"message,omitempty"`
	Count        int       `json:"count,omitempty"`
	Cursor       string    `json:"cursor,omitempty"`
	Total        int       `json:"total,omitempty"`
	Error        string    `json:"error,omitempty"`
	WorkerID     string    `json:"worker_id,omitempty"`
	QualityScore float64   `json:"quality_score,omitempty"`
	TrackCount   int       `json:"track_count,omitempty"`
}

// eventChannelBuffer sizes both the producer channel and the decoupled display
// channel. With non-blocking emission, this bounds how many events survive a
// burst before drops begin.
const eventChannelBuffer = 256

func NewEventChannel() chan ActivityEvent {
	return make(chan ActivityEvent, eventChannelBuffer)
}

// Emit sends ev on ch without ever blocking. If the buffer is full the event is
// dropped (drop-newest). This guarantees producer goroutines (analyzers,
// resolvers, cleaners, the coordinator) are never stalled by a slow consumer
// such as the TUI render loop.
//
// Producer-side "drop-oldest" is intentionally avoided: receiving from a shared
// multi-producer channel to evict the oldest event would race with the single
// consumer and could steal legitimate events.
func Emit(ch chan<- ActivityEvent, ev ActivityEvent) {
	select {
	case ch <- ev:
	default:
	}
}

// StartEventDecoupler forwards events from the producer channel to a separate
// display channel, dropping newest events only when the display channel is
// full. The forwarder's receive on in is the only consumer of the producer
// channel, so producers effectively never block: the forwarder drains in
// continuously and never blocks on out.
//
// This decouples the producer rate from the consumer (render) rate, which is
// what keeps analysis progressing even when the TUI is busy rendering.
func StartEventDecoupler(in <-chan ActivityEvent) chan ActivityEvent {
	out := make(chan ActivityEvent, eventChannelBuffer)
	go func() {
		for ev := range in {
			Emit(out, ev)
		}
		close(out)
	}()
	return out
}
