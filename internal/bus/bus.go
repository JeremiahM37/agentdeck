// Package bus is the in-process SSE fan-out. Channels are "board" (global) and
// "task:{id}".
package bus

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Message is one server-sent event.
type Message struct {
	ID    uint64 `json:"id"`
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// Bus fans published messages out to every subscriber of a channel.
type Bus struct {
	mu   sync.Mutex
	subs map[string]map[chan Message]struct{}
	seq  uint64
}

// New builds an empty bus.
func New() *Bus {
	return &Bus{subs: map[string]map[chan Message]struct{}{}}
}

// Subscribe registers a buffered channel on a channel name.
func (b *Bus) Subscribe(channel string) chan Message {
	ch := make(chan Message, 500)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[channel] == nil {
		b.subs[channel] = map[chan Message]struct{}{}
	}
	b.subs[channel][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *Bus) Unsubscribe(channel string, ch chan Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if set, ok := b.subs[channel]; ok {
		if _, ok := set[ch]; ok {
			delete(set, ch)
			close(ch)
		}
		if len(set) == 0 {
			delete(b.subs, channel)
		}
	}
}

// Publish delivers to every subscriber, dropping for any that is full — a slow
// client must never stall the scheduler, and the UI refetches on reconnect.
func (b *Bus) Publish(channel, event string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	msg := Message{ID: b.seq, Event: event, Data: data}
	for ch := range b.subs[channel] {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Format renders a message in the SSE wire format.
func Format(m Message) string {
	data, err := json.Marshal(m.Data)
	if err != nil {
		data = []byte("{}")
	}
	return fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", m.ID, m.Event, data)
}
