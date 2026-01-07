package handler

import (
	"context"
	"net/http"
)

type Reloader func(ctx context.Context) error

func Reload(reload Reloader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := reload(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write([]byte("OK\n"))
	}
}
