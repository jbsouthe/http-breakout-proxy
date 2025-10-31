package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed ui/*
var uiFS embed.FS

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

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	// /api/searches (GET list, POST upsert, PUT replace)
	mux.HandleFunc("/api/searches", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(searches.getAll())
			return
		case http.MethodPost:
			var in struct {
				Query  string `json:"query"`
				Label  string `json:"label"`
				Pinned bool   `json:"pinned"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			it := searches.upsertRaw(in.Query, in.Label, in.Pinned)
			if it.ID == "" {
				http.Error(w, "empty query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(it)
			return
		case http.MethodPut:
			var arr []SearchItem
			if err := json.NewDecoder(r.Body).Decode(&arr); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			searches.replace(arr)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"updated": len(arr)})
			return
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
	})

	// /api/searches/{id} (DELETE)
	mux.HandleFunc("/api/searches/", func(w http.ResponseWriter, r *http.Request) {
		if isVerbose() {
			log.Printf("UI Request URI: %s %s", r.Method, r.RequestURI)
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/searches/")
		if id == "" || strings.Contains(id, "/") {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
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

	// Static UI from embedded FS at root
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	return mux
}
