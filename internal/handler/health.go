package handler

import (
	"net/http"

	"ipinfo-memory-api/internal/dataset"
)

func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	}
}

func Ready(store *dataset.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !store.Ready() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready\n"))
	}
}
