package watch

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func Handler(store *Store) http.Handler {
	r := chi.NewRouter()
	r.Get("/", handleIndex)
	r.Get("/snapshots", handleSnapshots(store))
	r.Get("/snapshots/{sessionId}", handleSnapshot(store))
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
		snap, ok := store.Get(sessionID)
		if !ok {
			http.Error(w, `{"error":"snapshot not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(snap); err != nil {
			http.Error(w, `{"error":"failed to encode snapshot"}`, http.StatusInternalServerError)
		}
	}
}
