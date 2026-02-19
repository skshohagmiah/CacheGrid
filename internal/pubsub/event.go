package pubsub

import "time"

// EventType represents a cache event type.
type EventType string

const (
	EventSet    EventType = "set"
	EventDelete EventType = "delete"
	EventExpire EventType = "expire"
	EventEvict  EventType = "evict"
)

// Event represents a cache event.
type Event struct {
	Type      EventType
	Key       string
	Value     []byte
	Timestamp time.Time
	NodeName  string
}
