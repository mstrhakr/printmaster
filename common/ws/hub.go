package ws

import (
	"fmt"
	"sync"
)

// Hub manages in-process subscribers for websocket-capable clients.
// It is intentionally independent of net/http or gorilla/websocket so it
// can be used by both server and agent packages. Callers register a channel
// to receive broadcast messages.
type Hub struct {
	mu         sync.RWMutex
	clients    map[string]chan Message
	register   chan registration
	unregister chan string
	broadcast  chan Message
	shutdown   chan struct{}
}

type registration struct {
	id string
	ch chan Message
}

// NewHub creates and starts a new Hub.
func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[string]chan Message),
		register:   make(chan registration),
		unregister: make(chan string),
		broadcast:  make(chan Message, 100),
		shutdown:   make(chan struct{}),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	for {
		select {
		case reg := <-h.register:
			h.mu.Lock()
			h.clients[reg.id] = reg.ch
			h.mu.Unlock()
		case id := <-h.unregister:
			h.mu.Lock()
			if ch, ok := h.clients[id]; ok {
				close(ch)
				delete(h.clients, id)
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.RLock()
			for id, ch := range h.clients {
				select {
				case ch <- msg:
				default:
					// If client's buffer is full, skip to avoid blocking hub
					fmt.Printf("ws: client %s channel full, dropping message\n", id)
				}
			}
			h.mu.RUnlock()
		case <-h.shutdown:
			h.mu.Lock()
			for id, ch := range h.clients {
				close(ch)
				delete(h.clients, id)
			}
			h.mu.Unlock()
			return
		}
	}
}

// Register registers a client channel and returns an id to use for unregister.
// The provided channel should be buffered (recommended size 10).
func (h *Hub) Register(id string, ch chan Message) {
	h.register <- registration{id: id, ch: ch}
}

// Unregister removes the client with the given id.
func (h *Hub) Unregister(id string) {
	h.unregister <- id
}

// Broadcast sends a message to all registered clients (non-blocking per-client).
func (h *Hub) Broadcast(msg Message) {
	select {
	case h.broadcast <- msg:
	default:
		// Drop if broadcast queue full
	}
}

// Stop shuts down the hub and closes all client channels.
func (h *Hub) Stop() {
	close(h.shutdown)
}
