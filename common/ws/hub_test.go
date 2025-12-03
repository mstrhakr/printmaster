package ws

import (
	"sync"
	"testing"
	"time"
)

func TestHubRegisterUnregister(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Stop()

	ch := make(chan Message, 10)
	hub.Register("client1", ch)

	// Give the hub goroutine time to process the registration
	time.Sleep(10 * time.Millisecond)

	// Verify client is registered by broadcasting a message
	hub.Broadcast(Message{Type: "test"})

	select {
	case msg := <-ch:
		if msg.Type != "test" {
			t.Errorf("expected message type 'test', got %q", msg.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("did not receive broadcast message")
	}

	// Unregister and verify channel is closed
	hub.Unregister("client1")

	// Give hub time to process unregister
	time.Sleep(10 * time.Millisecond)

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unregister")
	}
}

func TestHubBroadcastToMultipleClients(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Stop()

	const numClients = 5
	channels := make([]chan Message, numClients)

	for i := 0; i < numClients; i++ {
		channels[i] = make(chan Message, 10)
		hub.Register(string(rune('A'+i)), channels[i])
	}

	// Give hub time to register all clients
	time.Sleep(20 * time.Millisecond)

	// Broadcast a message
	testMsg := Message{Type: "broadcast_test", Data: map[string]interface{}{"value": 42}}
	hub.Broadcast(testMsg)

	// All clients should receive the message
	for i, ch := range channels {
		select {
		case msg := <-ch:
			if msg.Type != "broadcast_test" {
				t.Errorf("client %d: expected type 'broadcast_test', got %q", i, msg.Type)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("client %d: did not receive broadcast message", i)
		}
	}
}

func TestHubUnregisterNonexistent(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Stop()

	// Should not panic when unregistering non-existent client
	hub.Unregister("nonexistent")

	// Give hub time to process
	time.Sleep(10 * time.Millisecond)
}

func TestHubStop(t *testing.T) {
	t.Parallel()

	hub := NewHub()

	ch1 := make(chan Message, 10)
	ch2 := make(chan Message, 10)
	hub.Register("client1", ch1)
	hub.Register("client2", ch2)

	// Give hub time to register
	time.Sleep(10 * time.Millisecond)

	// Stop the hub
	hub.Stop()

	// Give hub time to clean up
	time.Sleep(20 * time.Millisecond)

	// Both channels should be closed
	_, ok1 := <-ch1
	_, ok2 := <-ch2

	if ok1 {
		t.Error("ch1 should be closed after Stop()")
	}
	if ok2 {
		t.Error("ch2 should be closed after Stop()")
	}
}

func TestHubBroadcastDropsWhenFull(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Stop()

	// Use unbuffered channel to simulate a slow client
	ch := make(chan Message) // unbuffered!
	hub.Register("slow_client", ch)

	// Give hub time to register
	time.Sleep(10 * time.Millisecond)

	// Broadcast without anyone receiving - should not block
	done := make(chan struct{})
	go func() {
		hub.Broadcast(Message{Type: "test1"})
		hub.Broadcast(Message{Type: "test2"})
		hub.Broadcast(Message{Type: "test3"})
		close(done)
	}()

	select {
	case <-done:
		// Good - broadcast didn't block
	case <-time.After(500 * time.Millisecond):
		t.Error("Broadcast blocked on slow client")
	}
}

func TestHubConcurrentAccess(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Stop()

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOps = 50

	// Spawn multiple goroutines doing register/unregister/broadcast
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOps; j++ {
				// Use unique client ID for each registration to avoid channel reuse issues
				clientID := string(rune('A'+id)) + "_" + string(rune('0'+j%10))
				ch := make(chan Message, 100)

				hub.Register(clientID, ch)
				hub.Broadcast(Message{Type: "concurrent"})
				hub.Unregister(clientID)
			}
		}(i)
	}

	// Should complete without race conditions or deadlocks
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Error("concurrent access test timed out - possible deadlock")
	}
}

func TestHubBroadcastQueueFull(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Stop()

	// Fill the broadcast queue (size 100)
	// This should not block - messages are dropped if queue is full
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			hub.Broadcast(Message{Type: "flood"})
		}
		close(done)
	}()

	select {
	case <-done:
		// Good - didn't block
	case <-time.After(time.Second):
		t.Error("Broadcast blocked when queue was full")
	}
}
