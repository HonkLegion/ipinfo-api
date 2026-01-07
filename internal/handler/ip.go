package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"ipinfo-api/internal/dataset"
)

func IP(store *dataset.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ipStr := strings.TrimPrefix(r.URL.Path, "/ip/")
		ip := net.ParseIP(ipStr)
		if ip == nil {
			http.Error(w, "invalid ip", http.StatusBadRequest)
			return
		}

		meta := store.Lookup(ip)
		if meta == nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}
}
