package main

import (
	"sync"
	"time"
)

// Capture represents a single proxied transaction (request + response)
type Capture struct {
	ID                 int64               `json:"id"`
	Name               string              `json:"name,omitempty"`
	Time               time.Time           `json:"time"`
	Method             string              `json:"method"`
	URL                string              `json:"url"`
	RequestHeaders     map[string][]string `json:"request_headers"`
	RequestBodyBase64  string              `json:"request_body"` // base64 or truncated raw (string)
	ResponseStatus     int                 `json:"response_status"`
	ResponseHeaders    map[string][]string `json:"response_headers"`
	ResponseBodyBase64 string              `json:"response_body"`
	DurationMs         int64               `json:"duration_ms"`
	Notes              string              `json:"notes,omitempty"`
	Deleted            bool                `json:"deleted,omitempty"`

	// Phase timings (milliseconds)
	DNSMs      int64 `json:"dns_ms,omitempty"`
	ConnectMs  int64 `json:"connect_ms,omitempty"`
	TLSMs      int64 `json:"tls_ms,omitempty"`
	SendMs     int64 `json:"send_ms,omitempty"`      // write request body (client -> origin)
	TTFBMs     int64 `json:"ttfb_ms,omitempty"`      // first byte from origin after write
	RespReadMs int64 `json:"resp_read_ms,omitempty"` // body read duration (first->last byte)
	TotalMs    int64 `json:"total_ms,omitempty"`     // wall clock: RoundTrip start -> last byte

	// Connection / protocol
	ServerAddr string `json:"server_addr,omitempty"` // ip:port of origin
	ReusedConn bool   `json:"reused_conn,omitempty"`
	HTTP2      bool   `json:"h2,omitempty"` // negotiated h2
}

type captureStore struct {
	sync.Mutex
	buf   []Capture
	next  int
	count int
	seq   int64
}

func newCaptureStore(cap int) *captureStore {
	return &captureStore{
		buf:  make([]Capture, cap),
		next: 0,
		seq:  1,
	}
}

func (s *captureStore) add(c Capture) Capture {
	s.Lock()
	defer s.Unlock()
	c.ID = s.seq
	s.seq++
	s.buf[s.next] = c
	idx := s.next
	s.next = (s.next + 1) % len(s.buf)
	if s.count < len(s.buf) {
		s.count++
	}
	// return stored copy with assigned ID
	return s.buf[idx]
}

func (s *captureStore) list() []Capture {
	s.Lock()
	defer s.Unlock()
	out := make([]Capture, 0, s.count)
	// oldest first
	start := (s.next - s.count + len(s.buf)) % len(s.buf)
	for i := 0; i < s.count; i++ {
		out = append(out, s.buf[(start+i)%len(s.buf)])
	}
	return out
}

func (s *captureStore) get(id int64) (Capture, bool) {
	s.Lock()
	defer s.Unlock()
	for i := 0; i < s.count; i++ {
		idx := (s.next - s.count + i + len(s.buf)) % len(s.buf)
		if s.buf[idx].ID == id {
			return s.buf[idx], true
		}
	}
	return Capture{}, false
}

// delete removes a capture by ID. Returns true if deleted.
func (s *captureStore) delete(id int64) bool {
	s.Lock()
	defer s.Unlock()
	// Rebuild the list excluding the target id
	kept := make([]Capture, 0, s.count)
	start := (s.next - s.count + len(s.buf)) % len(s.buf)
	for i := 0; i < s.count; i++ {
		c := s.buf[(start+i)%len(s.buf)]
		if c.ID != id {
			kept = append(kept, c)
		}
	}
	if len(kept) == s.count {
		// nothing removed
		return false
	}
	// clear buffer and repopulate from kept
	for i := range s.buf {
		var zero Capture
		s.buf[i] = zero
	}
	for i := 0; i < len(kept); i++ {
		s.buf[i] = kept[i]
	}
	s.count = len(kept)
	s.next = s.count % len(s.buf)
	return true
}

// populateFromSlice fills the circular buffer from a slice of captures
func (s *captureStore) populateFromSlice(list []Capture) {
	s.Lock()
	defer s.Unlock()
	if len(list) == 0 {
		return
	}
	capBuf := len(s.buf)
	// If loaded list is larger than buffer, keep only the latest entries
	if len(list) > capBuf {
		list = list[len(list)-capBuf:]
	}
	// copy into buffer starting at 0..len(list)-1
	for i := 0; i < len(list); i++ {
		s.buf[i] = list[i]
	}
	s.count = len(list)
	s.next = s.count % capBuf
	// set seq to max ID + 1 to avoid collisions
	var maxID int64 = 0
	for _, c := range list {
		if c.ID > maxID {
			maxID = c.ID
		}
	}
	if maxID >= s.seq {
		s.seq = maxID + 1
	}
}

func (s *captureStore) clear() {
	s.Lock()
	defer s.Unlock()
	for i := range s.buf {
		var zero Capture
		s.buf[i] = zero
	}
	s.count = 0
	s.next = 0
}

func (s *captureStore) updateName(id int64, name string) (Capture, bool) {
	s.Lock()
	defer s.Unlock()
	for i := 0; i < s.count; i++ {
		idx := (s.next - s.count + i + len(s.buf)) % len(s.buf)
		if s.buf[idx].ID == id {
			s.buf[idx].Name = name
			return s.buf[idx], true
		}
	}
	return Capture{}, false
}