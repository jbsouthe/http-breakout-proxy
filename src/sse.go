package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

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
	ch := make(chan Capture, 16)
	b.Lock()
	b.clients[ch] = struct{}{}
	n := len(b.clients)
	b.Unlock()
	log.Printf("SSE: clients=%d", n)
	return ch
}
func (b *sseBroker) removeClient(ch chan Capture) {
	b.Lock()
	delete(b.clients, ch)
	n := len(b.clients)
	close(ch)
	b.Unlock()
	log.Printf("SSE: clients=%d", n)
}
func (b *sseBroker) publish(c Capture) {
	b.Lock()
	n := 0
	for ch := range b.clients {
		n++
		select {
		case ch <- c:
		default: /* drop if slow */
		}
	}
	b.Unlock()
	log.Printf("SSE: published id=%d to %d client(s)", c.ID, n)
}

func sseHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("SSE: client connect %s", r.RemoteAddr)

		// Mandatory SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Helpful for some proxies
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		ch := b.addClient()
		defer b.removeClient(ch)

		// Initial comment + flush to commit headers
		_, _ = w.Write([]byte(": ok\n\n"))
		flusher.Flush()

		// Optional heartbeat to keep intermediaries alive
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()

		notify := r.Context().Done()
		for {
			select {
			case c, ok := <-ch:
				if !ok {
					return
				}
				bts, _ := json.Marshal(c)
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(bts)
				_, _ = w.Write([]byte("\n\n"))
				flusher.Flush()
			case <-heartbeat.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flusher.Flush()
			case <-notify:
				log.Printf("SSE: client disconnect %s", r.RemoteAddr)
				return
			}
		}
	}
}
