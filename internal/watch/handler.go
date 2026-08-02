package watch

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func Handler(store *Store, hub *LiveHub) http.Handler {
	r := chi.NewRouter()
	r.Get("/", handleIndex)
	r.Get("/snapshots", handleSnapshots(store))
	r.Get("/snapshots/{sessionId}", handleSnapshot(store))
	r.Get("/sessions", handleSessions(hub))
	r.Get("/events", handleEvents(store))
	r.Get("/live/{sessionId}", handleLive(hub))
	return r
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(indexHTML); err != nil {
		slog.Error("failed to write watch index", "error", err)
	}
}

func handleSnapshots(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snaps := store.List()
		if err := json.NewEncoder(w).Encode(snaps); err != nil {
			http.Error(w, `{"error":"failed to encode snapshots"}`, http.StatusInternalServerError)
		}
	}
}

func handleSnapshot(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionId")
		hist, ok := store.Get(sessionID)
		if !ok {
			hist = []*Snapshot{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(hist); err != nil {
			http.Error(w, `{"error":"failed to encode snapshot"}`, http.StatusInternalServerError)
		}
	}
}

func handleSessions(hub *LiveHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		sessions := hub.Sessions()
		if sessions == nil {
			sessions = []string{}
		}
		if err := json.NewEncoder(w).Encode(sessions); err != nil {
			http.Error(w, `{"error":"failed to encode sessions"}`, http.StatusInternalServerError)
		}
	}
}

func handleEvents(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch := store.Subscribe()
		defer store.Unsubscribe(ch)

		for {
			select {
			case <-r.Context().Done():
				return
			case snap, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(snap)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func handleLive(hub *LiveHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
			return
		}
		// The parent server sets a write deadline on every connection, which
		// would tear down a long-lived SSE stream. Clear it for this response.
		if rc := http.NewResponseController(w); rc != nil {
			_ = rc.SetWriteDeadline(time.Time{})
		}

		sessionID := chi.URLParam(r, "sessionId")
		if !hub.HasPage(sessionID) {
			http.Error(w, `{"error":"browser session is no longer active"}`, http.StatusGone)
			return
		}
		ch, unsubscribe := hub.Subscribe(sessionID)
		defer unsubscribe()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case frame, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(frame)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
