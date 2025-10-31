package main

import "sync"

// SSE broadcaster for live updates
type sseBroker struct {
	sync.Mutex
	clients map[chan Capture]struct{}
}

func newSseBroker() *sseBroker {
	return &sseBroker{
		clients: make(map[chan Capture]struct{}),
	}
}

func (b *sseBroker) addClient() chan Capture {
	ch := make(chan Capture, 10)
	b.Lock()
	b.clients[ch] = struct{}{}
	b.Unlock()
	return ch
}

func (b *sseBroker) removeClient(ch chan Capture) {
	b.Lock()
	delete(b.clients, ch)
	close(ch)
	b.Unlock()
}

func (b *sseBroker) publish(c Capture) {
	b.Lock()
	for ch := range b.clients {
		select {
		case ch <- c:
		default:
			// drop if client is slow
		}
	}
	b.Unlock()
}