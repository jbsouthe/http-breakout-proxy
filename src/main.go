package main

import (
	"HTTPBreakoutBox/src/analysis"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
func isPaused() bool    { return paused.Load() }

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

	maxStoredBody = *maxBody

	paused.Store(false)

	// Create store with configured capacity
	store := newCaptureStore(*bufferSize)
	rules := &ruleStore{}
	broker := newSseBroker()
	searches := newSearchStore(100)
	analRegistry := analysis.NewDefaultRegistry()
	SetAnalysisRegistry(analRegistry)

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
			// build analysis registry from persisted captures
			RebuildAnalysisFromCaptures(analRegistry, caps)
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
		if r.Method == http.MethodConnect {
			proxyHandler.ServeHTTP(w, r)
			return
		}
		switch {
		case r.URL.Path == "/metrics/temporal":
			handleTemporalMetrics(w, r)
		case r.URL.Path == "/metrics/retries":
			handleRetryMetrics(w, r)
		case r.URL.Path == "/events",
			strings.HasPrefix(r.URL.Path, "/api/"),
			strings.HasSuffix(r.URL.Path, ".js"),
			strings.HasSuffix(r.URL.Path, ".css"),
			strings.HasSuffix(r.URL.Path, ".html"),
			r.URL.Path == "/":

			uiHandler.ServeHTTP(w, r)
		default:
			proxyHandler.ServeHTTP(w, r)
		}
	})

	log.Printf("Listening on %s for Proxy+UI (single-port).", *listen)
	log.Fatal(http.ListenAndServe(*listen, handler))
}
