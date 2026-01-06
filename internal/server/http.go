package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func Run(srv *http.Server, shutdown time.Duration, reload func()) {
	go func() {
		log.Printf("listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for {
		s := <-sig
		if s == syscall.SIGHUP {
			log.Println("hot reload triggered (SIGHUP)")
			reload()
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), shutdown)
		defer cancel()
		log.Println("shutting down")
		_ = srv.Shutdown(ctx)
		return
	}
}
