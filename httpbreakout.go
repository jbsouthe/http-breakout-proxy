package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"sort"

	"github.com/elazarl/goproxy"
)

//go:embed ui/*
var uiFS embed.FS
var paused atomic.Bool
var verbose atomic.Bool

var (
	maxStoredBody    = 1 << 20 // 1 MB per body
	maxStoredEntries = 1000    // circular buffer size
)

func setVerbose(b bool) { verbose.Store(b) }
func isVerbose() bool   { return verbose.Load() }

type PersistedData struct {
	Captures    []Capture    `json:"captures"`
	ColorRules  []ColorRule  `json:"color_rules,omitempty"`
	SearchItems []SearchItem `json:"search_history,omitempty"`
}

type SearchItem struct {
	ID        string    `json:"id"`
	Query     string    `json:"query"` // the raw filter string
	Label     string    `json:"label,omitempty"`
	Pinned    bool      `json:"pinned,omitempty"`
	Count     int       `json:"count,omitempty"`
	LastUsed  time.Time `json:"last_used"`
	CreatedAt time.Time `json:"created_at"`
}

type searchStore struct {
	sync.RWMutex
	items []SearchItem // MRU: pinned first (stable), then recency descending
	cap   int
}

func newSearchStore(cap int) *searchStore { return &searchStore{cap: cap} }
func (s *searchStore) getAll() []SearchItem {
	s.RLock()
	defer s.RUnlock()
	out := make([]SearchItem, len(s.items))
	copy(out, s.items)
	return out
}
func (s *searchStore) replace(all []SearchItem) {
	s.Lock()
	defer s.Unlock()
	s.items = normalizeAndSort(all, s.cap)
}
func (s *searchStore) upsertRaw(q string, label string, pin bool) SearchItem {
	now := time.Now().UTC()
	s.Lock()
	defer s.Unlock()

	q = strings.TrimSpace(q)
	if q == "" {
		return SearchItem{}
	}

	// de-dupe by exact query
	idx := -1
	for i := range s.items {
		if s.items[i].Query == q {
			idx = i
			break
		}
	}
	if idx >= 0 {
		it := s.items[idx]
		it.LastUsed = now
		it.Count++
		if label != "" {
			it.Label = label
		}
		if pin {
			it.Pinned = true
		}
		// move to front of its section (pinned or unpinned)
		s.items = append(append(s.items[:idx], s.items[idx+1:]...), it)
	} else {
		it := SearchItem{
			ID:        fmt.Sprintf("s-%d", now.UnixNano()),
			Query:     q,
			Label:     label,
			Pinned:    pin,
			Count:     1,
			LastUsed:  now,
			CreatedAt: now,
		}
		s.items = append([]SearchItem{it}, s.items...)
	}
	s.items = normalizeAndSort(s.items, s.cap)
	return s.items[0]
}
func normalizeAndSort(in []SearchItem, cap int) []SearchItem {
	// stable: pinned first (recency within pinned), then unpinned by recency
	pinned, rest := make([]SearchItem, 0, len(in)), make([]SearchItem, 0, len(in))
	for _, it := range in {
		if it.Query == "" {
			continue
		}
		if it.Pinned {
			pinned = append(pinned, it)
		} else {
			rest = append(rest, it)
		}
	}
	sort.SliceStable(pinned, func(i, j int) bool { return pinned[i].LastUsed.After(pinned[j].LastUsed) })
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].LastUsed.After(rest[j].LastUsed) })
	out := append(pinned, rest...)
	if cap > 0 && len(out) > cap {
		out = out[:cap]
	}
	return out
}

type ColorRule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Query    string `json:"query"`
	Color    string `json:"color"`
	Note     string `json:"note"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority,omitempty"`
}

type ruleStore struct {
	sync.RWMutex
	rules []ColorRule
}

func (rs *ruleStore) getAll() []ColorRule {
	rs.RLock()
	defer rs.RUnlock()
	out := make([]ColorRule, len(rs.rules))
	copy(out, rs.rules)
	return out
}
func (rs *ruleStore) replace(all []ColorRule) {
	rs.Lock()
	defer rs.Unlock()
	// Copy then sort by Priority DESC; stable keeps original relative order for ties.
	copied := append([]ColorRule(nil), all...)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Priority > copied[j].Priority
	})
	rs.rules = copied
}

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

type phases struct {
	startRT          time.Time // RoundTrip start
	dnsStart, dnsEnd time.Time
	conStart, conEnd time.Time
	tlsStart, tlsEnd time.Time
	wroteReq         time.Time
	firstByte        time.Time
	done             time.Time

	serverAddr string
	reused     bool
	h2         bool
}

// req pointer -> phases
var phaseMap sync.Map // map[string]*phases

func millis(a, b time.Time) int64 {
	if a.IsZero() || b.IsZero() {
		return 0
	}
	return b.Sub(a).Milliseconds()
}

type tracingRT struct{ base http.RoundTripper }

func (t tracingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	p := &phases{startRT: time.Now()}
	key := reqKey(req)
	phaseMap.Store(key, p)

	ct := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { p.dnsStart = time.Now() },
		DNSDone:  func(httptrace.DNSDoneInfo) { p.dnsEnd = time.Now() },

		ConnectStart: func(_, _ string) { p.conStart = time.Now() },
		ConnectDone:  func(_, _ string, _ error) { p.conEnd = time.Now() },

		TLSHandshakeStart: func() { p.tlsStart = time.Now() },
		TLSHandshakeDone: func(cs tls.ConnectionState, _ error) {
			p.tlsEnd = time.Now()
			p.h2 = (cs.NegotiatedProtocol == "h2")
		},

		GotConn: func(ci httptrace.GotConnInfo) {
			p.reused = ci.Reused
			if ci.Conn != nil && ci.Conn.RemoteAddr() != nil {
				p.serverAddr = ci.Conn.RemoteAddr().String()
			}
		},

		WroteRequest: func(httptrace.WroteRequestInfo) { p.wroteReq = time.Now() },

		GotFirstResponseByte: func() { p.firstByte = time.Now() },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), ct))

	// Delegate to base
	resp, err := t.base.RoundTrip(req)

	// Mark completion when the body is fully read by caller (OnResponse handler will set p.done)
	// If an error happened before body is available, mark done now.
	if err != nil {
		p.done = time.Now()
	}
	return resp, err
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

// persistHelpers: save/load circular buffer to JSON file (atomic write)
// saveAll writes both captures and color rules atomically.
func saveAll(path string, caps []Capture, rules []ColorRule) error {
	payload := PersistedData{
		Captures:   caps,
		ColorRules: rules,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadAll reads captures + rules. Back-compat: if the file is either a plain
// []Capture or an object containing only captures, we still succeed.
func loadAll(path string) ([]Capture, []ColorRule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	// Try new format first
	var pd PersistedData
	if err := json.Unmarshal(b, &pd); err == nil && (pd.Captures != nil || pd.ColorRules != nil) {
		return pd.Captures, pd.ColorRules, nil
	}

	return nil, nil, fmt.Errorf("unrecognized persistence format")
}

func defaultColorRules() []ColorRule {
	return []ColorRule{
		{
			ID:       "1",
			Name:     "red",
			Color:    "#e74c3c", // red
			Query:    "status:5",
			Priority: 100,
			Note:     "Failed HTTP request, 5xx Errors",
			Enabled:  true,
		},
		{
			ID:       "2",
			Name:     "orange",
			Color:    "#d77d28", // orange
			Query:    "status:4",
			Priority: 100,
			Note:     "Failed HTTP request, 4xx Errors",
			Enabled:  true,
		},
		{
			ID:       "3",
			Name:     "blue",
			Color:    "#3498db", // blue
			Query:    "method:POST",
			Priority: 0,
			Note:     "General API traffic",
			Enabled:  true,
		},
		{
			ID:       "4",
			Name:     "green",
			Color:    "#2ecc71", // green
			Query:    "method:GET",
			Priority: 0,
			Note:     "GET request",
			Enabled:  true,
		},
	}
}

func saveCapturesToFile(path string, list []Capture) error {
	// write to temp file and rename
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadCapturesFromFile(path string) ([]Capture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []Capture
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	return list, nil
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

// small helper: read up to N bytes and return as string (base64 would be safer for arbitrary bytes).
// imports needed:
//   "bytes"
//   "compress/gzip"
//   "compress/zlib"
//   "io"
//   "io/ioutil"
//   "strings"

func readLimitedBody(rc io.ReadCloser, max int, encoding string) (string, io.ReadCloser, error) {
	if rc == nil {
		return "", ioutil.NopCloser(bytes.NewReader(nil)), nil
	}
	defer rc.Close()

	var buf bytes.Buffer
	limited := io.LimitReader(rc, int64(max)+1) // read up to max+1 to detect truncation
	n, err := io.Copy(&buf, limited)
	if err != nil {
		return "", ioutil.NopCloser(bytes.NewReader(nil)), err
	}
	raw := buf.Bytes()

	// Always return the ORIGINAL bytes to the caller for reconstituting r.Body,
	// so proxying behavior is unchanged.
	restore := ioutil.NopCloser(bytes.NewReader(raw))

	// If we exceeded the cap, we cannot reliably decompress; return truncated marker.
	if n > int64(max) {
		return string(raw[:max]) + "\n--truncated--", restore, nil
	}

	// Try to decode per Content-Encoding for DISPLAY ONLY.
	// We keep the original compressed bytes in the returned ReadCloser.
	enc := strings.ToLower(strings.TrimSpace(encoding))
	switch enc {
	case "gzip":
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err == nil {
			defer gr.Close()
			if dec, derr := ioutil.ReadAll(gr); derr == nil {
				// Trim to max if decompressed payload is too large (should be rare here since n<=max).
				if len(dec) > max {
					return string(dec[:max]) + "\n--truncated--", restore, nil
				}
				return string(dec), restore, nil
			}
		}
	case "deflate":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err == nil {
			defer zr.Close()
			if dec, derr := ioutil.ReadAll(zr); derr == nil {
				if len(dec) > max {
					return string(dec[:max]) + "\n--truncated--", restore, nil
				}
				return string(dec), restore, nil
			}
		}
	}

	// Fallback: treat as plain text
	return string(raw), restore, nil
}

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

// generateCA returns both:
// - parsedCert: *x509.Certificate  (suitable for goproxy.GoproxyCa)
// - parsedKey:  *rsa.PrivateKey    (suitable for goproxy.GoproxyCaKey)
// - certPEM, keyPEM []byte         (PEM bytes, useful to build tls.Certificate)
func generateCA() (parsedCert *x509.Certificate, parsedKey *rsa.PrivateKey, certPEM, keyPEM []byte, err error) {
	// generate key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// create certificate template
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Example MITM CA"},
			CommonName:   "Example MITM CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	parsed, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// pem encode cert and key
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return parsed, priv, certPEM, keyPEM, nil
}

// call this before starting the proxy server
func enableMITM(proxy *goproxy.ProxyHttpServer, persist bool, dir string) error {
	// Load or generate an X.509 CA (your existing helpers are fine)
	var (
		caX509 *x509.Certificate
		caKey  *rsa.PrivateKey
		err    error
	)
	if persist {
		caX509, caKey, err = loadOrCreateCA(filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca.key"))
	} else {
		caX509, caKey, err = createEphemeralCA()
	}
	if err != nil {
		return err
	}

	// Build a tls.Certificate (this is what the current API expects)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caX509.Raw})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})

	caPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("failed to build tls cert pair: %w", err)
	}

	// Create a TLS config generator from the CA pair
	tlsFromCA := goproxy.TLSConfigFromCA(&caPair) // returns func(host, ctx) (*tls.Config, error)

	// Instruct goproxy to MITM CONNECT using our CA
	proxy.OnRequest().HandleConnect(goproxy.FuncHttpsHandler(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return &goproxy.ConnectAction{
				Action:    goproxy.ConnectMitm,
				TLSConfig: tlsFromCA,
			}, host
		},
	))

	// Upstream transport: we usually skip verification since we’re intercepting
	proxy.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return nil
}

func loadOrCreateCA(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	// Try to load
	if cert, key, err := loadCA(certPath, keyPath); err == nil {
		return cert, key, nil
	}
	// Create and persist
	cert, key, err := createEphemeralCA()
	if err != nil {
		return nil, nil, err
	}
	if err := saveCA(cert, key, certPath, keyPath); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func loadCA(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	cb, _ := pem.Decode(certPEM)
	if cb == nil || cb.Type != "CERTIFICATE" {
		return nil, nil, errors.New("invalid CA cert PEM")
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil || (kb.Type != "RSA PRIVATE KEY" && kb.Type != "PRIVATE KEY") {
		return nil, nil, errors.New("invalid CA key PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	var key *rsa.PrivateKey
	if kb.Type == "RSA PRIVATE KEY" {
		key, err = x509.ParsePKCS1PrivateKey(kb.Bytes)
	} else {
		// If you later switch to PKCS8, add x509.ParsePKCS8PrivateKey here.
		return nil, nil, errors.New("unsupported private key format")
	}
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func saveCA(cert *x509.Certificate, key *rsa.PrivateKey, certPath, keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return err
	}
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certOut, fs.FileMode(0o644)); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, keyOut, fs.FileMode(0o600)); err != nil {
		return err
	}
	return nil
}

func createEphemeralCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Go MITM Proxy CA"},
			CommonName:   "Go MITM Proxy CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            2,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// --- end MITM initialization --------------------------------------------------

func main() {
	// CLI flags (match README)
	var (
		listen     = flag.String("l", "127.0.0.1:8080", "address for proxy + UI to listen on (single-port mode)")
		mitm       = flag.Bool("mitm", true, "enable HTTPS Man In The Middle mode (requires installing CA in clients)")
		caDir      = flag.String("ca", "./ca", "directory to store persistent CA cert and key")
		persist    = flag.String("f", "./captures.json", "path to captures persistence file (e.g. ./captures.json). empty = no persistence")
		maxBody    = flag.Int("max-body", maxStoredBody, "maximum bytes to store/display per request/response body")
		bufferSize = flag.Int("buffer-size", maxStoredEntries, "circular buffer capacity for captured entries")
		verbose    = flag.Bool("v", false, "enable verbose logging")
	)
	flag.Parse()

	setVerbose(*verbose)

	if isVerbose() {
		log.Printf("Flags: listen=%s mitm=%v ca=%s file=%s max-body=%d buffer-size=%d verbose=%s",
			*listen, *mitm, *caDir, *persist, *maxBody, *bufferSize, *verbose)
	}

	// Apply runtime-configurable constants (if you prefer to keep package-level consts, you can copy/assign)
	// Replace the package consts with local vars where needed. Example:
	// Note: here we update the globals used elsewhere by assigning.
	// If you want to avoid globals, refactor code to accept these parameters.
	// Update globals (one-off):
	// maxStoredBody = *maxBody         // cannot assign to const; make variable if needed
	// maxStoredEntries = *bufferSize   // same as above

	// If you want to change buffer sizes dynamically, change your package-level consts to vars:
	// var maxStoredBody = 1 << 20
	// var maxStoredEntries = 1000
	// then here: maxStoredBody = *maxBody; maxStoredEntries = *bufferSize

	paused.Store(false)

	// Create store with configured capacity
	store := newCaptureStore(*bufferSize)
	rules := &ruleStore{}
	broker := newSseBroker()
	searches := newSearchStore(100)

	// Persistence
	persistPath := *persist
	if persistPath != "" {
		if caps, crs, err := loadAll(persistPath); err == nil {
			log.Printf("Loaded %d captures and %d color rules from %s", len(caps), len(crs), persistPath)
			// populate capture store
			for _, c := range caps {
				_ = store.add(c) // or store.populateFromSlice if you have it
			}
			// populate rules
			rules.replace(crs)
		} else if !os.IsNotExist(err) {
			log.Printf("Warning: failed to load %s: %v", persistPath, err)
		} else if os.IsNotExist(err) {
			rules.replace(defaultColorRules())
		}

		// periodic save
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := saveAll(persistPath, store.list(), rules.getAll()); err != nil {
					log.Printf("Error saving %s: %v", persistPath, err)
				}
			}
		}()

		// graceful shutdown save
		go func() {
			sigc := make(chan os.Signal, 1)
			signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
			<-sigc
			log.Printf("Shutting down: saving %s", persistPath)
			if err := saveAll(persistPath, store.list(), rules.getAll()); err != nil {
				log.Printf("Error saving on shutdown: %v", err)
			}
			os.Exit(0)
		}()
	} else {
		// still set up a graceful shutdown saver that does nothing if no persistence requested
		go func() {
			sigc := make(chan os.Signal, 1)
			signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
			<-sigc
			log.Printf("Shutting down")
			os.Exit(0)
		}()
	}

	// Build handlers. Pass relevant flags through where required:
	uiHandler := buildUIHandler(store, rules, broker, searches)
	// Pass caDir and maxBody if enableMITM or proxy code needs them.
	proxyHandler := buildProxyHandler(*mitm, store, broker, *caDir)

	// Combined handler: route proxy-style requests to proxy; everything else to UI
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Proxy requests are either CONNECT or have an absolute URL (non-empty scheme).
		if r.Method == http.MethodConnect || (r.URL != nil && r.URL.Scheme != "") {
			proxyHandler.ServeHTTP(w, r)
			return
		}
		// Otherwise treat as UI/API/SSE/static request
		uiHandler.ServeHTTP(w, r)
	})

	log.Printf("Listening on %s for Proxy+UI (single-port).", *listen)
	log.Fatal(http.ListenAndServe(*listen, handler))
}

// buildUIHandler returns the mux for UI, REST, SSE, and static files.
func buildUIHandler(store *captureStore, rules *ruleStore, broker *sseBroker, searches *searchStore) http.Handler {
	mux := http.NewServeMux()

	// /api/captures  (list + clear)
	mux.HandleFunc("/api/captures", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		switch r.Method {
		case http.MethodGet:
			list := store.list()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
			return

		case http.MethodDelete:
			// wipe the buffer
			store.clear()
			broker.publish(Capture{Time: time.Now().UTC(), Notes: "cleared"})
			// optional: persist immediately (if you added persistence helpers)
			// _ = saveCapturesToFile("./captures.json", store.list())

			// optional: broadcast a “cleared” event over SSE
			// broker.publish(Capture{Time: time.Now().UTC(), Notes: "cleared"})

			w.WriteHeader(http.StatusNoContent)
			return

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	// GET /api/data -> PersistedData
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PersistedData{
			Captures:   store.list(),
			ColorRules: rules.getAll(),
		})
	})

	mux.HandleFunc("/api/captures/", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		// expect /api/captures/{id}
		const prefix = "/api/captures/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		idStr := r.URL.Path[len(prefix):]
		if idStr == "" || strings.Contains(idStr, "/") {
			http.NotFound(w, r)
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			c, ok := store.get(id)
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(c)
			return

		case http.MethodDelete:
			deleted := store.delete(id)
			if !deleted {
				http.NotFound(w, r)
				return
			}
			// Broadcast a deletion event over SSE
			broker.publish(Capture{
				ID:      id,
				Time:    time.Now().UTC(),
				Deleted: true,
				Notes:   "deleted",
			})
			w.WriteHeader(http.StatusNoContent)
			return

		case http.MethodPatch:
			var payload struct {
				Name *string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Name == nil {
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			updated, ok := store.updateName(id, strings.TrimSpace(*payload.Name))
			if !ok {
				http.NotFound(w, r)
				return
			}
			// notify other clients via SSE
			broker.publish(updated)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(updated)
			return

		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
	})

	// GET /api/rules -> []ColorRule
	mux.HandleFunc("/api/rules", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rules.getAll())
		case http.MethodPut:
			// Replace the full ruleset
			var incoming []ColorRule
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			// Basic validation: ensure IDs exist
			for i := range incoming {
				if strings.TrimSpace(incoming[i].ID) == "" {
					incoming[i].ID = fmt.Sprintf("%d", time.Now().UnixNano()+int64(i))
				}
			}
			// Ensure server canonical order: highest priority first.
			sort.SliceStable(incoming, func(i, j int) bool { return incoming[i].Priority > incoming[j].Priority })
			rules.replace(incoming)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"updated": len(incoming)})
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	})

	// SSE events
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := broker.addClient()
		defer broker.removeClient(ch)

		fmt.Fprintf(w, ": ok\n\n")
		flusher.Flush()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				return
			case c, ok := <-ch:
				if !ok {
					return
				}
				b, _ := json.Marshal(c)
				fmt.Fprintf(w, "data: %s\n\n", b)
				flusher.Flush()
			}
		}
	})

	mux.HandleFunc("/api/pause", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"paused": paused.Load()})
			return

		case http.MethodPost:
			// Accept JSON body: { "paused": true/false }
			var payload struct {
				Paused *bool `json:"paused"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Paused == nil {
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			was := paused.Swap(*payload.Paused)
			// Emit an SSE control event (using your existing Capture type)
			note := "resumed"
			if *payload.Paused {
				note = "paused"
			}
			broker.publish(Capture{
				Time:  time.Now().UTC(),
				Notes: note, // client will look for notes == "paused"/"resumed"
			})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"paused": paused.Load(), "was": was})
			return

			// GET /api/searches -> []SearchItem
			mux.HandleFunc("/api/searches", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					http.Error(w, "method", http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(searches.getAll())
			})

			// POST /api/searches {query,label?,pinned?} -> SearchItem (upsert MRU)
			mux.HandleFunc("/api/searches", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "method", http.StatusMethodNotAllowed)
					return
				}
				var in struct {
					Query, Label string
					Pinned       bool
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					http.Error(w, "bad json", 400)
					return
				}
				it := searches.upsertRaw(in.Query, in.Label, in.Pinned)
				_ = json.NewEncoder(w).Encode(it)
			})

			// PUT /api/searches  (replace whole list)
			mux.HandleFunc("/api/searches", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					http.Error(w, "method", http.StatusMethodNotAllowed)
					return
				}
				var arr []SearchItem
				if err := json.NewDecoder(r.Body).Decode(&arr); err != nil {
					http.Error(w, "bad json", 400)
					return
				}
				searches.replace(arr)
				_ = json.NewEncoder(w).Encode(map[string]any{"updated": len(arr)})
			})

			// DELETE /api/searches/:id
			mux.HandleFunc("/api/searches/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					http.Error(w, "method", http.StatusMethodNotAllowed)
					return
				}
				id := strings.TrimPrefix(r.URL.Path, "/api/searches/")
				items := searches.getAll()
				out := make([]SearchItem, 0, len(items))
				for _, it := range items {
					if it.ID != id {
						out = append(out, it)
					}
				}
				searches.replace(out)
				w.WriteHeader(http.StatusNoContent)
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	// Static UI from embedded FS at root
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	return mux
}

// buildProxyHandler configures and returns the proxy handler.
func buildProxyHandler(mitmEnabled bool, store *captureStore, broker *sseBroker, caDur string) http.Handler {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: true,
	}
	proxy.Tr = tr

	// Enable MITM if requested
	if mitmEnabled {
		if err := enableMITM(proxy, true, caDur); err != nil {
			log.Fatalf("MITM init failed: %v", err)
		}
	} else {
		log.Println("MITM disabled: HTTPS will be tunneled (opaque bodies).")
	}

	// Ephemeral map for partial captures
	var reqMap sync.Map

	// Capture request
	proxy.OnRequest().DoFunc(func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		log.Printf("Proxy Request: %s %s", r.Method, r.URL.String())
		if isPaused() {
			// Do not record; just pass through unchanged.
			return r, nil
		}
		start := time.Now()
		p := &phases{startRT: start}
		key := reqKey(r)
		phaseMap.Store(key, p)

		ct := &httptrace.ClientTrace{
			DNSStart:          func(httptrace.DNSStartInfo) { p.dnsStart = time.Now() },
			DNSDone:           func(httptrace.DNSDoneInfo) { p.dnsEnd = time.Now() },
			ConnectStart:      func(_, _ string) { p.conStart = time.Now() },
			ConnectDone:       func(_, _ string, _ error) { p.conEnd = time.Now() },
			TLSHandshakeStart: func() { p.tlsStart = time.Now() },
			TLSHandshakeDone: func(cs tls.ConnectionState, _ error) {
				p.tlsEnd = time.Now()
				p.h2 = (cs.NegotiatedProtocol == "h2")
			},
			GotConn: func(ci httptrace.GotConnInfo) {
				p.reused = ci.Reused
				if ci.Conn != nil && ci.Conn.RemoteAddr() != nil {
					p.serverAddr = ci.Conn.RemoteAddr().String()
				}
			},
			WroteRequest:         func(httptrace.WroteRequestInfo) { p.wroteReq = time.Now() },
			GotFirstResponseByte: func() { p.firstByte = time.Now() },
		}

		reqHeaders := make(map[string][]string, len(r.Header))
		for k, v := range r.Header {
			reqHeaders[k] = append([]string(nil), v...)
		}
		encoding := r.Header.Get("Content-Encoding")
		bodyStr, newBody, err := readLimitedBody(r.Body, maxStoredBody, encoding)
		if err != nil {
			log.Printf("error reading request body: %v", err)
			bodyStr = "--body-read-error--"
			newBody = io.NopCloser(bytes.NewReader(nil))
		}
		r.Body = newBody

		c := Capture{
			Time:              time.Now().UTC(),
			Method:            r.Method,
			URL:               r.URL.String(),
			RequestHeaders:    reqHeaders,
			RequestBodyBase64: bodyStr,
			Notes:             fmt.Sprintf("pending (captured at %s)", start.Format(time.RFC3339)),
		}

		ctx.UserData = start
		reqMap.Store(key, c)
		r = r.WithContext(httptrace.WithClientTrace(r.Context(), ct))
		return r, nil
	})

	// Capture response
	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if isPaused() || resp == nil || ctx == nil || ctx.Req == nil {
			return resp
		}
		key := reqKey(ctx.Req)
		val, ok := reqMap.Load(key)
		if !ok {
			return resp
		}
		partial := val.(Capture)

		if v, ok := phaseMap.Load(key); ok {
			p := v.(*phases)
			if p.done.IsZero() {
				p.done = time.Now()
			}

			partial.DNSMs = millis(p.dnsStart, p.dnsEnd)
			partial.ConnectMs = millis(p.conStart, p.conEnd)
			partial.TLSMs = millis(p.tlsStart, p.tlsEnd)
			partial.TTFBMs = millis(p.wroteReq, p.firstByte)
			partial.RespReadMs = millis(p.firstByte, p.done)
			partial.TotalMs = millis(p.startRT, p.done)
			partial.ServerAddr = p.serverAddr
			partial.ReusedConn = p.reused
			if resp.Request != nil && resp.Request.TLS != nil {
				partial.HTTP2 = (resp.Request.TLS.NegotiatedProtocol == "h2")
			} else {
				partial.HTTP2 = p.h2
			}
			phaseMap.Delete(key)
		}

		encoding := resp.Header.Get("Content-Encoding")
		respBodyStr, newRespBody, err := readLimitedBody(resp.Body, maxStoredBody, strings.ToLower(encoding))
		if err != nil {
			respBodyStr = "--resp-body-read-error--"
			newRespBody = io.NopCloser(bytes.NewReader(nil))
		}
		resp.Body = newRespBody

		rh := make(map[string][]string, len(resp.Header))
		for k, v := range resp.Header {
			rh[k] = append([]string(nil), v...)
		}

		var durationMs int64
		if st, ok := ctx.UserData.(time.Time); ok {
			durationMs = time.Since(st).Milliseconds()
		}

		if partial.Name == "" {
			partial.Name = fmt.Sprintf("%s %s [%d]", partial.Method, partial.URL, resp.StatusCode)
		}
		partial.ResponseStatus = resp.StatusCode
		partial.ResponseHeaders = rh
		partial.ResponseBodyBase64 = respBodyStr
		partial.DurationMs = durationMs
		partial.Notes = ""

		stored := store.add(partial)
		broker.publish(stored)
		reqMap.Delete(key)

		log.Printf("Response '%s' Status %s", resp.Request.URL.String(), resp.Status)
		return resp
	})

	return proxy
}

// reqKey returns a stable string key for a request pointer
func reqKey(r *http.Request) string {
	return fmt.Sprintf("%p", r)
}

// generateRootCA generates an in-memory RSA root CA certificate for MITM.
// Note: This does not persist the CA; for persistent CA, write cert/key to files and reuse.
func generateRootCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Go MITM Proxy CA"},
			CommonName:   "Go MITM Proxy CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, priv, nil
}

func isPaused() bool { return paused.Load() }

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

// helper: decompress if gzip or deflate
func maybeDecompress(body []byte, encoding string) []byte {
	switch encoding {
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return body
		}
		defer r.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			return body
		}
		return out
	case "deflate":
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return body
		}
		defer r.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			return body
		}
		return out
	default:
		return body
	}
}
