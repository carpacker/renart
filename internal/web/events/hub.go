// Package events provides SSE pub/sub functionality for the Bruin web server.
package events

import (
	"encoding/json"
	"sync"
	"time"
)

// Hub manages SSE client subscriptions and message broadcasting.
// It supports optional event coalescing: rapid publishes within a debounce
// window are merged so only the latest payload is sent.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}

	// Debounce support: pending holds the latest event during a debounce
	// window. When debounce <= 0, events are published immediately.
	debounceMu sync.Mutex
	debounce   time.Duration
	pending    []byte
	timer      *time.Timer
}

// NewHub creates a new SSE hub with no debounce (immediate publishing).
func NewHub() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

// NewDebouncedHub creates an SSE hub that coalesces rapid publishes.
// Events published within the debounce window are merged: only the
// latest event is sent once the window expires.
func NewDebouncedHub(debounce time.Duration) *Hub {
	return &Hub{
		clients:  make(map[chan []byte]struct{}),
		debounce: debounce,
	}
}

// Subscribe returns a channel that receives published events.
// The caller must call Unsubscribe when done to prevent leaks.
func (h *Hub) Subscribe() chan []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 16)
	h.clients[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a client channel from the hub and closes it.
func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	close(ch)
}

// Publish broadcasts a message to all subscribed clients.
// If the hub was created with NewDebouncedHub, the message is coalesced:
// only the latest message within the debounce window is actually sent.
func (h *Hub) Publish(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}

	if h.debounce <= 0 {
		h.broadcast(payload)
		return
	}

	h.debounceMu.Lock()
	defer h.debounceMu.Unlock()

	h.pending = payload

	if h.timer != nil {
		h.timer.Stop()
	}
	h.timer = time.AfterFunc(h.debounce, h.flush)
}

// PublishImmediate broadcasts a message immediately, bypassing any debounce.
// Use for events that must be delivered without delay (e.g. handler-triggered).
func (h *Hub) PublishImmediate(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.broadcast(payload)
}

func (h *Hub) flush() {
	h.debounceMu.Lock()
	payload := h.pending
	h.pending = nil
	h.timer = nil
	h.debounceMu.Unlock()

	if payload != nil {
		h.broadcast(payload)
	}
}

func (h *Hub) broadcast(payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- payload:
		default:
			// Client buffer full, drop message
		}
	}
}

// ClientCount returns the number of currently subscribed clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
