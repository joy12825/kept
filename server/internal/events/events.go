// Package events is the in-process event bus behind the SSE stream (design
// doc 11.8). Publishers never block: a subscriber that falls behind loses
// events, and the web app refetches what it shows when it reconnects.
package events

import (
	"sync"
	"time"
)

// Event types.
const (
	JobUpdated      = "job.updated"
	InboxChanged    = "inbox.changed"
	DocumentUpdated = "document.updated"
)

type Event struct {
	Type        string    `json:"type"`
	WorkspaceID int64     `json:"workspaceId"`
	Data        any       `json:"data,omitempty"`
	At          time.Time `json:"at"`
}

type Bus struct {
	mu      sync.Mutex
	nextID  int
	subs    map[int]subscriber
	dropped func(Event)
}

type subscriber struct {
	ws int64
	ch chan Event
}

const buffer = 64

func NewBus() *Bus { return &Bus{subs: map[int]subscriber{}} }

// OnDrop is called (for logging) when a slow subscriber misses an event.
func (b *Bus) OnDrop(f func(Event)) { b.mu.Lock(); b.dropped = f; b.mu.Unlock() }

// Subscribe receives the events of one workspace until cancel is called.
func (b *Bus) Subscribe(workspaceID int64) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	ch := make(chan Event, buffer)
	b.subs[id] = subscriber{ws: workspaceID, ch: ch}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(ch)
		})
	}
}

// SubscribeAll receives the events of every workspace (the search indexer);
// never hand it to a client.
func (b *Bus) SubscribeAll() (<-chan Event, func()) { return b.Subscribe(allWorkspaces) }

// allWorkspaces is a workspace ID no workspace has (they start at 1).
const allWorkspaces = 0

// Publish delivers e to the subscribers of its workspace without waiting.
// Publish after the change it announces has been committed.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if s.ws != allWorkspaces && s.ws != e.WorkspaceID {
			continue
		}
		select {
		case s.ch <- e:
		default:
			if b.dropped != nil {
				b.dropped(e)
			}
		}
	}
}
