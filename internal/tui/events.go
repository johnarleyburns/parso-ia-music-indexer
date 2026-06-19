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

func NewEventChannel() chan ActivityEvent {
	return make(chan ActivityEvent, 100)
}
