package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

var paused atomic.Bool
var verbose atomic.Bool

var (
	maxStoredBody    = 1 << 20 // 1 MB per body
	maxStoredEntries = 1000    // circular buffer size
)

func setVerbose(b bool) { verbose.Store(b) }
func isVerbose() bool   { return verbose.Load() }
func isPaused() bool { return paused.Load() }

type PersistedData struct {
	Captures    []Capture    `json:"captures"`
	ColorRules  []ColorRule  `json:"color_rules,omitempty"`
	SearchItems []SearchItem `json:"search_history,omitempty"`
}

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