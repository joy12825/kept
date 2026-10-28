package events

import "testing"

func TestPublishReachesOnlyTheWorkspace(t *testing.T) {
	b := NewBus()
	a, cancelA := b.Subscribe(1)
	other, cancelOther := b.Subscribe(2)
	defer cancelOther()
	b.Publish(Event{Type: JobUpdated, WorkspaceID: 1, Data: 7})
	if e := <-a; e.Type != JobUpdated || e.Data != 7 || e.At.IsZero() {
		t.Errorf("event = %+v", e)
	}
	select {
	case e := <-other:
		t.Errorf("workspace 2 received %+v", e)
	default:
	}
	cancelA()
	cancelA()                                          // idempotent
	b.Publish(Event{Type: JobUpdated, WorkspaceID: 1}) // no subscriber left: must not panic
}

func TestSlowSubscribersDoNotBlock(t *testing.T) {
	b := NewBus()
	_, cancel := b.Subscribe(1)
	defer cancel()
	dropped := 0
	b.OnDrop(func(Event) { dropped++ })
	for i := 0; i < buffer+10; i++ {
		b.Publish(Event{Type: JobUpdated, WorkspaceID: 1})
	}
	if dropped != 10 {
		t.Errorf("dropped = %d, want 10", dropped)
	}
	var nilBus *Bus
	nilBus.Publish(Event{}) // a nil bus is a no-op
}
